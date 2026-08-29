package finder

// DarwinVolumesDir is the parent of macFUSE volume mountpoints. mount_macfuse
// (setuid, since macFUSE 3.5) creates a missing direct child of this directory;
// a regular user cannot mkdir there (/Volumes is root:wheel 0755 since Sierra).
const DarwinVolumesDir = "/Volumes"

// VolumeInfo.Kind values (local live share vs remote file-sharing scheme).
const (
	KindLocal    = "local"
	KindAFP      = "afp"
	KindSMB      = "smb"
	KindNCP      = "ncp"
	KindEtherDFS = "etherdfs"
)

// VolumeInfo.Transport values for remote clients (sidebar badges).
const (
	TransportTCP     = "tcp"
	TransportDDP     = "ddp"
	TransportIPX     = "ipx"
	TransportNetBEUI = "netbeui"
	TransportNBP     = "nbp"
	TransportEDFS    = "etherdfs"
)
