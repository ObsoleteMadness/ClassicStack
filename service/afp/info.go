//go:build afp || all

package afp

import (
	"bytes"

	"github.com/ObsoleteMadness/ClassicStack/pkg/binutil"
)

// BuildServerInfo constructs the payload for an AFP FPGetSrvrInfo or ASP GetStatus reply.
// The structure of the Server Information block is:
// [Machine Offset:2]
// [AFP Versions Offset:2]
// [UAMs Offset:2]
// [Volume Icon Offset:2]
// [Server Flags:2]
// [Server Name:PStr]
// ... paddings and variable strings ...
func BuildServerInfo(serverName string) []byte {
	machineType := "Macintosh"
	afpVersions := []string{Version20, Version21}
	uams := []string{UAMNoUserAuthent}

	// Start offsets after the 4 offsets (8 bytes) + 2 bytes for Flags
	// + 1 byte for ServerName length + ServerName string length
	baseOffset := 8 + 2 + 1 + len(serverName)
	if baseOffset%2 != 0 {
		baseOffset++ // ServerName is often padded to an even boundary
	}

	machineOffset := baseOffset
	machineLen := 1 + len(machineType)

	// In the spec, ONLY the field immediately following ServerName is padded
	// so that it begins on an even boundary. All other fields are packed back-to-back.
	versionsOffset := machineOffset + machineLen

	// Calculate versions block length: 1 byte count + (1 byte length + string len) for each
	versionsLen := 1
	for _, v := range afpVersions {
		versionsLen += 1 + len(v)
	}

	uamsOffset := versionsOffset + versionsLen

	uamsLen := 1
	for _, u := range uams {
		uamsLen += 1 + len(u)
	}

	// We do not have a volume icon
	iconOffset := 0

	buf := new(bytes.Buffer)

	// Write Offsets
	// For FPGetSrvrInfo, the layout requires exactly 4 offsets.
	binutil.WriteU16(buf, uint16(machineOffset))
	binutil.WriteU16(buf, uint16(versionsOffset))
	binutil.WriteU16(buf, uint16(uamsOffset))
	binutil.WriteU16(buf, uint16(iconOffset))

	// Write Flags
	flags := uint16(0x0001 | 0x0002) // Supports CopyFile, Supports Choose Message (example flags)
	binutil.WriteU16(buf, flags)

	// Write Server Name (Pascal String)
	buf.WriteByte(byte(len(serverName)))
	buf.WriteString(serverName)

	// Pad to machineOffset
	for buf.Len() < machineOffset {
		buf.WriteByte(0)
	}

	// Write Machine Type (Pascal String)
	buf.WriteByte(byte(len(machineType)))
	buf.WriteString(machineType)

	// Write AFP Versions
	buf.WriteByte(byte(len(afpVersions)))
	for _, v := range afpVersions {
		buf.WriteByte(byte(len(v)))
		buf.WriteString(v)
	}

	// Write UAMs
	buf.WriteByte(byte(len(uams)))
	for _, u := range uams {
		buf.WriteByte(byte(len(u)))
		buf.WriteString(u)
	}

	return buf.Bytes()
}
