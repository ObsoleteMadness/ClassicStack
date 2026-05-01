package zip

import pzip "github.com/ObsoleteMadness/ClassicStack/protocol/zip"

// Wire constants re-exported from protocol/zip.
const (
	SAS               = pzip.SAS
	DDPType           = pzip.DDPType
	FuncQuery         = pzip.FuncQuery
	FuncReply         = pzip.FuncReply
	FuncGetNetInfoReq = pzip.FuncGetNetInfoReq
	FuncGetNetInfoRep = pzip.FuncGetNetInfoRep
	FuncExtReply      = pzip.FuncExtReply

	GetNetInfoZoneInvalid  = pzip.GetNetInfoZoneInvalid
	GetNetInfoUseBroadcast = pzip.GetNetInfoUseBroadcast
	GetNetInfoOnlyOneZone  = pzip.GetNetInfoOnlyOneZone

	ATPDDPType          = pzip.ATPDDPType
	ATPFuncTReq         = pzip.ATPFuncTReq
	ATPFuncTResp        = pzip.ATPFuncTResp
	ATPEOM              = pzip.ATPEOM
	ATPGetMyZone        = pzip.ATPGetMyZone
	ATPGetZoneList      = pzip.ATPGetZoneList
	ATPGetLocalZoneList = pzip.ATPGetLocalZoneList
)
