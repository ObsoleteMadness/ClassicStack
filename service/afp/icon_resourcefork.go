//go:build afp

package afp

// BNDL/FREF/ICN# extraction on top of the generic resource-fork parser in
// resource_fork.go. Used to populate the AFP Desktop database from
// AppleDouble resource forks on volumes that were never served through our
// own FPAddIcon path.

import (
	"encoding/binary"
)

// extractedIcon is one (creator, fileType) → icon mapping derived from a
// BNDL/FREF chain in a classic Mac resource fork, or from an AppleDouble
// embedded icon entry.
type extractedIcon struct {
	creator  [4]byte
	fileType [4]byte
	iconType byte // AFP icon type; 1 = large B&W (ICN#).
	bitmap   []byte
}

// AFP icon-type codes for the classic icon family. These mirror what Finder
// sends in the FPGetIcon/FPAddIcon iType byte.
const (
	afpIconTypeICN   byte = 1 // 'ICN#' 32x32 1-bit
	afpIconTypeICL4  byte = 2 // 'icl4' 32x32 4-bit
	afpIconTypeICL8  byte = 3 // 'icl8' 32x32 8-bit
	afpIconTypeICSBW byte = 4 // 'ics#' 16x16 1-bit
	afpIconTypeICS4  byte = 5 // 'ics4' 16x16 4-bit
	afpIconTypeICS8  byte = 6 // 'ics8' 16x16 8-bit
)

// ICN# is 128 bytes of bitmap followed by 128 bytes of mask = 256 total.
const icnPoundSize = 256

// iconFromAppleDoubleEntry converts an AppleDouble embedded B&W icon entry
// (128-byte 32x32 1-bit bitmap, no mask) into an AFP ICN# icon keyed by the
// file's own (type, creator) from its FinderInfo. A 128-byte all-ones mask is
// appended to turn the bare bitmap into a valid 256-byte ICN# payload.
func iconFromAppleDoubleEntry(finderInfo [32]byte, iconBW []byte) (extractedIcon, bool) {
	if len(iconBW) < 128 {
		return extractedIcon{}, false
	}
	var fileType, creator [4]byte
	copy(fileType[:], finderInfo[0:4])
	copy(creator[:], finderInfo[4:8])
	var zero [4]byte
	if fileType == zero && creator == zero {
		return extractedIcon{}, false
	}
	bitmap := make([]byte, icnPoundSize)
	copy(bitmap[0:128], iconBW[:128])
	for i := 128; i < icnPoundSize; i++ {
		bitmap[i] = 0xFF
	}
	return extractedIcon{
		creator:  creator,
		fileType: fileType,
		iconType: afpIconTypeICN,
		bitmap:   bitmap,
	}, true
}

// extractAppIconFromResourceFork returns the default application icons for an
// APPL file: 'ICN#', 'icl4', and 'icl8' resources with ID 128 (the classic
// default). Each is emitted as (creator, 'APPL') with the appropriate AFP
// icon-type byte. Caller supplies the app's creator code (from FinderInfo).
func extractAppIconFromResourceFork(rsrc []byte, creator [4]byte) []extractedIcon {
	resources, err := parseResourceFork(rsrc)
	if err != nil || len(resources) == 0 {
		return nil
	}
	appl := [4]byte{'A', 'P', 'P', 'L'}
	icn := [4]byte{'I', 'C', 'N', '#'}
	icl4 := [4]byte{'i', 'c', 'l', '4'}
	icl8 := [4]byte{'i', 'c', 'l', '8'}
	var out []extractedIcon
	for _, r := range resources {
		if r.resID != 128 {
			continue
		}
		switch r.resType {
		case icn:
			if len(r.data) >= icnPoundSize {
				out = append(out, extractedIcon{
					creator:  creator,
					fileType: appl,
					iconType: afpIconTypeICN,
					bitmap:   append([]byte(nil), r.data[:icnPoundSize]...),
				})
			}
		case icl4:
			out = append(out, extractedIcon{
				creator:  creator,
				fileType: appl,
				iconType: afpIconTypeICL4,
				bitmap:   append([]byte(nil), r.data...),
			})
		case icl8:
			out = append(out, extractedIcon{
				creator:  creator,
				fileType: appl,
				iconType: afpIconTypeICL8,
				bitmap:   append([]byte(nil), r.data...),
			})
		}
	}
	return out
}

// kCustomIconResource is the classic Mac resource ID used by Finder for
// custom folder/file icons stored in the "Icon\r" file's resource fork.
const kCustomIconResource int16 = -16455

// extractCustomIconFromResourceFork extracts ICN#, icl4, and icl8 resources
// at the well-known custom icon resource ID (-16455) from an Icon\r file's
// resource fork. The icons are keyed under the supplied creator and fileType
// (typically the folder's type/creator from FinderInfo, or a default pair).
func extractCustomIconFromResourceFork(rsrc []byte, creator, fileType [4]byte) []extractedIcon {
	resources, err := parseResourceFork(rsrc)
	if err != nil || len(resources) == 0 {
		return nil
	}
	icn := [4]byte{'I', 'C', 'N', '#'}
	icl4 := [4]byte{'i', 'c', 'l', '4'}
	icl8 := [4]byte{'i', 'c', 'l', '8'}
	var out []extractedIcon
	for _, r := range resources {
		if r.resID != kCustomIconResource {
			continue
		}
		switch r.resType {
		case icn:
			if len(r.data) >= icnPoundSize {
				out = append(out, extractedIcon{
					creator:  creator,
					fileType: fileType,
					iconType: afpIconTypeICN,
					bitmap:   append([]byte(nil), r.data[:icnPoundSize]...),
				})
			}
		case icl4:
			out = append(out, extractedIcon{
				creator:  creator,
				fileType: fileType,
				iconType: afpIconTypeICL4,
				bitmap:   append([]byte(nil), r.data...),
			})
		case icl8:
			out = append(out, extractedIcon{
				creator:  creator,
				fileType: fileType,
				iconType: afpIconTypeICL8,
				bitmap:   append([]byte(nil), r.data...),
			})
		}
	}
	return out
}

// extractIconsFromResourceFork walks a resource fork's BNDL resources and
// joins FREF → ICN# chains to produce (creator, fileType) → ICN# bitmaps.
// Returns nil if there is no BNDL or the chain cannot be resolved.
func extractIconsFromResourceFork(rsrc []byte) []extractedIcon {
	resources, err := parseResourceFork(rsrc)
	if err != nil || len(resources) == 0 {
		return nil
	}
	var bndls []resourceForkResource
	frefsByID := map[int16][]byte{}
	icnByID := map[int16][]byte{}
	for _, r := range resources {
		switch r.resType {
		case [4]byte{'B', 'N', 'D', 'L'}:
			bndls = append(bndls, r)
		case [4]byte{'F', 'R', 'E', 'F'}:
			frefsByID[r.resID] = r.data
		case [4]byte{'I', 'C', 'N', '#'}:
			icnByID[r.resID] = r.data
		}
	}
	if len(bndls) == 0 {
		return nil
	}
	var out []extractedIcon
	for _, b := range bndls {
		d := b.data
		if len(d) < 8 {
			continue
		}
		var creator [4]byte
		copy(creator[:], d[0:4])
		// d[4:6] version, d[6:8] numTypes-1
		numTypesM1 := binary.BigEndian.Uint16(d[6:8])
		numTypes := int(numTypesM1) + 1
		off := 8
		// Parse BNDL into typeMaps[typeCode][localID] = actualResID.
		typeMaps := make(map[[4]byte]map[uint16]int16, numTypes)
		for t := 0; t < numTypes; t++ {
			if off+6 > len(d) {
				break
			}
			var tc [4]byte
			copy(tc[:], d[off:off+4])
			countM1 := binary.BigEndian.Uint16(d[off+4 : off+6])
			off += 6
			count := int(countM1) + 1
			m := make(map[uint16]int16, count)
			for k := 0; k < count; k++ {
				if off+4 > len(d) {
					break
				}
				localID := binary.BigEndian.Uint16(d[off : off+2])
				resID := int16(binary.BigEndian.Uint16(d[off+2 : off+4]))
				m[localID] = resID
				off += 4
			}
			typeMaps[tc] = m
		}
		frefMap := typeMaps[[4]byte{'F', 'R', 'E', 'F'}]
		iconMap := typeMaps[[4]byte{'I', 'C', 'N', '#'}]
		if frefMap == nil || iconMap == nil {
			continue
		}
		for localID, frefResID := range frefMap {
			frefData, ok := frefsByID[frefResID]
			if !ok || len(frefData) < 4 {
				continue
			}
			var fileType [4]byte
			copy(fileType[:], frefData[0:4])
			iconResID, ok := iconMap[localID]
			if !ok {
				continue
			}
			icn, ok := icnByID[iconResID]
			if !ok || len(icn) < icnPoundSize {
				continue
			}
			out = append(out, extractedIcon{
				creator:  creator,
				fileType: fileType,
				iconType: afpIconTypeICN,
				bitmap:   append([]byte(nil), icn[:icnPoundSize]...),
			})
		}
	}
	return out
}
