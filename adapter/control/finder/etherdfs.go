package finder

import (
	"errors"
	"fmt"
	"strings"
	"time"

	etherdfsclient "github.com/ObsoleteMadness/ClassicStack/client/etherdfs"
	"github.com/ObsoleteMadness/ClassicStack/client/uri"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	etherdfsproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/etherdfs"
)

const etherdfsBrowseWindow = 2 * time.Second

func (s *Service) discoverEtherDFS(req DiscoverRequest) ([]VolumeInfo, error) {
	opener, err := s.openerFor(KindEtherDFS, req.IfaceType, req.Iface, req.Transport, uri.Target{})
	if err != nil {
		return nil, err
	}
	fl, err := opener.FrameLink("ether proto 0xedf5")
	if err != nil {
		s.log.Log1(log.Debug, "finder etherdfs scan", log.Str("err", err.Error()))
		return nil, err
	}
	defer func() { _ = fl.Close() }()

	srcMAC := opener.MAC
	if srcMAC == ([6]byte{}) {
		srcMAC = etherdfsclient.RandomMAC()
	}
	reqFrame := etherdfsproto.Frame{
		DstMAC:   [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
		SrcMAC:   srcMAC,
		Sequence: 1,
		Drive:    0,
		Opcode:   etherdfsproto.OpInstallChk,
	}
	if err := fl.Write(reqFrame.Encode(nil)); err != nil {
		return nil, err
	}

	seen := map[[6]byte]bool{}
	var out []VolumeInfo
	deadline := time.Now().Add(etherdfsBrowseWindow)
	for time.Now().Before(deadline) {
		frame, err := fl.Read()
		if err != nil {
			if errors.Is(err, link.ErrTimeout) {
				continue
			}
			break
		}
		f, perr := etherdfsproto.ParseFrame(frame)
		if perr != nil || f.SrcMAC == srcMAC || seen[f.SrcMAC] {
			continue
		}
		seen[f.SrcMAC] = true
		name := strings.TrimRight(string(f.Payload), "\x00")
		mac := fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x",
			f.SrcMAC[0], f.SrcMAC[1], f.SrcMAC[2], f.SrcMAC[3], f.SrcMAC[4], f.SrcMAC[5])
		title := name
		if title == "" {
			title = mac
		}
		out = append(out, VolumeInfo{
			ID:        fmt.Sprintf("etherdfs://%s/C", mac),
			Kind:      KindEtherDFS,
			Title:     title,
			Subtitle:  mac,
			Protocol:  KindEtherDFS,
			Transport: TransportEDFS,
			Address:   mac,
			URI:       serverURI(KindEtherDFS, strings.ReplaceAll(mac, ":", "-"), ""),
		})
	}
	s.log.Log2(log.Debug, "finder etherdfs scan",
		log.Str("iface", opener.Spec.Name), log.Int("count", int64(len(out))))
	return out, nil
}
