package zip

const (
	SAS               = 6
	DDPType           = 6
	FuncQuery         = 1
	FuncReply         = 2
	FuncGetNetInfoReq = 5
	FuncGetNetInfoRep = 6
	FuncExtReply      = 8

	GetNetInfoZoneInvalid  = 0x80
	GetNetInfoUseBroadcast = 0x40
	GetNetInfoOnlyOneZone  = 0x20

	ATPDDPType          = 3
	ATPFuncTReq         = 0x40
	ATPFuncTResp        = 0x80
	ATPEOM              = 0x10
	ATPGetMyZone        = 7
	ATPGetZoneList      = 8
	ATPGetLocalZoneList = 9
)
