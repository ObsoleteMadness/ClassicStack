package afp

import (
	"encoding/binary"
	"testing"
)

func TestConstrainAFPVolumeType(t *testing.T) {
	tests := []struct {
		name string
		in   uint16
		want uint16
	}{
		{name: "flat", in: AFPVolumeTypeFlat, want: AFPVolumeTypeFlat},
		{name: "fixed", in: AFPVolumeTypeFixedDirID, want: AFPVolumeTypeFixedDirID},
		{name: "variable", in: AFPVolumeTypeVariableDirID, want: AFPVolumeTypeVariableDirID},
		{name: "invalid defaults to fixed", in: 99, want: AFPVolumeTypeFixedDirID},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := constrainAFPVolumeType(tt.in); got != tt.want {
				t.Fatalf("constrainAFPVolumeType(%d)=%d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestAFP_OpenVol_UsesFixedDirIDVolumeType(t *testing.T) {
	root := t.TempDir()
	s := NewAFPService("TestServer", []VolumeConfig{{Name: "Vol", Path: root}}, &LocalFileSystem{}, nil)

	res, errCode := s.handleOpenVol(&FPOpenVolReq{
		Bitmap:  VolBitmapSignature | VolBitmapVolID,
		VolName: "Vol",
	})
	if errCode != NoErr {
		t.Fatalf("errCode=%d", errCode)
	}
	if len(res.Data) < 2 {
		t.Fatalf("data too short: %d", len(res.Data))
	}

	sig := binary.BigEndian.Uint16(res.Data[0:2])
	if sig != AFPVolumeTypeFixedDirID {
		t.Fatalf("signature=%d, want %d (Fixed Directory ID)", sig, AFPVolumeTypeFixedDirID)
	}
}

func TestAFP_GetVolParms_UsesFixedDirIDVolumeType(t *testing.T) {
	root := t.TempDir()
	s := NewAFPService("TestServer", []VolumeConfig{{Name: "Vol", Path: root}}, &LocalFileSystem{}, nil)

	res, errCode := s.handleGetVolParms(&FPGetVolParmsReq{
		VolumeID: 1,
		Bitmap:   VolBitmapSignature,
	})
	if errCode != NoErr {
		t.Fatalf("errCode=%d", errCode)
	}
	if len(res.Data) < 2 {
		t.Fatalf("data too short: %d", len(res.Data))
	}

	sig := binary.BigEndian.Uint16(res.Data[0:2])
	if sig != AFPVolumeTypeFixedDirID {
		t.Fatalf("signature=%d, want %d (Fixed Directory ID)", sig, AFPVolumeTypeFixedDirID)
	}
}
