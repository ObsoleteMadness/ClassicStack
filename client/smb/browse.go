package smb

import (
	"fmt"

	"github.com/ObsoleteMadness/ClassicStack/client"
	clientlink "github.com/ObsoleteMadness/ClassicStack/client/link"
	"github.com/ObsoleteMadness/ClassicStack/client/uri"
	protocol "github.com/ObsoleteMadness/ClassicStack/core/protocol/smb"
)

// browse.go implements the SMB server-root listing: `csfs ls smb://server/` (no share)
// connects to the server's IPC$ pipe and runs a RAP NetShareEnum, returning the server's
// share list — the SMB analogue of AFP's server-root volume browse. When a share IS named
// in the URI the ordinary connect path mounts it instead.

// ServerListing is the result of a server-root browse: the server label, negotiated
// dialect/security/capabilities, and its shares.
type ServerListing struct {
	ServerName       string
	Dialect          string
	Capabilities     uint32
	UserSecurity     bool
	EncryptPasswords bool
	Guest            bool
	Shares           []Share
}

// BrowseServer is one server a master browser reported in its browse list (RAP
// NetServerEnum2): the server name, the SV_TYPE_* bits it advertises, and its comment.
type BrowseServer struct {
	Name    string
	Type    uint32
	Comment string
}

// EnumServers connects to master (a master or backup browser named by masterName) over the
// carrier the opener selects, binds its IPC$ pipe, and runs a RAP NetServerEnum2 for the
// authoritative browse list of servers it knows in workgroup (workgroup "" = the master's
// own domain). It is the session half of a "net view": the datagram probe finds the master
// (client/netbios.FindMaster), and this asks that master who is on the workgroup — far more
// than a broadcast solicit sees, since ordinary hosts announce only to the master. user /
// pass authenticate the IPC$ session (typically empty for an anonymous browse).
func EnumServers(opener *clientlink.Opener, masterName, workgroup, user, pass string) ([]BrowseServer, error) {
	if opener == nil {
		return nil, fmt.Errorf("smb: enum servers: an opener is required")
	}
	tr, err := openTransport(opener, masterName)
	if err != nil {
		return nil, fmt.Errorf("smb: open transport: %w", err)
	}
	sess, err := OpenIPC(tr, DialParams{ServerName: masterName, User: user, Password: pass})
	if err != nil {
		_ = tr.Close()
		return nil, err
	}
	defer sess.Close()

	servers, err := sess.EnumServers(protocol.ServerTypeAll, workgroup)
	if err != nil {
		return nil, err
	}
	out := make([]BrowseServer, 0, len(servers))
	for _, s := range servers {
		out = append(out, BrowseServer{Name: s.Name, Type: s.Type, Comment: s.Comment})
	}
	return out, nil
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

	out := ServerListing{
		ServerName:       target.Server,
		Dialect:          sess.Dialect(),
		Capabilities:     sess.Capabilities(),
		UserSecurity:     sess.UserSecurity(),
		EncryptPasswords: sess.EncryptPasswords(),
		Guest:            sess.Guest(),
	}
	for _, sh := range shares {
		out.Shares = append(out.Shares, Share{
			Name:    sh.Name,
			IsIPC:   sh.Type == protocol.ShareTypeIPC,
			Comment: sh.Comment,
		})
	}
	return out, nil
}
