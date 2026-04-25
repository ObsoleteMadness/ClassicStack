//go:build macgarden

package afp

import (
	"encoding/binary"
	"testing"
)

func TestAFP_MacGardenVolume_AdvertisesReadOnlyAndCatSearch(t *testing.T) {
	root := t.TempDir()
	s := NewAFPService("TestServer", []VolumeConfig{{Name: "Garden", Path: root, FSType: FSTypeMacGarden}}, NewMacGardenFileSystem(root), nil)

	openRes, errCode := s.handleOpenVol(&FPOpenVolReq{
		Bitmap:  VolBitmapAttributes | VolBitmapVolID,
		VolName: "Garden",
	})
	if errCode != NoErr {
		t.Fatalf("handleOpenVol errCode=%d, want %d", errCode, NoErr)
	}
	if len(openRes.Data) < 2 {
		t.Fatalf("open data too short: %d", len(openRes.Data))
	}
	openAttrs := binary.BigEndian.Uint16(openRes.Data[:2])
	want := VolAttrReadOnly | VolAttrSupportsCatSearch
	if openAttrs != want {
		t.Fatalf("open attrs=%#04x, want %#04x", openAttrs, want)
	}

	getRes, errCode := s.handleGetVolParms(&FPGetVolParmsReq{
		VolumeID: 1,
		Bitmap:   VolBitmapAttributes,
	})
	if errCode != NoErr {
		t.Fatalf("handleGetVolParms errCode=%d, want %d", errCode, NoErr)
	}
	if len(getRes.Data) < 2 {
		t.Fatalf("getvol data too short: %d", len(getRes.Data))
	}
	getAttrs := binary.BigEndian.Uint16(getRes.Data[:2])
	if getAttrs != want {
		t.Fatalf("getvol attrs=%#04x, want %#04x", getAttrs, want)
	}
}
