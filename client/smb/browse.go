package smb

import (
	"fmt"

	"github.com/ObsoleteMadness/ClassicStack/client"
	"github.com/ObsoleteMadness/ClassicStack/client/uri"
	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// browse.go implements the SMB server-root listing: `csfs ls smb://server/` (no share)
// connects to the server's IPC$ pipe and runs a RAP NetShareEnum, returning the server's
// share list — the SMB analogue of AFP's server-root volume browse. When a share IS named
// in the URI the ordinary connect path mounts it instead.

// ServerListing is the result of a server-root browse: the server label and its shares.
type ServerListing struct {
	ServerName string
	Dialect    string
	Shares     []Share
}

// Share is one enumerated share: its name, whether it is the IPC$ pipe or a disk tree,
// and the operator remark/comment.
type Share struct {
	Name    string
	IsIPC   bool
	Comment string
}

// Browse connects to target's server (NEGOTIATE + SESSION_SETUP), binds the IPC$ pipe,
// runs a RAP NetShareEnum, and returns the share list. It is used for a URI that names a
// server but no share. The transport is opened from opts.Opener exactly as connect does.
func Browse(target uri.Target, opts client.Options) (ServerListing, error) {
	if opts.Opener == nil {
		return ServerListing{}, fmt.Errorf("smb: browse: an opener is required")
	}
	tr, err := openTransport(opts.Opener, target.Server)
	if err != nil {
		return ServerListing{}, fmt.Errorf("smb: open transport: %w", err)
	}
	sess, err := OpenIPC(tr, DialParams{
		ServerName: target.Server,
		User:       target.User,
		Password:   target.Pass,
	})
	if err != nil {
		_ = tr.Close()
		return ServerListing{}, err
	}
	defer sess.Close()

	shares, err := sess.EnumShares()
	if err != nil {
		return ServerListing{}, err
	}

	out := ServerListing{ServerName: target.Server, Dialect: sess.Dialect()}
	for _, sh := range shares {
		out.Shares = append(out.Shares, Share{
			Name:    sh.Name,
			IsIPC:   sh.Type == protocol.ShareTypeIPC,
			Comment: sh.Comment,
		})
	}
	return out, nil
}
