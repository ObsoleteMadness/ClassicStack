package smb

import (
	"encoding/binary"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
	netbiosproto "github.com/ObsoleteMadness/ClassicStack/protocol/netbios"
	"github.com/ObsoleteMadness/ClassicStack/service/netbios"
)

func (s *Service) HandleSession(_ *netbiosproto.SessionPacket) error { return ErrNotImplemented }

// HandleSessionContext implements netbios.ContextualSessionHandler.
// It handles the minimal SMB1 session sequence needed for Network
// Neighbourhood enumeration: NegotiateProtocol (0x72), SessionSetupAndX
// (0x73), TreeConnectAndX (0x75), and LANMAN Transaction requests on
// \PIPE\LANMAN (NetServerEnum2). All other commands return
// STATUS_NOT_SUPPORTED.
func (s *Service) HandleSessionContext(packet *netbiosproto.SessionPacket, ctx netbios.SessionContext) (*netbiosproto.SessionPacket, error) {
	if packet == nil || len(packet.Payload) < smbHeaderLen || string(packet.Payload[0:4]) != "\xffSMB" {
		return nil, nil
	}

	connID := connKeyFromSession(ctx)
	conn := s.ensureConn(connID)

	server := s.opts.ServerName
	if server == "" {
		server = "CLASSICSTACK"
	}
	workgroup := s.opts.Workgroup
	if workgroup == "" {
		workgroup = "WORKGROUP"
	}

	cmd := packet.Payload[4]
	var respPayload []byte

	switch cmd {
	case CommandNegotiate:
		netlog.Debug("[SMB][Session] negotiate src=%x.%x:%02x%02x",
			ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
		respPayload = buildNegotiateResponse(packet.Payload, workgroup)

	case CommandSessionSetupAndX:
		netlog.Debug("[SMB][Session] session-setup src=%x.%x:%02x%02x",
			ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
		conn.mu.Lock()
		if conn.uid == 0 {
			conn.uid = s.allocUID()
		}
		uid := conn.uid
		conn.mu.Unlock()
		respPayload = buildSessionSetupResponse(packet.Payload, uid)

	case CommandTreeConnectAndX:
		netlog.Debug("[SMB][Session] tree-connect src=%x.%x:%02x%02x",
			ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
		respPayload = s.handleTreeConnectAndX(packet.Payload, conn)

	case CommandEcho:
		netlog.Debug("[SMB][Session] echo src=%x.%x:%02x%02x",
			ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
		if !isValidEchoTID(packet.Payload, conn) {
			respPayload = buildSMBErrorResponse(packet.Payload, smbStatusBadTID)
			break
		}
		respPayload = buildEchoResponse(packet.Payload)

	case CommandTreeDisconnect:
		netlog.Debug("[SMB][Session] tree-disconnect src=%x.%x:%02x%02x",
			ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
		conn.mu.Lock()
		if len(packet.Payload) >= smbHeaderLen {
			tid := binary.LittleEndian.Uint16(packet.Payload[smbOffTID : smbOffTID+2])
			delete(conn.tids, tid)
		}
		conn.mu.Unlock()
		respPayload = buildSimpleSuccessResponse(packet.Payload)

	case CommandLogoffAndX:
		netlog.Debug("[SMB][Session] logoff src=%x.%x:%02x%02x",
			ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
		s.dropConn(connID)
		respPayload = buildSimpleSuccessResponse(packet.Payload)

	case CommandTransaction:
		if !isLANMANTransactionRequest(packet.Payload) {
			netlog.Debug("[SMB][Session] unsupported transaction src=%x.%x:%02x%02x",
				ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
			respPayload = buildSMBErrorResponse(packet.Payload, smbStatusNotSupported)
		} else {
			fc, ok := parseLANMANFunctionCode(packet.Payload)
			if ok && fc == rapNetServerEnum2 {
				serverType, _ := parseNetServerEnum2ServerType(packet.Payload)
				reqDomain, _ := parseNetServerEnum2Domain(packet.Payload)
				netlog.Debug("[SMB][Session] NetServerEnum2 src=%x.%x:%02x%02x serverType=%#x domain=%q",
					ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1], serverType, reqDomain)
				entries, rapStatus := s.netServerEnum2Entries(serverType, workgroup, reqDomain)
				if rapStatus != 0 {
					respPayload = buildNetServerEnum2RAPErrorResponse(packet.Payload, rapStatus)
				} else {
					respPayload = buildNetServerEnum2Response(packet.Payload, entries)
				}
			} else {
				netlog.Debug("[SMB][Session] LANMAN fc=%#x src=%x.%x:%02x%02x",
					fc, ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
				respPayload = buildSMBTransactionEmptySuccess(packet.Payload)
			}
		}

	case CommandQueryInformationDisk:
		netlog.Debug("[SMB][Session] query-information-disk src=%x.%x:%02x%02x",
			ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
		respPayload = s.handleQueryInformationDisk(packet.Payload, conn)

	case CommandCheckDirectory:
		netlog.Debug("[SMB][Session] check-directory src=%x.%x:%02x%02x",
			ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
		respPayload = s.handleCheckDirectory(packet.Payload, conn)

	case CommandSearch:
		netlog.Debug("[SMB][Session] search src=%x.%x:%02x%02x",
			ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
		respPayload = s.handleSearch(packet.Payload, conn)

	case CommandOpenAndX:
		netlog.Debug("[SMB][Session] open-andx src=%x.%x:%02x%02x",
			ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
		respPayload = s.handleOpenAndX(packet.Payload, conn)

	case CommandReadAndX:
		netlog.Debug("[SMB][Session] read-andx src=%x.%x:%02x%02x",
			ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
		respPayload = s.handleReadAndX(packet.Payload, conn)

	case CommandWriteAndX:
		netlog.Debug("[SMB][Session] write-andx src=%x.%x:%02x%02x",
			ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
		respPayload = s.handleWriteAndX(packet.Payload, conn)

	case CommandClose:
		netlog.Debug("[SMB][Session] close src=%x.%x:%02x%02x",
			ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
		respPayload = s.handleClose(packet.Payload, conn)

	case CommandFlush:
		netlog.Debug("[SMB][Session] flush src=%x.%x:%02x%02x",
			ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
		respPayload = s.handleFlush(packet.Payload, conn)

	case CommandLockingAndX:
		netlog.Debug("[SMB][Session] locking-andx src=%x.%x:%02x%02x",
			ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
		respPayload = s.handleLockingAndX(packet.Payload, conn)

	case CommandDelete:
		netlog.Debug("[SMB][Session] delete src=%x.%x:%02x%02x",
			ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
		respPayload = s.handleDelete(packet.Payload, conn)

	case CommandRename:
		netlog.Debug("[SMB][Session] rename src=%x.%x:%02x%02x",
			ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
		respPayload = s.handleRename(packet.Payload, conn)

	case CommandDeleteDirectory:
		netlog.Debug("[SMB][Session] delete-directory src=%x.%x:%02x%02x",
			ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
		respPayload = s.handleDeleteDirectory(packet.Payload, conn)

	default:
		netlog.Debug("[SMB][Session] unsupported command=0x%02x src=%x.%x:%02x%02x",
			cmd, ctx.Remote.Network, ctx.Remote.Node, ctx.Remote.Socket[0], ctx.Remote.Socket[1])
		respPayload = buildSMBErrorResponse(packet.Payload, smbStatusNotSupported)
	}

	if respPayload == nil {
		return nil, nil
	}
	return &netbiosproto.SessionPacket{
		Type:    netbiosproto.SessionMessage,
		Payload: respPayload,
	}, nil
}
