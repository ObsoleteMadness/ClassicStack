//go:build afp || all

/*
Package afp implements the AppleTalk Filing Protocol (AFP) 2.x.

AFP is an application-layer protocol that allows users to share files and network
resources.

Inside Macintosh: Networking, Chapter 9.
https://dev.os9.ca/techpubs/mac/Networking/Networking-223.html
*/
package afp

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/pgodw/omnitalk/port"
	"github.com/pgodw/omnitalk/protocol/ddp"
	"github.com/pgodw/omnitalk/service"
)

// Service implements AppleTalk Filing Protocol.
type Service struct {
	ServerName  string
	Volumes     []Volume
	fs          FileSystem
	volumeFS    map[uint16]FileSystem
	meta        ForkMetadataBackend            // global override when ForkMetadataBackend is injected via options
	metas       map[uint16]ForkMetadataBackend // per-volume backends (keyed by Volume.ID)
	mu          sync.RWMutex
	options     Options
	cnidStores  map[uint16]CNIDStore
	desktopDB   DesktopDBBackend
	forks       map[uint16]*forkHandle
	nextFork    uint16
	byteLocks   []byteRangeLock
	maxReadSize int // transport quantum limit; 0 = unlimited
	maxLocks    int

	users       map[string]string // map[username]password
	nextSRefNum uint16

	// volumeBackupDate stores AFP "backup date" (ADouble-style seconds since 1904)
	// per volume, as set by FPSetVolParms (AFP 2.x §5.1.32).
	volumeBackupDate map[uint16]uint32

	// Desktop database state — one DesktopDB per volume (persists across sessions).
	desktopDBs map[uint16]DesktopDB
	dtRefs     map[uint16]uint16 // DTRefNum → volume ID
	nextDTRef  uint16

	transports []Transport
	dumper     service.PacketDumper

	stop chan struct{}
	wg   sync.WaitGroup
}

func (s *Service) SetPacketDumper(dumper service.PacketDumper) {
	s.dumper = dumper
}

// applyMaxReadSize caps FPRead ReqCount to n bytes and propagates the same
// limit to any filesystem that supports range limiting (e.g.
// MacGardenFileSystem). Called from Start after each transport has resolved
// its quantum; n=0 leaves reads uncapped.
func (s *Service) applyMaxReadSize(n int) {
	s.maxReadSize = n
	if n == 0 {
		return
	}
	type rangeLimiter interface{ SetMaxRangeSize(int) }
	if rl, ok := s.fs.(rangeLimiter); ok {
		rl.SetMaxRangeSize(n)
	}
	for _, vfs := range s.volumeFS {
		if rl, ok := vfs.(rangeLimiter); ok {
			rl.SetMaxRangeSize(n)
		}
	}
}

func NewService(serverName string, configs []VolumeConfig, fs FileSystem, transports []Transport, opts ...Options) *Service {
	options := DefaultOptions()
	if len(opts) > 0 {
		options = opts[0]
	}

	s := &Service{
		ServerName:  serverName,
		fs:          fs,
		stop:        make(chan struct{}),
		volumeFS:    make(map[uint16]FileSystem),
		options:     options,
		cnidStores:  make(map[uint16]CNIDStore),
		desktopDB:   resolveDesktopDBBackend(options),
		forks:       make(map[uint16]*forkHandle),
		nextFork:    1,
		byteLocks:   make([]byteRangeLock, 0),
		maxLocks:    defaultMaxByteRangeLocks,
		users:       make(map[string]string),
		nextSRefNum: 1,

		volumeBackupDate: make(map[uint16]uint32),

		desktopDBs: make(map[uint16]DesktopDB),
		dtRefs:     make(map[uint16]uint16),
		nextDTRef:  1,

		transports: transports,
	}

	s.initForkMetadata(options)
	s.installVolumes(configs, fs)
	s.spawnDesktopRebuild()
	return s
}


// Start initializes all underlying transports and resolves the read-size cap
// from whichever transport advertises the smallest non-zero quantum.
func (s *Service) Start(ctx context.Context, router service.Router) error {
	for _, t := range s.transports {
		if err := t.Start(ctx, router); err != nil {
			return err
		}
	}
	cap := 0
	for _, t := range s.transports {
		n := t.MaxReadSize()
		if n <= 0 {
			continue
		}
		if cap == 0 || n < cap {
			cap = n
		}
	}
	s.applyMaxReadSize(cap)
	return nil
}

// Stop shuts down all underlying transports.
func (s *Service) Stop() error {
	var errs []error
	if s.stop != nil {
		select {
		case <-s.stop:
		default:
			close(s.stop)
		}
	}
	for _, t := range s.transports {
		if err := t.Stop(); err != nil {
			errs = append(errs, err)
		}
	}
	s.wg.Wait()
	type closer interface{ Close() error }
	for _, fsys := range s.volumeFS {
		if c, ok := fsys.(closer); ok {
			if err := c.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("afp: stop: %w", errors.Join(errs...))
	}
	return nil
}

// Socket returns the AppleTalk socket number if any of the transports listen on one.
// We return asp.ServerSocket (252) if we have a transport that needs it.
func (s *Service) Socket() uint8 {
	// The router expects services that listen on a specific socket to return it here.
	// Since AFPService wraps transports, we return the well-known ASP socket (252).
	// TCP-only instances won't be called for AppleTalk routing anyway if they don't register NBP.
	return 252 // asp.ServerSocket
}

// Inbound delegates inbound DDP packets to the underlying transports.
func (s *Service) Inbound(d ddp.Datagram, p port.Port) {
	for _, t := range s.transports {
		t.Inbound(d, p)
	}
}

// GetStatus implements the CommandHandler interface
func (s *Service) GetStatus() []byte {
	return BuildServerInfo(s.ServerName)
}


