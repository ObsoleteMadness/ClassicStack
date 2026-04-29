//go:build afp || all

package afp

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestBuildServerInfo_LayoutAndPadding(t *testing.T) {
	serverName := "TestServer"
	payload := BuildServerInfo(serverName)

	if len(payload) < 16 {
		t.Fatalf("Payload too short: %d bytes", len(payload))
	}

	buf := bytes.NewReader(payload)

	var machineOffset, versionsOffset, uamsOffset, iconOffset, flags uint16
	binary.Read(buf, binary.BigEndian, &machineOffset)
	binary.Read(buf, binary.BigEndian, &versionsOffset)
	binary.Read(buf, binary.BigEndian, &uamsOffset)
	binary.Read(buf, binary.BigEndian, &iconOffset)
	binary.Read(buf, binary.BigEndian, &flags)

	// Validate Server Name
	nameLen, _ := buf.ReadByte()
	nameBuf := make([]byte, nameLen)
	buf.Read(nameBuf)
	if string(nameBuf) != serverName {
		t.Errorf("Expected ServerName %s, got %s", serverName, string(nameBuf))
	}

	// Calculate expected offsets based on spec.
	// Offsets (4 * 2) = 8 bytes.
	// Flags = 2 bytes.
	// ServerName len byte = 1 byte.
	// ServerName string = 10 bytes ("TestServer").
	// Total before padding = 8 + 2 + 1 + 10 = 21 bytes.
	// The next field (Machine Type) MUST start on an even boundary.
	// So machineOffset should be 22 (padded by 1 byte).
	expectedMachineOffset := uint16(22)
	if machineOffset != expectedMachineOffset {
		t.Errorf("Expected machineOffset to be %d, got %d", expectedMachineOffset, machineOffset)
	}

	// Machine type length = 1 byte length + 9 bytes "Macintosh" = 10 bytes.
	// No padding needed here since it's packed back to back!
	// versionsOffset should be 22 + 10 = 32.
	expectedVersionsOffset := machineOffset + 10
	if versionsOffset != expectedVersionsOffset {
		t.Errorf("Expected versionsOffset to be %d, got %d", expectedVersionsOffset, versionsOffset)
	}

	// Validate AFP Versions
	vCount := int(payload[versionsOffset])
	if vCount != 2 {
		t.Errorf("Expected 2 AFP versions, got %d", vCount)
	}

	// versions block length: 1 count + (1 len + 14 chars "AFPVersion 2.0") + (1 len + 14 chars "AFPVersion 2.1")
	// total = 1 + 15 + 15 = 31 bytes.
	expectedUamsOffset := versionsOffset + 31
	if uamsOffset != expectedUamsOffset {
		t.Errorf("Expected uamsOffset to be %d, got %d", expectedUamsOffset, uamsOffset)
	}

	// Verify the actual payload length matches what we expect
	// UAM block length: 1 count + (1 len + 15 chars "No User Authent") = 17 bytes.
	expectedTotalLength := int(expectedUamsOffset) + 17
	if len(payload) != expectedTotalLength {
		t.Errorf("Expected payload length %d, got %d", expectedTotalLength, len(payload))
	}
}
