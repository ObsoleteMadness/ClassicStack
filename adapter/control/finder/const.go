package finder

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
	TransportTCP  = "tcp"
	TransportDDP  = "ddp"
	TransportIPX  = "ipx"
	TransportNBP  = "nbp"
	TransportEDFS = "etherdfs"
)
