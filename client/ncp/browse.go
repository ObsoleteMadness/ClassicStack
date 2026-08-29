package ncp

import (
	"github.com/ObsoleteMadness/ClassicStack/client"
	"github.com/ObsoleteMadness/ClassicStack/client/uri"
)

// browse.go serves the NCP "server root" — what the client shows when a URI names a
// server but no volume (ncp://SERVER/). It logs in (GUEST when no user is given) and
// enumerates the server's mounted volumes via Get Volume Name, mirroring the AFP volume
// list and SMB share list. The CLI prints this instead of failing Get Volume Number with
// an empty volume name.

// ServerListing is the result of browsing an NCP server root: the server name (from the
// URI, as NCP has no cheap "server info" call the browse needs), whether keyed bindery
// login was used, and the mounted volumes the logged-in identity can see, each mountable
// as ncp://SERVER/<volume>.
type ServerListing struct {
	ServerName string
	Encrypted  bool // Get Login Key succeeded and keyed login was used
	Volumes    []string
}

// Browse logs into the server named by target (ignoring target.Volume) and returns its
// mounted-volume list — the server-root view for ncp://SERVER/. It owns the whole session
// for the call and tears it down before returning, so the caller manages no connection.
func Browse(target uri.Target, opts client.Options) (ServerListing, error) {
	tr, err := openTransport(opts.Opener, target.Server)
	if err != nil {
		return ServerListing{}, err
	}
	sess, err := attach(tr, DialParams{User: target.User, Password: target.Pass})
	if err != nil {
		_ = tr.Close()
		return ServerListing{}, err
	}
	defer func() {
		sess.destroyConnection()
		_ = tr.Close()
	}()

	return ServerListing{
		ServerName: target.Server,
		Encrypted:  sess.encrypted,
		Volumes:    sess.ListVolumes(),
	}, nil
}
