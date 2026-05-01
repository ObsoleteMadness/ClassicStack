//go:build afp || all

package afp

import (
	"bytes"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/pgodw/omnitalk/pkg/binutil"
)

// toAFPTime converts a Go time.Time to AFP's seconds-since-1904 epoch.
// Times before the epoch clamp to 0; overflow clamps to the max uint32.
func toAFPTime(t time.Time) uint32 {
	epoch := time.Date(1904, 1, 1, 0, 0, 0, 0, time.Local)
	if t.Before(epoch) {
		return 0
	}
	secs := t.Sub(epoch).Seconds()
	if secs > float64(^uint32(0)) {
		return ^uint32(0)
	}
	return uint32(secs)
}

// File and directory parameter wire packing. The pack functions here
// resolve Service state (CNID, metadata, FS capabilities) and emit the
// AFP 2.x file/directory parameter block layout used by FPGetFileParms,
// FPGetDirParms, FPGetFileDirParms, and FPEnumerate result entries.

// calcDirParamsSize returns the total byte size of all fixed fields (including
// variable-name offset pointers) for a directory parameter block with the given bitmap.
func calcDirParamsSize(bitmap uint16) int {
	size := 0
	if bitmap&DirBitmapAttributes != 0 {
		size += 2
	}
	if bitmap&DirBitmapParentDID != 0 {
		size += 4
	}
	if bitmap&DirBitmapCreateDate != 0 {
		size += 4
	}
	if bitmap&DirBitmapModDate != 0 {
		size += 4
	}
	if bitmap&DirBitmapBackupDate != 0 {
		size += 4
	}
	if bitmap&DirBitmapFinderInfo != 0 {
		size += 32
	}
	if bitmap&DirBitmapLongName != 0 {
		size += 2 // offset pointer
	}
	if bitmap&DirBitmapShortName != 0 {
		size += 2 // offset pointer
	}
	if bitmap&DirBitmapDirID != 0 {
		size += 4
	}
	if bitmap&DirBitmapOffspringCount != 0 {
		size += 2
	}
	if bitmap&DirBitmapOwnerID != 0 {
		size += 4
	}
	if bitmap&DirBitmapGroupID != 0 {
		size += 4
	}
	if bitmap&DirBitmapAccessRights != 0 {
		size += 4
	}
	if bitmap&DirBitmapProDOSInfo != 0 {
		size += 6
	}
	return size
}

// calcFileParamsSize returns the total byte size of all fixed fields (including
// variable-name offset pointers) for a file parameter block with the given bitmap.
func calcFileParamsSize(bitmap uint16) int {
	size := 0
	if bitmap&FileBitmapAttributes != 0 {
		size += 2
	}
	if bitmap&FileBitmapParentDID != 0 {
		size += 4
	}
	if bitmap&FileBitmapCreateDate != 0 {
		size += 4
	}
	if bitmap&FileBitmapModDate != 0 {
		size += 4
	}
	if bitmap&FileBitmapBackupDate != 0 {
		size += 4
	}
	if bitmap&FileBitmapFinderInfo != 0 {
		size += 32
	}
	if bitmap&FileBitmapLongName != 0 {
		size += 2 // offset pointer
	}
	if bitmap&FileBitmapShortName != 0 {
		size += 2 // offset pointer
	}
	if bitmap&FileBitmapFileNum != 0 {
		size += 4
	}
	if bitmap&FileBitmapDataForkLen != 0 {
		size += 4
	}
	if bitmap&FileBitmapRsrcForkLen != 0 {
		size += 4
	}
	if bitmap&FileBitmapProDOSInfo != 0 {
		size += 6
	}
	return size
}

func (s *Service) packFileInfo(buf *bytes.Buffer, volumeID uint16, bitmap uint16, parentPath, name string, info fs.FileInfo, isDir bool) {
	var varBuf bytes.Buffer
	fullPath := filepath.Join(parentPath, name)
	name = s.catalogNameForPath(volumeID, fullPath, name)
	volFS := s.fsForVolume(volumeID)

	metadata := ForkMetadata{}
	if m := s.metaFor(volumeID); m != nil {
		if md, err := m.ReadForkMetadata(fullPath); err == nil {
			metadata = md
		}
	}
	if !isDir && !hasFinderTypeCreator(metadata.FinderInfo) && s.options.ExtensionMap != nil {
		if mapping, ok := s.options.ExtensionMap.Lookup(fullPath); ok {
			metadata.FinderInfo = applyExtensionMapping(metadata.FinderInfo, mapping)
		}
	}

	if isDir {
		fixedSize := calcDirParamsSize(bitmap)

		if bitmap&DirBitmapAttributes != 0 {
			var dirAttrs uint16
			if volFS != nil && volFS.Capabilities().DirAttributes {
				if attrs, err := volFS.DirAttributes(fullPath); err == nil {
					dirAttrs = attrs
				}
			}
			binutil.WriteU16(buf, dirAttrs)
		}
		if bitmap&DirBitmapParentDID != 0 {
			// The root directory (DID=2) has a logical parent DID of 1.
			var pdir uint32
			thisDID := s.getPathDID(volumeID, fullPath)
			if thisDID == CNIDRoot {
				pdir = CNIDParentOfRoot
			} else {
				pdir = s.getPathDID(volumeID, parentPath)
			}
			binutil.WriteU32(buf, pdir)
		}
		if bitmap&DirBitmapCreateDate != 0 {
			binutil.WriteU32(buf, uint32(toAFPTime(info.ModTime())))
		}
		if bitmap&DirBitmapModDate != 0 {
			binutil.WriteU32(buf, uint32(toAFPTime(info.ModTime())))
		}
		if bitmap&DirBitmapBackupDate != 0 {
			binutil.WriteU32(buf, 0)
		}
		if bitmap&DirBitmapFinderInfo != 0 {
			buf.Write(metadata.FinderInfo[:])
		}
		if bitmap&DirBitmapLongName != 0 {
			offset := uint16(fixedSize + varBuf.Len())
			binutil.WriteU16(buf, offset)
			s.writeAFPName(&varBuf, name, volumeID)
		}
		if bitmap&DirBitmapShortName != 0 {
			offset := uint16(fixedSize + varBuf.Len())
			binutil.WriteU16(buf, offset)
			s.writeAFPName(&varBuf, name, volumeID)
		}
		if bitmap&DirBitmapDirID != 0 {
			did := s.getPathDID(volumeID, fullPath)
			binutil.WriteU32(buf, did)
		}
		if bitmap&DirBitmapOffspringCount != 0 {
			count := uint16(0)
			if volFS != nil && volFS.Capabilities().ChildCount {
				if cachedCount, err := volFS.ChildCount(fullPath); err == nil {
					count = cachedCount
				} else if entries, dirErr := volFS.ReadDir(fullPath); dirErr == nil {
					for _, e := range entries {
						if !s.isMetadataArtifact(e.Name(), e.IsDir(), volumeID) {
							count++
						}
					}
				}
			} else if volFS != nil {
				if entries, err := volFS.ReadDir(fullPath); err == nil {
					for _, e := range entries {
						if !s.isMetadataArtifact(e.Name(), e.IsDir(), volumeID) {
							count++
						}
					}
				}
			}
			binutil.WriteU16(buf, count)
		}
		if bitmap&DirBitmapOwnerID != 0 {
			binutil.WriteU32(buf, 0)
		}
		if bitmap&DirBitmapGroupID != 0 {
			binutil.WriteU32(buf, 0)
		}
		if bitmap&DirBitmapAccessRights != 0 {
			rights := uint32(0x87070707)
			if s.volumeIsReadOnly(volumeID) {
				// Read-only volumes should advertise read+search rights, not write.
				rights = 0x87030303
			}
			binutil.WriteU32(buf, rights)
		}
		if bitmap&DirBitmapProDOSInfo != 0 {
			buf.Write(make([]byte, 6))
		}
	} else {
		fixedSize := calcFileParamsSize(bitmap)

		if bitmap&FileBitmapAttributes != 0 {
			attr := uint16(0)
			if s.volumeIsReadOnly(volumeID) {
				attr |= FileAttrWriteInhibit
			}
			binutil.WriteU16(buf, attr)
		}
		if bitmap&FileBitmapParentDID != 0 {
			pdir := s.getPathDID(volumeID, parentPath)
			binutil.WriteU32(buf, pdir)
		}
		if bitmap&FileBitmapCreateDate != 0 {
			binutil.WriteU32(buf, uint32(toAFPTime(info.ModTime())))
		}
		if bitmap&FileBitmapModDate != 0 {
			binutil.WriteU32(buf, uint32(toAFPTime(info.ModTime())))
		}
		if bitmap&FileBitmapBackupDate != 0 {
			binutil.WriteU32(buf, 0)
		}
		if bitmap&FileBitmapFinderInfo != 0 {
			buf.Write(metadata.FinderInfo[:])
		}
		if bitmap&FileBitmapLongName != 0 {
			offset := uint16(fixedSize + varBuf.Len())
			binutil.WriteU16(buf, offset)
			s.writeAFPName(&varBuf, name, volumeID)
		}
		if bitmap&FileBitmapShortName != 0 {
			offset := uint16(fixedSize + varBuf.Len())
			binutil.WriteU16(buf, offset)
			s.writeAFPName(&varBuf, name, volumeID)
		}
		if bitmap&FileBitmapFileNum != 0 {
			did := s.getPathDID(volumeID, fullPath)
			binutil.WriteU32(buf, did)
		}
		if bitmap&FileBitmapDataForkLen != 0 {
			binutil.WriteU32(buf, uint32(info.Size()))
		}
		if bitmap&FileBitmapRsrcForkLen != 0 {
			binutil.WriteU32(buf, uint32(metadata.ResourceForkLen))
		}
		if bitmap&FileBitmapProDOSInfo != 0 {
			buf.Write(make([]byte, 6))
		}
	}

	buf.Write(varBuf.Bytes())
}
