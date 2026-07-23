package afp

import (
	"github.com/ObsoleteMadness/ClassicStack/client"
	"github.com/ObsoleteMadness/ClassicStack/client/uri"
	proto "github.com/ObsoleteMadness/ClassicStack/core/protocol/afp"
)

// browse.go serves the AFP "server root" — what a client shows when a URI names a server
// but no volume (afp://server/). It logs in and calls FPGetSrvrParms to list the
// volumes the logged-in identity may see, alongside the server's own FPGetSrvrInfo
// (machine type, AFP versions, UAMs). The CLI prints this instead of failing an
// FPOpenVol with an empty volume name (kFPParamErr).

// Volume is one advertised AFP volume: its name and whether it has a password / is
// configured (the FPGetSrvrParms flag bits), plus the ready-to-use AFP URI to mount it.
type Volume struct {
	Name        string
	HasPassword bool // FPGetSrvrParms flag bit 0 (volume has a volume password)
	HasConfig   bool // FPGetSrvrParms flag bit 1 (volume has a configurator / is a server config)
}

// ServerListing is the result of browsing a server root: the server's own info and the
// volumes it advertises to the logged-in identity.
type ServerListing struct {
	// ServerName / MachineType / AFPVersions / UAMs come from FPGetSrvrInfo (empty when
	// the server did not answer GetStatus).
	ServerName  string
	MachineType string
	AFPVersions []string
	UAMs        []string
	// Volumes are the volumes FPGetSrvrParms returned.
	Volumes []Volume
}

// FPGetSrvrParms volume flag bits (Inside AppleTalk: Networking, "GetSrvrParms").
const (
	volFlagHasPassword uint8 = 0x01 // the volume has a volume password
	volFlagHasConfig   uint8 = 0x02 // the volume carries server configuration
)

// Browse logs into the server named by target (ignoring target.Volume) and returns its
// server info plus the advertised volume list — the server-root view for
// afp://server/. It owns the whole session for the call and tears it down before
// returning, so the caller need not manage a connection.
func Browse(target uri.Target, opts client.Options) (ServerListing, error) {
	ep, sess, srvInfo, err := dialAndLogin(target, opts)
	if err != nil {
		return ServerListing{}, err
	}
	defer func() {
		_ = sess.Close()
		_ = ep.Close()
	}()

	listing := ServerListing{
		ServerName:  srvInfo.ServerName,
		MachineType: srvInfo.MachineType,
		AFPVersions: srvInfo.AFPVersions,
		UAMs:        srvInfo.UAMs,
	}

	body, result, err := sess.Command(proto.GetSrvrParmsRequest{}.Marshal())
	if err != nil {
		return listing, err
	}
	if result != proto.NoErr {
		return listing, afpError("FPGetSrvrParms", result)
	}
	parms, ok := proto.ParseGetSrvrParmsReply(body)
	if !ok {
		return listing, errMalformed("FPGetSrvrParms reply")
	}
	for _, v := range parms.Volumes {
		listing.Volumes = append(listing.Volumes, Volume{
			Name:        v.Name,
			HasPassword: v.Flags&volFlagHasPassword != 0,
			HasConfig:   v.Flags&volFlagHasConfig != 0,
		})
	}
	return listing, nil
}
