package ncp

// functions.go holds the NCP function codes and multiplexed subfunction codes as
// WIRE constants — the values that travel in the NCP request, independent of either
// direction. The server engine (core/service/ncp/dispatch.go) keeps its own
// unexported copy for its dispatch switch; these exported names are what the
// client-direction Requester (client.go / clientfileops.go) stamps into requests and
// what a test asserts against. Named from mars_nwe nwconn.c; values are the on-wire
// function codes.
//
// Reference: Novell NCP function codes; mars_nwe / ncpfs (CLAUDE.md #7).

// NCP function codes (the first body byte of a TypeRequest packet).
const (
	fnLogFile           uint8 = 0x03 // log/lock a file
	fnReleaseFile       uint8 = 0x05 // release a file lock
	fnGetFileSize       uint8 = 0x47 // seek to end, return file size
	fnReadFile          uint8 = 0x48 // read file
	fnWriteFile         uint8 = 0x49 // write file
	fnOpenFile          uint8 = 0x4C // open file
	fnCreateFile        uint8 = 0x43 // create file, overwrite if exists
	fnCloseFile         uint8 = 0x42 // close file
	fnEraseFile         uint8 = 0x44 // erase/delete file
	fnRenameFile        uint8 = 0x45 // rename file
	fnSearchForFile     uint8 = 0x40 // Search for a File (FCB-era one-call-per-entry)
	fnFileSearchInit    uint8 = 0x3E // File Search Initialize (62) — NW 3.x dir scan setup
	fnFileSearchCont    uint8 = 0x3F // File Search Continue (63) — NW 3.x dir scan paging
	fnDirServices       uint8 = 0x16 // multiplexed dir-handle / volume services
	fnConnBindery       uint8 = 0x17 // multiplexed connection/bindery services
	fnGetServerDateTime uint8 = 0x14 // get file-server date/time
	fnNegotiateBuffer   uint8 = 0x21 // Negotiate Buffer Size (max read/write packet)
)

// Subfunctions of fnConnBindery (0x17) the client uses. The encrypted-login trio
// (GetLoginKey / GetBinderyObjectID / LoginEncrypted) is the classic NetWare 3.x bindery
// login a default-configured real server requires; the cleartext login (0x14) is the
// fallback our own server / mars_nwe also accept. NDS (NetWare 4+) login is NOT handled.
const (
	sf17GetServerInfo      uint8 = 0x11 // Get File Server Information
	sf17LoginUnencrypted   uint8 = 0x14 // Login To File Server (cleartext)
	sf17GetLoginKey        uint8 = 0x17 // Get Login Key (8-byte challenge) — 23 decimal
	sf17LoginEncrypted     uint8 = 0x18 // Login Object (Encrypted) — 24 decimal
	sf17GetBinderyObjectID uint8 = 0x35 // Get Bindery Object ID (name → object id) — 53
)

// Subfunctions of fnDirServices (0x16) the client uses.
const (
	sf16GetVolumeName   uint8 = 0x06 // Get Volume Name (by number) — for volume enumeration
	sf16GetVolumeNumber uint8 = 0x05 // Get Volume Number (by name)
	sf16CreateDir       uint8 = 0x0A // Create Directory
	sf16DeleteDir       uint8 = 0x0B // Delete Directory
	sf16AllocPermDir    uint8 = 0x12 // Allocate Permanent Directory Handle
	sf16DeallocDirHdl   uint8 = 0x14 // Deallocate Directory Handle
	sf16GetVolumeInfo   uint8 = 0x15 // Get Volume Info with Handle
)

// maxVolumeSlots is the number of volume-number slots a NetWare 3.x server exposes
// (0..63); a browse enumerates them via Get Volume Name.
const maxVolumeSlots = 64
