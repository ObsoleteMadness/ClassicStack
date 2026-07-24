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
	fnDirServices       uint8 = 0x16 // multiplexed dir-handle / volume services
	fnConnBindery       uint8 = 0x17 // multiplexed connection/bindery services
	fnGetServerDateTime uint8 = 0x14 // get file-server date/time
	fnNegotiateBuffer   uint8 = 0x21 // Negotiate Buffer Size (max read/write packet)
)

// Subfunctions of fnConnBindery (0x17) the client uses.
const (
	sf17GetServerInfo    uint8 = 0x11 // Get File Server Information
	sf17LoginUnencrypted uint8 = 0x14 // Login To File Server (cleartext)
)

// Subfunctions of fnDirServices (0x16) the client uses.
const (
	sf16GetVolumeNumber uint8 = 0x05 // Get Volume Number (by name)
	sf16CreateDir       uint8 = 0x0A // Create Directory
	sf16DeleteDir       uint8 = 0x0B // Delete Directory
	sf16AllocPermDir    uint8 = 0x12 // Allocate Permanent Directory Handle
	sf16DeallocDirHdl   uint8 = 0x14 // Deallocate Directory Handle
	sf16GetVolumeInfo   uint8 = 0x15 // Get Volume Info with Handle
)
