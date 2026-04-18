package afp

// Debug enables debug logging for AFP server.
var Debug bool = false

type Volume struct {
	Config VolumeConfig
	ID     uint16
}

const (
	Version11 = "AFPVersion 1.1"
	Version20 = "AFPVersion 2.0"
	Version21 = "AFPVersion 2.1"
)

const (
	UAMNoUserAuthent  = "No User Authent"
	UAMCleartxtPasswd = "Cleartxt Passwrd"
)

const (
	NoErr           int32 = 0
	ErrAccessDenied int32 = -5000 // kFPAccessDenied
	ErrAuthContinue int32 = -5001 // kFPAuthContinue
	ErrBadUAM       int32 = -5002 // kFPBadUAM
	ErrBadVersNum   int32 = -5003 // kFPBadVersNum
	// An attempt was made to retrieve a parameter that cannot be obtained with this call.
	ErrBitmapErr    int32 = -5004 // kFPBitmapErr
	ErrCantMove     int32 = -5005 // kFPCantMove
	ErrDenyConflict int32 = -5006 // kFPDenyConflict
	ErrDirNotEmpty  int32 = -5007 // kFPDirNotEmpty
	// No more space exists on the volume
	ErrDiskFull         int32 = -5008 // kFPDiskFull
	ErrEOFErr           int32 = -5009 // kFPEOFErr
	ErrFileBusy         int32 = -5010 // kFPFileBusy
	ErrFlatVol          int32 = -5011 // kFPFlatVol
	ErrItemNotFound     int32 = -5012 // kFPItemNotFound
	ErrLockErr          int32 = -5013 // kFPLockErr
	ErrMiscErr          int32 = -5014 // kFPMiscErr
	ErrNoMoreLocks      int32 = -5015 // kFPNoMoreLocks
	ErrNoServer         int32 = -5016 // kFPNoServer
	ErrObjectExists     int32 = -5017 // kFPObjectExists
	ErrObjectNotFound   int32 = -5018 // kFPObjectNotFound
	ErrParamErr         int32 = -5019 // kFPParamErr
	ErrRangeNotLocked   int32 = -5020 // kFPRangeNotLocked
	ErrRangeOverlap     int32 = -5021 // kFPRangeOverlap
	ErrSessClosed       int32 = -5022 // kFPSessClosed
	ErrUserNotAuth      int32 = -5023 // kFPUserNotAuth
	ErrCallNotSupported int32 = -5024 // kFPCallNotSupported
	ErrObjectTypeErr    int32 = -5025 // kFPObjectTypeErr
	ErrTooManyFilesOpen int32 = -5026 // kFPTooManyFilesOpen
	ErrServerGoingDown  int32 = -5027 // kFPServerGoingDown
	ErrCantRename       int32 = -5028 // kFPCantRename
	ErrDirNotFound      int32 = -5029 // kFPDirNotFound
	ErrIconTypeError    int32 = -5030 // kFPIconTypeError
	ErrVolLocked        int32 = -5031 // kFPVolLocked
	ErrObjectLocked     int32 = -5032 // kFPObjectLocked

	// Backward-compatible alias retained for existing code/tests.
	ErrDFull int32 = ErrDiskFull
)

// FPCreateFile CreateFlag constants (wire CreateFlag byte).
// Bit 7 selects hard-create (1) vs soft-create (0).
const (
	// Soft create: no bits set.
	FPCreateFileFlagSoftCreate uint8 = 0
	// Hard create: bit 7 set (1 << 7 == 0x80).
	FPCreateFileFlagHardCreate uint8 = 1 << 7
)

const (
	FileBitmapAttributes  = 1 << 0
	FileBitmapParentDID   = 1 << 1
	FileBitmapCreateDate  = 1 << 2
	FileBitmapModDate     = 1 << 3
	FileBitmapBackupDate  = 1 << 4
	FileBitmapFinderInfo  = 1 << 5
	FileBitmapLongName    = 1 << 6
	FileBitmapShortName   = 1 << 7
	FileBitmapFileNum     = 1 << 8
	FileBitmapDataForkLen = 1 << 9
	FileBitmapRsrcForkLen = 1 << 10
	FileBitmapProDOSInfo  = 1 << 13

	DirBitmapAttributes     = 1 << 0
	DirBitmapParentDID      = 1 << 1
	DirBitmapCreateDate     = 1 << 2
	DirBitmapModDate        = 1 << 3
	DirBitmapBackupDate     = 1 << 4
	DirBitmapFinderInfo     = 1 << 5
	DirBitmapLongName       = 1 << 6
	DirBitmapShortName      = 1 << 7
	DirBitmapDirID          = 1 << 8
	DirBitmapOffspringCount = 1 << 9
	DirBitmapOwnerID        = 1 << 10
	DirBitmapGroupID        = 1 << 11
	DirBitmapAccessRights   = 1 << 12
	DirBitmapProDOSInfo     = 1 << 13

	VolBitmapAttributes    = 1 << 0
	VolBitmapSignature     = 1 << 1
	VolBitmapCreateDate    = 1 << 2
	VolBitmapModDate       = 1 << 3
	VolBitmapBackupDate    = 1 << 4
	VolBitmapVolID         = 1 << 5
	VolBitmapBytesFree     = 1 << 6
	VolBitmapBytesTotal    = 1 << 7
	VolBitmapName          = 1 << 8
	VolBitmapExtBytesFree  = 1 << 9
	VolBitmapExtBytesTotal = 1 << 10
	VolBitmapBlockSize     = 1 << 11
)

const (
	SupportedVolBitmap = VolBitmapAttributes | VolBitmapSignature | VolBitmapCreateDate |
		VolBitmapModDate | VolBitmapBackupDate | VolBitmapVolID | VolBitmapBytesFree |
		VolBitmapBytesTotal | VolBitmapName | VolBitmapExtBytesFree | VolBitmapExtBytesTotal |
		VolBitmapBlockSize

	SupportedFileBitmap = FileBitmapAttributes | FileBitmapParentDID | FileBitmapCreateDate |
		FileBitmapModDate | FileBitmapDataForkLen | FileBitmapFileNum | FileBitmapLongName

	SupportedDirBitmap = DirBitmapAttributes | DirBitmapParentDID | DirBitmapCreateDate |
		DirBitmapModDate | DirBitmapDirID | DirBitmapLongName
)

// AFP volume signature values (Table 75).
const (
	AFPVolumeTypeFlat          uint16 = 1 // Flat (no directories)
	AFPVolumeTypeFixedDirID    uint16 = 2 // Fixed Directory ID
	AFPVolumeTypeVariableDirID uint16 = 3 // Variable Directory ID
)

// Volume attribute flags returned in the Attributes field when
// VolBitmapAttributes is requested. Bits are measured in a 16-bit
// attributes word; only the ReadOnly flag (bit 0) is defined here.
const (
	// VolAttrReadOnly indicates the volume is read-only (bit 0).
	VolAttrReadOnly uint16 = 1 << 0
)

// File and directory attribute flags returned in the Attributes field
// when FileBitmapAttributes or DirBitmapAttributes is requested.
// Per AFP 2.x specification, these are bit positions in a 16-bit attributes word.
const (
	// File attributes (per AFP 2.x §5.1.1)
	FileAttrInvisible     uint16 = 1 << 0  // Invisible
	FileAttrMultiUser     uint16 = 1 << 1  // MultiUser
	FileAttrSystem        uint16 = 1 << 2  // System
	FileAttrDAlreadyOpen  uint16 = 1 << 3  // Data fork already open
	FileAttrRAlreadyOpen  uint16 = 1 << 4  // Resource fork already open
	FileAttrWriteInhibit  uint16 = 1 << 5  // ReadOnly/WriteInhibit (AFP 2.0)
	FileAttrBackupNeeded  uint16 = 1 << 6  // BackupNeeded
	FileAttrRenameInhibit uint16 = 1 << 7  // RenameInhibit
	FileAttrDeleteInhibit uint16 = 1 << 8  // DeleteInhibit
	FileAttrCopyProtect   uint16 = 1 << 10 // CopyProtect
	FileAttrSetClear      uint16 = 1 << 15 // Set/Clear (used in FPSetFileDirParms)

	// Directory attributes (per AFP 2.x §5.1.2)
	DirAttrInvisible     uint16 = 1 << 0 // Invisible
	DirAttrSystem        uint16 = 1 << 2 // System
	DirAttrBackupNeeded  uint16 = 1 << 6 // BackupNeeded
	DirAttrRenameInhibit uint16 = 1 << 7 // RenameInhibit
	DirAttrDeleteInhibit uint16 = 1 << 8 // DeleteInhibit
)

// PathType constants indicate whether a Pathname is composed of long or short names.
const (
	PathTypeShortNames uint8 = 1 // Short names (8.3 or less)
	PathTypeLongNames  uint8 = 2 // Long names (up to 31 bytes)
	PathTypeUTF8       uint8 = 3 // UTF-8 encoded names (up to 255 bytes)
)

const (
	// Context comments preserved as aliases where the semantic note is useful.
	ErrObjectExistsSoftCreate int32 = ErrObjectExists // soft-create failed because object already exists
)
