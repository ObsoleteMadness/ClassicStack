package smb

const (
	CommandCreateDirectory       = 0x00
	CommandDeleteDirectory       = 0x01
	CommandClose                 = 0x04
	CommandFlush                 = 0x05
	CommandDelete                = 0x06
	CommandRename                = 0x07
	CommandQueryInformation      = 0x08
	CommandSetInformation        = 0x09
	CommandRead                  = 0x0A
	CommandWrite                 = 0x0B
	CommandCheckDirectory        = 0x10
	CommandWriteRaw              = 0x1D
	CommandWriteComplete         = 0x20
	CommandSetInformation2       = 0x22
	CommandLockingAndX           = 0x24
	CommandTransaction           = 0x25
	CommandTransactionSecondary  = 0x26
	CommandEcho                  = 0x2B
	CommandOpenAndX              = 0x2D
	CommandReadAndX              = 0x2E
	CommandWriteAndX             = 0x2F
	CommandTransaction2          = 0x32
	CommandTransaction2Secondary = 0x33
	CommandFindClose2            = 0x34
	CommandTreeDisconnect        = 0x71
	CommandNegotiate             = 0x72
	CommandSessionSetupAndX      = 0x73
	CommandLogoffAndX            = 0x74
	CommandTreeConnectAndX       = 0x75
	CommandQueryInformationDisk  = 0x80
	CommandSearch                = 0x81
	CommandNtTransact            = 0xA0
	CommandNtTransactSecondary   = 0xA1
	CommandNtCreateAndX          = 0xA2
	CommandNtCancel              = 0xA4
	CommandNoAndXCommand         = 0xFF

	FileAttributeNormal    = 0x0000
	FileAttributeReadOnly  = 0x0001
	FileAttributeHidden    = 0x0002
	FileAttributeSystem    = 0x0004
	FileAttributeVolume    = 0x0008
	FileAttributeDirectory = 0x0010
	FileAttributeArchive   = 0x0020

	SearchAttributeReadOnly  = 0x0100
	SearchAttributeHidden    = 0x0200
	SearchAttributeSystem    = 0x0400
	SearchAttributeDirectory = 0x1000
	SearchAttributeArchive   = 0x2000
)
