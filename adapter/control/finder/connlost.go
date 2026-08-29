package finder

import (
	"errors"
	"net"

	"github.com/ObsoleteMadness/ClassicStack/client/atalk"
	etherdfsclient "github.com/ObsoleteMadness/ClassicStack/client/etherdfs"
	ncpclient "github.com/ObsoleteMadness/ClassicStack/client/ncp"
	smbclient "github.com/ObsoleteMadness/ClassicStack/client/smb"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

// isConnectionLost reports whether err indicates the underlying client transport
// to a remote server died (peer gone, socket reset/closed) rather than an
// ordinary protocol-level failure (not found, permission denied, short read at
// end of a fork — client/afp's fork reader deliberately returns io.EOF there, so
// plain io.EOF is NOT treated as connection loss). net.Error covers the raw
// socket failures (reset, broken pipe, "use of closed network connection") that
// have no package-specific sentinel.
func isConnectionLost(err error) bool {
	if err == nil {
		return false
	}
	switch {
	case errors.Is(err, atalk.ErrATPTimeout),
		errors.Is(err, smbclient.ErrTransportClosed),
		errors.Is(err, smbclient.ErrNBIPXSessionEnded),
		errors.Is(err, ncpclient.ErrTransportClosed),
		errors.Is(err, etherdfsclient.ErrTransportClosed),
		errors.Is(err, net.ErrClosed):
		return true
	}
	var netErr net.Error
	return errors.As(err, &netErr)
}

// InvalidateOnError removes sessionID (and unmounts any host FUSE/WinFsp mount
// riding it) when err shows the client connection backing it has died, so a dead
// session does not linger in GET /finder/mounted after the peer is gone. Reports
// whether it invalidated the session.
func (s *Service) InvalidateOnError(sessionID string, err error) bool {
	if sessionID == "" || !isConnectionLost(err) {
		return false
	}
	if closeErr := s.CloseSession(sessionID); closeErr != nil {
		return false
	}
	s.log.Log2(log.Warn, "finder: connection lost, session removed",
		log.Str("session", sessionID), log.Str("err", err.Error()))
	return true
}
