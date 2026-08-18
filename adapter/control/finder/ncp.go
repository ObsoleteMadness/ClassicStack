package finder

import (
	"fmt"
	"time"

	ncpclient "github.com/ObsoleteMadness/ClassicStack/client/ncp"
	"github.com/ObsoleteMadness/ClassicStack/client/uri"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	ipxport "github.com/ObsoleteMadness/ClassicStack/core/port/ipx"
	ipxproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ipx"
	ncpproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ncp"
)

const ncpBrowseWindow = 2 * time.Second

const sapDiscoverIPXType uint8 = 0x04 // IPX packet type SAP rides (PEP)

var sapQueryFrameTypes = []ipxport.FrameType{ipxport.FrameEthernetII, ipxport.FrameRaw8023, ipxport.FrameLLC8022}

func (s *Service) discoverNCP(req DiscoverRequest) ([]VolumeInfo, error) {
	opener, err := s.openerFor(KindNCP, req.IfaceType, req.Iface, req.Transport, uri.Target{})
	if err != nil {
		return nil, err
	}
	fl, err := opener.FrameLink("ipx")
	if err != nil {
		s.log.Log1(log.Debug, "finder ncp sap scan", log.Str("err", err.Error()))
		return nil, err
	}
	defer fl.Close()

	srcMAC := opener.MAC
	if srcMAC == ([6]byte{}) {
		srcMAC = ncpclient.RandomMAC()
	}
	query := ncpproto.MarshalQuery(ncpproto.SAPGeneralQuery, ncpproto.SAPServerTypeFileServer, nil)
	d := &ipxproto.Datagram{
		Type:    sapDiscoverIPXType,
		DstNode: [6]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
		DstSock: ncpproto.SAPSocket,
		SrcNode: srcMAC,
		SrcSock: ncpproto.SAPSocket,
		Payload: query,
	}
	for _, ft := range sapQueryFrameTypes {
		if err := writeSAPFrame(fl, d, srcMAC, ft); err != nil {
			s.log.Log2(log.Debug, "finder ncp sap send",
				log.Str("frametype", ft.String()), log.Str("err", err.Error()))
		}
	}

	seen := map[string]bool{}
	var out []VolumeInfo
	deadline := time.Now().Add(ncpBrowseWindow)
	for time.Now().Before(deadline) {
		frame, err := fl.Read()
		if err != nil {
			if err == link.ErrTimeout {
				continue
			}
			break
		}
		payload, _, ok := ipxport.Strip(frame)
		if !ok {
			continue
		}
		dd, derr := ipxproto.Decode(payload)
		if derr != nil || (dd.DstSock != ncpproto.SAPSocket && dd.SrcSock != ncpproto.SAPSocket) {
			continue
		}
		op, entries, perr := ncpproto.ParseSAPResponse(dd.Payload)
		if perr != nil || (op != ncpproto.SAPGeneralResponse && op != ncpproto.SAPNearestResponse) {
			continue
		}
		for _, e := range entries {
			if e.Type != ncpproto.SAPServerTypeFileServer || e.Name == "" || seen[e.Name] {
				continue
			}
			seen[e.Name] = true
			out = append(out, ncpVolume(e))
		}
	}
	s.log.Log2(log.Debug, "finder ncp sap scan",
		log.Str("iface", opener.Spec.Name), log.Int("count", int64(len(out))))
	return out, nil
}

func ncpVolume(e ncpproto.SAPEntry) VolumeInfo {
	return VolumeInfo{
		ID:        fmt.Sprintf("ncp://%s/SYS", e.Name),
		Kind:      KindNCP,
		Title:     e.Name,
		Protocol:  KindNCP,
		Transport: TransportIPX,
		Address:   sapAddress(e),
		URI:       serverURI(KindNCP, e.Name, TransportIPX),
	}
}

func sapAddress(e ncpproto.SAPEntry) string {
	return fmt.Sprintf("%02X%02X%02X%02X:%02X:%02X:%02X:%02X:%02X:%02X",
		e.Network[0], e.Network[1], e.Network[2], e.Network[3],
		e.Node[0], e.Node[1], e.Node[2], e.Node[3], e.Node[4], e.Node[5])
}

func writeSAPFrame(fl link.FrameLink, d *ipxproto.Datagram, srcMAC [6]byte, frameType ipxport.FrameType) error {
	ipxBytes, err := d.Encode(nil)
	if err != nil {
		return err
	}
	return fl.Write(frameType.Encapsulate(d.DstNode, srcMAC, ipxBytes))
}

// formatNCPLogin lists the bindery login methods the browse used. ClassicStack's
// own NCP server authenticates cleartext; keyed login is accepted as guest-equivalent.
// A real NetWare 3.x server requires encrypted bindery login.
func formatNCPLogin(encrypted bool) []string {
	if encrypted {
		return []string{"Encrypted bindery", "Unencrypted"}
	}
	return []string{"Unencrypted"}
}
