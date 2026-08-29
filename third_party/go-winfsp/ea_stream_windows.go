package winfsp

import (
	"syscall"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// This file adds named-stream (GetStreamInfo) and extended
// attribute (GetEa/SetEa) support on top of the base fork.
//
// It is ported from upstream github.com/winfsp/go-winfsp
// (filesystem_windows.go + gohelper_windows.go), adapted to
// this fork's FileSystemRef / delegate layout. ClassicStack
// uses these to expose native fork storage through streams
// and SMB EAs via the csmount tool.

const (
	streamInfoAlignment uint32 = 8
	eaInfoAlignment     uint32 = 4
	eaNameOffset               = 8 // FIELD_OFFSET(FILE_FULL_EA_INFORMATION, EaName)
)

func alignUpUint32(x, align uint32) uint32 {
	return (x + align - 1) & ^(align - 1)
}

// FileSystemAddStreamInfo adds named stream information to a
// buffer like FspFileSystemAddStreamInfo.
//
// Pass end=true to write the EOF marker for GetStreamInfo.
// bytesTransferred is both the current write offset and the
// updated number of bytes stored on success.
func FileSystemAddStreamInfo(
	name string,
	streamSize, streamAllocationSize uint64,
	end bool,
	buffer []byte,
	bytesTransferred *uint32,
) bool {
	offset := *bytesTransferred
	if end {
		const srcLen = 2
		if uint32(len(buffer)) < offset+srcLen {
			return false
		}
		buffer[offset] = 0
		buffer[offset+1] = 0
		*bytesTransferred = offset + srcLen
		return true
	}

	var utf16Len uint16
	for _, r := range name {
		switch utf16.RuneLen(r) {
		case 1:
			utf16Len++
		case 2:
			utf16Len += 2
		default:
			utf16Len++
		}
	}
	streamInfoSize := uint32(unsafe.Sizeof(FSP_FSCTL_STREAM_INFO{}))
	requiredSize := streamInfoSize + uint32(utf16Len)*SIZEOF_WCHAR
	dstLen := alignUpUint32(requiredSize, streamInfoAlignment)
	if uint32(len(buffer)) < offset+dstLen {
		return false
	}
	dst := buffer[offset : offset+dstLen]
	si := (*FSP_FSCTL_STREAM_INFO)(unsafe.Pointer(&dst[0]))
	si.Size = uint16(requiredSize)
	si.StreamSize = streamSize
	si.StreamAllocationSize = streamAllocationSize
	if utf16Len > 0 {
		utf16Buffer := unsafe.Slice(
			(*uint16)(unsafe.Pointer(&dst[streamInfoSize])),
			utf16Len,
		)
		utf16Index := 0
		for _, r := range name {
			switch utf16.RuneLen(r) {
			case 1:
				utf16Buffer[utf16Index] = uint16(r)
				utf16Index++
			case 2:
				r1, r2 := utf16.EncodeRune(r)
				utf16Buffer[utf16Index] = uint16(r1)
				utf16Buffer[utf16Index+1] = uint16(r2)
				utf16Index += 2
			default:
				utf16Buffer[utf16Index] = uint16(replacementChar)
				utf16Index++
			}
		}
	}
	*bytesTransferred = offset + dstLen
	return true
}

// FileSystemAddEa adds an extended attribute to a buffer like
// FspFileSystemAddEa.
//
// Pass end=true to finalize the EA list (clear the last
// NextEntryOffset). bytesTransferred is both the current write
// offset and the updated number of bytes stored on success.
func FileSystemAddEa(
	flags uint8,
	name string,
	value []byte,
	end bool,
	buffer []byte,
	bytesTransferred *uint32,
) bool {
	if end {
		if *bytesTransferred < eaNameOffset {
			return true
		}
		ea := (*FILE_FULL_EA_INFORMATION)(unsafe.Pointer(&buffer[0]))
		endOff := *bytesTransferred
		for {
			next := ea.NextEntryOffset
			if next == 0 {
				break
			}
			nextOff := uint32(uintptr(unsafe.Pointer(ea))-uintptr(unsafe.Pointer(&buffer[0]))) + next
			if nextOff+eaNameOffset > endOff {
				break
			}
			ea = (*FILE_FULL_EA_INFORMATION)(unsafe.Pointer(&buffer[nextOff]))
		}
		ea.NextEntryOffset = 0
		return true
	}

	nameBytes := []byte(name)
	if len(nameBytes) > 0xff {
		nameBytes = nameBytes[:0xff]
	}
	eaValueLength := len(value)
	if eaValueLength > 0xffff {
		eaValueLength = 0xffff
		value = value[:eaValueLength]
	}
	eaLen := uint32(eaNameOffset + len(nameBytes) + 1 + eaValueLength)
	offset := alignUpUint32(*bytesTransferred, eaInfoAlignment)
	if uint32(len(buffer)) < offset+eaLen {
		return false
	}
	dst := buffer[offset : offset+eaLen]
	ea := (*FILE_FULL_EA_INFORMATION)(unsafe.Pointer(&dst[0]))
	ea.NextEntryOffset = alignUpUint32(eaLen, eaInfoAlignment)
	ea.Flags = flags
	ea.EaNameLength = uint8(len(nameBytes))
	ea.EaValueLength = uint16(eaValueLength)
	copy(dst[eaNameOffset:], nameBytes)
	dst[eaNameOffset+len(nameBytes)] = 0
	copy(dst[eaNameOffset+len(nameBytes)+1:], value)
	*bytesTransferred = offset + eaLen
	return true
}

// FileSystemGetEaPackedSize returns the packed EA size that
// matches what NTFS reports (FspFileSystemGetEaPackedSize).
func FileSystemGetEaPackedSize(nameLength uint8, valueLength uint16) uint32 {
	return 5 + uint32(nameLength) + uint32(valueLength)
}

// EnumerateEa walks a FILE_FULL_EA_INFORMATION buffer and
// invokes fn for each entry. An empty value indicates the EA
// should be deleted (SetEa semantics).
func EnumerateEa(
	eaBuffer []byte,
	fn func(flags uint8, name string, value []byte) error,
) error {
	if len(eaBuffer) == 0 {
		return nil
	}
	offset := 0
	for offset+eaNameOffset <= len(eaBuffer) {
		ea := (*FILE_FULL_EA_INFORMATION)(unsafe.Pointer(&eaBuffer[offset]))
		nameLen := int(ea.EaNameLength)
		valueLen := int(ea.EaValueLength)
		entryBase := offset + eaNameOffset
		need := entryBase + nameLen + 1 + valueLen
		if need > len(eaBuffer) {
			break
		}
		name := string(eaBuffer[entryBase : entryBase+nameLen])
		value := eaBuffer[entryBase+nameLen+1 : entryBase+nameLen+1+valueLen]
		if err := fn(ea.Flags, name, value); err != nil {
			return err
		}
		if ea.NextEntryOffset == 0 {
			break
		}
		offset += int(ea.NextEntryOffset)
	}
	return nil
}

// BehaviourGetStreamInfoRaw is the raw interface of
// GetStreamInfo. Under most circumstances, implement
// BehaviourGetStreamInfo instead.
type BehaviourGetStreamInfoRaw interface {
	GetStreamInfoRaw(
		fs *FileSystemRef, file uintptr, buf []byte,
	) (int, error)
}

func delegateGetStreamInfo(
	fileSystem, fileContext uintptr,
	buf uintptr, length uint32, numRead *uint32,
) windows.NTStatus {
	ref := loadFileSystemRef(fileSystem)
	if ref == nil {
		return ntStatusNoRef
	}
	n, err := ref.getStreamInfoRaw.GetStreamInfoRaw(
		ref, fileContext, enforceBytePtr(buf, int(length)))
	*numRead = uint32(n)
	return convertNTStatus(err)
}

var go_delegateGetStreamInfo = syscall.NewCallbackCDecl(func(
	fileSystem, fileContext uintptr,
	buf uintptr, length uint32, numRead *uint32,
) uintptr {
	return uintptr(delegateGetStreamInfo(
		fileSystem, fileContext, buf, length, numRead,
	))
})

// BehaviourGetStreamInfo lists named streams for a file.
//
// Call fill for each stream (the default unnamed stream
// uses an empty name). Return false from fill to stop
// early when the response buffer is full.
type BehaviourGetStreamInfo interface {
	GetStreamInfo(
		fs *FileSystemRef, file uintptr,
		fill func(name string, streamSize, streamAllocationSize uint64) (bool, error),
	) error
}

type behaviourGetStreamInfoDelegate struct {
	getStreamInfo BehaviourGetStreamInfo
}

func (d *behaviourGetStreamInfoDelegate) GetStreamInfoRaw(
	fs *FileSystemRef, file uintptr, buf []byte,
) (int, error) {
	var transferred uint32
	err := d.getStreamInfo.GetStreamInfo(fs, file,
		func(name string, streamSize, streamAllocationSize uint64) (bool, error) {
			if !FileSystemAddStreamInfo(
				name, streamSize, streamAllocationSize,
				false, buf, &transferred,
			) {
				return false, nil
			}
			return true, nil
		})
	if err != nil {
		return int(transferred), err
	}
	FileSystemAddStreamInfo("", 0, 0, true, buf, &transferred)
	return int(transferred), nil
}

// BehaviourGetEaRaw is the raw interface of GetEa.
// Under most circumstances, implement BehaviourGetEa
// instead.
type BehaviourGetEaRaw interface {
	GetEaRaw(
		fs *FileSystemRef, file uintptr, buf []byte,
	) (int, error)
}

func delegateGetEa(
	fileSystem, fileContext uintptr,
	buf uintptr, length uint32, numRead *uint32,
) windows.NTStatus {
	ref := loadFileSystemRef(fileSystem)
	if ref == nil {
		return ntStatusNoRef
	}
	n, err := ref.getEaRaw.GetEaRaw(
		ref, fileContext, enforceBytePtr(buf, int(length)))
	*numRead = uint32(n)
	return convertNTStatus(err)
}

var go_delegateGetEa = syscall.NewCallbackCDecl(func(
	fileSystem, fileContext uintptr,
	buf uintptr, length uint32, numRead *uint32,
) uintptr {
	return uintptr(delegateGetEa(
		fileSystem, fileContext, buf, length, numRead,
	))
})

// BehaviourGetEa lists extended attributes for a file.
//
// Call fill for each EA. Return false from fill to stop
// early when the response buffer is full.
type BehaviourGetEa interface {
	GetEa(
		fs *FileSystemRef, file uintptr,
		fill func(flags uint8, name string, value []byte) (bool, error),
	) error
}

type behaviourGetEaDelegate struct {
	getEa BehaviourGetEa
}

func (d *behaviourGetEaDelegate) GetEaRaw(
	fs *FileSystemRef, file uintptr, buf []byte,
) (int, error) {
	var transferred uint32
	err := d.getEa.GetEa(fs, file,
		func(flags uint8, name string, value []byte) (bool, error) {
			if !FileSystemAddEa(
				flags, name, value, false, buf, &transferred,
			) {
				return false, nil
			}
			return true, nil
		})
	if err != nil {
		return int(transferred), err
	}
	FileSystemAddEa(0, "", nil, true, buf, &transferred)
	return int(transferred), nil
}

// BehaviourSetEa sets extended attributes on a file.
//
// ApplyEa is invoked once per EA entry in the request
// buffer (via EnumerateEa). An empty value means the EA
// should be deleted. After all entries are applied,
// CompleteSetEa must write file information to info.
type BehaviourSetEa interface {
	ApplyEa(
		fs *FileSystemRef, file uintptr,
		flags uint8, name string, value []byte,
	) error

	CompleteSetEa(
		fs *FileSystemRef, file uintptr,
		info *FSP_FSCTL_FILE_INFO,
	) error
}

// BehaviourSetEaRaw is the raw interface of SetEa that
// receives the unparsed EA buffer.
type BehaviourSetEaRaw interface {
	SetEaRaw(
		fs *FileSystemRef, file uintptr,
		ea []byte, info *FSP_FSCTL_FILE_INFO,
	) error
}

type behaviourSetEaDelegate struct {
	setEa BehaviourSetEa
}

func (d *behaviourSetEaDelegate) SetEaRaw(
	fs *FileSystemRef, file uintptr,
	ea []byte, info *FSP_FSCTL_FILE_INFO,
) error {
	if err := EnumerateEa(ea, func(flags uint8, name string, value []byte) error {
		return d.setEa.ApplyEa(fs, file, flags, name, value)
	}); err != nil {
		return err
	}
	return d.setEa.CompleteSetEa(fs, file, info)
}

func delegateSetEa(
	fileSystem, fileContext uintptr,
	ea uintptr, eaLength uint32, fileInfoAddr uintptr,
) windows.NTStatus {
	ref := loadFileSystemRef(fileSystem)
	if ref == nil {
		return ntStatusNoRef
	}
	return convertNTStatus(ref.setEaRaw.SetEaRaw(
		ref, fileContext,
		enforceBytePtr(ea, int(eaLength)),
		(*FSP_FSCTL_FILE_INFO)(unsafe.Pointer(fileInfoAddr)),
	))
}

var go_delegateSetEa = syscall.NewCallbackCDecl(func(
	fileSystem, fileContext uintptr,
	ea uintptr, eaLength uint32, fileInfoAddr uintptr,
) uintptr {
	return uintptr(delegateSetEa(
		fileSystem, fileContext, ea, eaLength, fileInfoAddr,
	))
})
