package ncp

// names.go carries human-readable names for the NCP function codes (and the
// 0x16/0x17/0x57 subfunction codes) so the diagnostic logs read
// `fn="0x17/0x16 Get Connection Information (old)"` instead of bare hex. The
// tables include well-known functions this server does NOT implement (burst
// mode, bindery property writes, trustees, queues) precisely because those are
// the codes that show up in "NCP request failed" lines. Names follow the
// Novell NCP call names (as used by mars_nwe and the Wireshark NCP dissector).

import (
	ncpproto "github.com/ObsoleteMadness/ClassicStack/core/protocol/ncp"
)

// fnNames names the top-level NCP function codes.
var fnNames = map[uint8]string{
	0x01:                  "File Set Lock",
	0x02:                  "File Release Lock",
	fnLogFile:             "Log File",
	0x04:                  "Lock File Set",
	fnReleaseFile:         "Release File",
	fnReleaseFileSet:      "Release File Set",
	fnClearFile:           "Clear File",
	fnClearFileSet:        "Clear File Set",
	fnLogLogicalRecord:    "Log Logical Record",
	fnLogLogicalRecordSet: "Lock Logical Record Set",
	fnClearLogicalRecord:  "Clear Logical Record",
	0x0C:                  "Release Logical Record",
	fnReleaseLogRecordSet: "Release Logical Record Set",
	fnClearLogRecordSet:   "Clear Logical Record Set",
	0x0F:                  "Allocate Resource",
	0x10:                  "Deallocate Resource",
	0x11:                  "Print Services",
	fnGetVolInfoNumber:    "Get Volume Info with Number",
	fnGetStationNumber:    "Get Station Number",
	fnGetServerDateTime:   "Get File Server Date And Time",
	0x15:                  "Message Services",
	fnDirServices:         "Directory Services",
	fnConnBindery:         "Connection/Bindery Services",
	fnEndOfJob:            "End Of Job",
	fnLogout:              "Logout",
	fnLogPhysicalRecord:   "Log Physical Record",
	0x1B:                  "Lock Physical Record Set",
	0x1C:                  "Release Physical Record",
	0x1D:                  "Release Physical Record Set",
	fnClearPhysicalRecord: "Clear Physical Record",
	fnClearPhysRecordSet:  "Clear Physical Record Set",
	0x20:                  "Semaphore Services",
	fnNegotiateBuffer:     "Negotiate Buffer Size",
	fnTTS:                 "TTS Services",
	fnAFP:                 "AFP Services",
	fnCommitFile:          "Commit File",
	0x3C:                  "Set File Extended Attributes",
	fnCommitFile2:         "Commit File (old)",
	fnFileSearchInit:      "File Search Initialize",
	fnFileSearchContinue:  "File Search Continue",
	fnSearchForFile:       "Search for a File",
	fnOpenForRead:         "Open File (old)",
	fnCloseFile:           "Close File",
	fnCreateFile:          "Create File",
	fnEraseFile:           "Erase File",
	fnRenameFile:          "Rename File",
	fnSetFileAttributes:   "Set File Attributes",
	fnGetFileSize:         "Get Current Size of File",
	fnReadFile:            "Read From A File",
	fnWriteFile:           "Write To A File",
	0x4A:                  "Copy From One File To Another",
	0x4B:                  "Set File Time Date Stamp",
	fnOpenFile:            "Open File",
	fnCreateNewFile:       "Create New File",
	fnNameSpace:           "Name Space Services",
	0x58:                  "Extended Attribute Services",
	0x5C:                  "Socket Services (SPX)",
	0x61:                  "Get Big Packet NCP Max Packet Size",
	0x65:                  "Packet Burst Connection Request",
	0x68:                  "NDS Services",
	0x72:                  "Packet Burst Transaction",
}

// sf17Names names the 0x17 connection/bindery subfunctions.
var sf17Names = map[uint8]string{
	0x01:                 "Change User Password (old)",
	0x02:                 "Set Connection Password",
	0x0A:                 "Enter Login Area",
	sf17GetServerInfo:    "Get File Server Information",
	0x12:                 "Get Network Serial Number",
	sf17GetInetAddrOld:   "Get Connection Internet Address (old)",
	sf17LoginUnencrypted: "Login Object (unencrypted)",
	sf17GetObjConnList:   "Get Object Connection List (old)",
	sf17GetConnInfoOld:   "Get Connection Information (old)",
	sf17GetLoginKey:      "Get Login Key",
	sf17LoginEncrypted:   "Keyed Login",
	0x19:                 "Get User Restriction (accounting)",
	sf17GetInetAddr:      "Get Connection Internet Address",
	sf17GetObjConnList2:  "Get Object Connection List",
	sf17GetConnInfo:      "Get Connection Information",
	0x1D:                 "Get Connection Task Information",
	0x32:                 "Create Bindery Object",
	0x33:                 "Delete Bindery Object",
	0x34:                 "Rename Bindery Object",
	sf17GetObjectID:      "Get Bindery Object ID",
	sf17GetObjectName:    "Get Bindery Object Name",
	sf17ScanObject:       "Scan Bindery Object",
	0x38:                 "Change Bindery Object Security",
	0x39:                 "Create Property",
	0x3A:                 "Delete Property",
	0x3B:                 "Change Property Security",
	0x3C:                 "Scan Property",
	0x3D:                 "Read Property Value",
	0x3E:                 "Write Property Value",
	0x3F:                 "Verify Bindery Object Password",
	0x40:                 "Change Bindery Object Password",
	0x41:                 "Add Bindery Object To Set",
	0x42:                 "Delete Bindery Object From Set",
	0x43:                 "Is Bindery Object In Set",
	0x44:                 "Close Bindery",
	0x45:                 "Open Bindery",
	sf17GetBinderyAccess: "Get Bindery Access Level",
	0x47:                 "Scan Bindery Object Trustee Paths",
	0x48:                 "Get Bindery Object Access Level",
	0x49:                 "Is Station A Manager",
	0x4A:                 "Keyed Verify Password",
	0x4B:                 "Keyed Change Password",
	0x4C:                 "List Relations Of An Object",
}

// sf16Names names the 0x16 directory-services subfunctions.
var sf16Names = map[uint8]string{
	sf16SetDirHandle:    "Set Directory Handle",
	sf16GetDirPath:      "Get Directory Path",
	sf16ScanDirInfo:     "Scan Directory Information",
	sf16GetEffDirRights: "Get Effective Directory Rights",
	0x04:                "Modify Maximum Rights Mask",
	sf16GetVolumeNumber: "Get Volume Number",
	sf16GetVolumeName:   "Get Volume Name",
	sf16CreateDir:       "Create Directory",
	sf16DeleteDir:       "Delete Directory",
	0x0C:                "Scan Directory For Trustees",
	0x0D:                "Add Trustee To Directory",
	0x0E:                "Delete Trustee From Directory",
	sf16RenameDir:       "Rename Directory",
	0x10:                "Purge Erased Files (old)",
	0x11:                "Restore Erased File (old)",
	sf16AllocPermDir:    "Allocate Permanent Directory Handle",
	sf16AllocTempDir:    "Allocate Temporary Directory Handle",
	sf16DeallocDirHdl:   "Deallocate Directory Handle",
	sf16GetVolumeInfo:   "Get Volume Info with Handle",
	sf16AllocSpecialDir: "Allocate Special Temporary Directory Handle",
	0x17:                "Set Directory Disk Space Restriction",
	0x18:                "Get Directory Disk Space Restriction",
	sf16SetDirInfo:      "Set Directory Information",
	0x1E:                "Scan A Directory",
	0x1F:                "Get Directory Entry",
	sf16ScanVolRestrict: "Scan Volume's User Disk Restrictions",
	0x21:                "Add User Disk Space Restriction",
	0x22:                "Remove User Disk Space Restrictions",
	0x25:                "Set Data Stream",
	0x26:                "Get Data Stream Info",
	sf16GetVolPurgeInfo: "Get Volume and Purge Information",
	sf16GetDirInfo:      "Get Directory Information",
	0x2E:                "Scan Salvageable Files",
	0x2F:                "Recover Salvageable File",
	0x30:                "Purge Salvageable File",
	0x33:                "Get Name Space Directory Entry",
}

// sf57Names names the 0x57 name-space subfunctions.
var sf57Names = map[uint8]string{
	ncpproto.NSGetNamespaceInfo: "Get Name Space Information",
	ncpproto.NSOpenCreate:       "Open/Create File or Subdirectory",
	ncpproto.NSInitSearch:       "Initialize Search",
	ncpproto.NSSearch:           "Search for File or Subdirectory",
	0x04:                        "Rename Or Move",
	0x05:                        "Scan File or Directory for Trustees",
	ncpproto.NSObtainInfo:       "Obtain File or Subdirectory Information",
	0x07:                        "Modify File or Subdirectory DOS Information",
	0x08:                        "Delete a File or Subdirectory",
	0x09:                        "Set Short Directory Handle",
	0x0C:                        "Allocate Short Directory Handle",
	ncpproto.NSGenDirBase:       "Generate Directory Base and Volume Number",
	ncpproto.NSGetLoadedList:    "Get Name Spaces Loaded",
	0x19:                        "Set Name Space Information",
	0x1A:                        "Get Huge Name Space Information",
	0x1C:                        "Get Full Path String",
}

// fnString renders a request's function — and, for the multiplexed families
// (0x16/0x17 subfn at body[2], 0x57 subfn at body[0]), its subfunction — with
// its call name for the diagnostic logs, e.g. "0x17/0x16 Get Connection
// Information (old)". Unknown codes stay bare hex.
func fnString(req *ncpproto.RequestHeader) string {
	fn := req.Function
	switch fn {
	case fnDirServices, fnConnBindery:
		if len(req.Body) >= 3 {
			sub := sf16Names
			if fn == fnConnBindery {
				sub = sf17Names
			}
			return withName(hex8(fn)+"/"+hex8(req.Body[2]), sub[req.Body[2]])
		}
	case fnNameSpace:
		if len(req.Body) >= 1 {
			return withName(hex8(fn)+"/"+hex8(req.Body[0]), sf57Names[req.Body[0]])
		}
	}
	return withName(hex8(fn), fnNames[fn])
}

// withName appends the call name to the hex code when one is known.
func withName(code, name string) string {
	if name == "" {
		return code
	}
	return code + " " + name
}
