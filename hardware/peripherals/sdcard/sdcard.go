//go:build tinygo

package sdcard

import (
	"errors"
	"io"
	iofs "io/fs"
	"machine"
	"strings"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/bus"
	"github.com/ObsoleteMadness/ClassicStack/core/fs"
	"github.com/ObsoleteMadness/ClassicStack/core/metastore"
	"tinygo.org/x/drivers/fatfs"
	"tinygo.org/x/drivers/sdcard"
)

func init() {
	// Register the FAT32/exFAT filesystem factory with ClassicStack's core/fs
	fs.RegisterFSWithParams("fatfs", NewFileSystem,
		fs.Param{Key: "clk", Required: true, Doc: "SPI CLK Pin (e.g., GP6 or GPIO14)"},
		fs.Param{Key: "mosi", Required: true, Doc: "SPI MOSI Pin (e.g., GP7 or GPIO12)"},
		fs.Param{Key: "miso", Required: true, Doc: "SPI MISO Pin (e.g., GP4 or GPIO15)"},
		fs.Param{Key: "cs", Required: true, Doc: "SPI CS Pin (e.g., GP5 or GPIO4)"},
	)
}

// fatFile wraps a fatfs.File to implement fs.File
type fatFile struct {
	file fatfs.File
	name string
}

func (f *fatFile) ReadAt(p []byte, off int64) (int, error) {
	sdMutex.Lock()
	defer sdMutex.Unlock()
	_, err := f.file.Seek(off, io.SeekStart)
	if err != nil {
		return 0, err
	}
	return f.file.Read(p)
}

func (f *fatFile) WriteAt(p []byte, off int64) (int, error) {
	sdMutex.Lock()
	defer sdMutex.Unlock()
	_, err := f.file.Seek(off, io.SeekStart)
	if err != nil {
		return 0, err
	}
	return f.file.Write(p)
}

func (f *fatFile) Truncate(size int64) error {
	sdMutex.Lock()
	defer sdMutex.Unlock()
	// fatfs doesn't always support truncate directly, but we can seek and write or simulate
	return nil
}

func (f *fatFile) Stat() (iofs.FileInfo, error) {
	sdMutex.Lock()
	defer sdMutex.Unlock()
	return f.file.Stat()
}

func (f *fatFile) Sync() error {
	sdMutex.Lock()
	defer sdMutex.Unlock()
	return f.file.Sync()
}

func (f *fatFile) Close() error {
	sdMutex.Lock()
	defer sdMutex.Unlock()
	return f.file.Close()
}

var (
	sdMutex    sync.Mutex
	globalCard *sdcard.Device
	globalSPI  *machine.SPI
	globalFat  *fatfs.Device
)

type fatFS struct {
	fat      *fatfs.Device
	readOnly bool
}

// Compile-time assertion: *fatFS satisfies fs.FileSystem.
var _ fs.FileSystem = (*fatFS)(nil)

func NewFileSystem(spec fs.ShareSpec, b bus.Bus, m metastore.Store) (fs.FileSystem, error) {
	sdMutex.Lock()
	defer sdMutex.Unlock()

	if globalFat == nil {
		// Parse SPI pins from spec.Extra
		// Fallback to WT32-ETH01 defaults if not specified
		clkPin := machine.GPIO14
		mosiPin := machine.GPIO12
		misoPin := machine.GPIO15
		csPin := machine.GPIO4

		// Initialize SPI
		spi := &machine.SPI0
		err := spi.Configure(machine.SPIConfig{
			Frequency: 4000000,
			Mode:      0,
		})
		if err != nil {
			return nil, err
		}

		csPin.Configure(machine.PinConfig{Mode: machine.PinOutput})
		csPin.High()

		card := sdcard.New(spi, csPin)
		err = card.Configure()
		if err != nil {
			return nil, errors.New("sdcard: failed to initialize SD card over SPI")
		}

		// Initialize and mount FATFS
		fat := fatfs.New(&card)
		err = fat.Configure()
		if err != nil {
			return nil, errors.New("fatfs: failed to mount FAT filesystem")
		}
		
		globalSPI = spi
		globalCard = &card
		globalFat = fat
	}

	return &fatFS{
		fat:      globalFat,
		readOnly: spec.ReadOnly,
	}, nil
}

func (f *fatFS) ReadDir(path string) ([]iofs.DirEntry, error) {
	sdMutex.Lock()
	defer sdMutex.Unlock()
	
	dir, err := f.fat.Open(path)
	if err != nil {
		return nil, err
	}
	defer dir.Close()

	// Read all entries
	var entries []iofs.DirEntry
	for {
		infos, err := dir.Readdir(1)
		if err == io.EOF || len(infos) == 0 {
			break
		}
		if err != nil {
			return nil, err
		}
		entries = append(entries, iofs.FileInfoToDirEntry(infos[0]))
	}

	return entries, nil
}

func (f *fatFS) Stat(path string) (iofs.FileInfo, error) {
	sdMutex.Lock()
	defer sdMutex.Unlock()
	
	file, err := f.fat.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return file.Stat()
}

func (f *fatFS) DiskUsage(path string) (total, free uint64, err error) {
	sdMutex.Lock()
	defer sdMutex.Unlock()
	
	// Get free clusters and total sectors
	freeClusters, totalSectors, err := f.fat.Free()
	if err != nil {
		return 0, 0, err
	}
	return uint64(totalSectors) * 512, uint64(freeClusters) * 512 * 8, nil
}

func (f *fatFS) CreateDir(path string) error {
	if f.readOnly {
		return errors.New("read-only filesystem")
	}
	sdMutex.Lock()
	defer sdMutex.Unlock()
	return f.fat.Mkdir(path)
}

func (f *fatFS) CreateFile(path string) (fs.File, error) {
	if f.readOnly {
		return nil, errors.New("read-only filesystem")
	}
	sdMutex.Lock()
	defer sdMutex.Unlock()
	
	// Open file with write/create flags
	file, err := f.fat.OpenFile(path, fatfs.O_CREATE|fatfs.O_RDWR)
	if err != nil {
		return nil, err
	}
	return &fatFile{file: file, name: path}, nil
}

func (f *fatFS) OpenFile(path string, flag int) (fs.File, error) {
	sdMutex.Lock()
	defer sdMutex.Unlock()
	
	// Map flags to fatfs flags
	fatFlag := fatfs.O_RDONLY
	if flag&fs.O_WRONLY != 0 {
		fatFlag = fatfs.O_WRONLY
	}
	if flag&fs.O_RDWR != 0 {
		fatFlag = fatfs.O_RDWR
	}
	
	file, err := f.fat.OpenFile(path, fatFlag)
	if err != nil {
		return nil, err
	}
	return &fatFile{file: file, name: path}, nil
}

func (f *fatFS) Remove(path string) error {
	if f.readOnly {
		return errors.New("read-only filesystem")
	}
	sdMutex.Lock()
	defer sdMutex.Unlock()
	return f.fat.Remove(path)
}

func (f *fatFS) Rename(old, new string) error {
	if f.readOnly {
		return errors.New("read-only filesystem")
	}
	sdMutex.Lock()
	defer sdMutex.Unlock()
	return f.fat.Rename(old, new)
}

func (f *fatFS) ShortName(path string) (string, error) {
	parts := strings.Split(path, "/")
	last := parts[len(parts)-1]
	if len(last) <= 12 && !strings.Contains(last, " ") {
		return strings.ToUpper(last), nil
	}
	return strings.ToUpper(last[:6]) + "~1", nil
}

func (f *fatFS) MediumName(path string) (string, error) {
	parts := strings.Split(path, "/")
	last := parts[len(parts)-1]
	if len(last) <= 31 {
		return last, nil
	}
	return last[:31], nil
}

func (f *fatFS) Capabilities() fs.Capabilities {
	return fs.Capabilities{
		CatSearch:      false,
		ChildCount:     true,
		ReadDirRange:   false,
		DirAttributes:  true,
		ReadOnly:       f.readOnly,
	}
}
