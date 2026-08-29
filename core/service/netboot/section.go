// SPDX-FileCopyrightText: Based on Netboot code by Elliot Nunn
// SPDX-License-Identifier: MIT

package netboot

import (
	"errors"

	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/abp"
)

// SectionKey is the config-section / registry name for the netboot service. It
// matches the component Name ("Netboot"), the singleton convention.
const SectionKey = Name

// Section is the netboot singleton config: the served boot payload, the
// optional EBP disk image, and serving knobs. Satisfies config.Section so the
// model round-trips it.
type Section struct {
	// SKey is the section key; always "Netboot". Stored so Key() is a plain getter.
	SKey string `toml:"-"`
	// Enabled gates the service (component.Enableable).
	Enabled bool `toml:"enabled" display:"Enabled" desc:"Whether netboot is configured on." default:"false"`
	// Payload is the host path of the ABP boot payload — executable 68k code
	// the ROM downloads and runs: ChainLoader.bin for the streaming-disk path,
	// a BootWrapper/romdrv-style RAM-disk driver stub, or a fully pre-built
	// payload. The Snefru self-authentication trailer is appended at load
	// unless the file already carries a valid one.
	Payload string `toml:"payload,omitempty" display:"Payload" desc:"Host path of the ABP boot payload (ChainLoader.bin, BootWrapper, …)." example:"ChainLoader.bin"`
	// Image is an optional disk image appended to Payload at load (the RAM-disk
	// contents a BootWrapper-style stub serves): the server concatenates
	// payload+image verbatim and appends the Snefru trailer — the dynamic
	// equivalent of the NetBoot repo's `cat BootWrapper.bin disk.dsk` +
	// snefru_hash.py build. Not used by ChainLoader payloads (see Disk).
	Image string `toml:"image,omitempty" display:"Image" desc:"Optional RAM-disk image concatenated onto Payload (BootWrapper path)." example:"disk.dsk"`
	// BlockSize is the ABP block size the payload is served with. 0 → 512
	// (disksector); ChainLoader payloads use 256 (ATBOOT_BLOCK_SIZE). Must be
	// a multiple of 64 for the Snefru trailer.
	BlockSize int `toml:"block_size,omitempty" display:"Block size" desc:"ABP block size in bytes (0 = 512; ChainLoader uses 256)." default:"0" example:"256"`
	// Disk is the host path of the writable HFS disk image streamed over the
	// ChainBoot EBP protocol (the System volume the client boots into).
	// Empty disables EBP. Opened read-write; single concurrent client.
	Disk string `toml:"disk,omitempty" display:"Disk image" desc:"Writable HFS image streamed over ChainBoot EBP. Empty disables EBP." example:"System.img"`
	// PaceMs is the inter-packet delay of the ABP block flood in milliseconds.
	// 0 → 2 ms (LToUDP has no link backpressure).
	PaceMs int `toml:"pace_ms,omitempty" display:"ABP pace (ms)" desc:"Inter-packet delay for the ABP block flood (0 = 2 ms)." default:"0" example:"2"`
	// ChainPaceMs is the inter-packet delay of ChainBoot EBP read-reply bursts
	// in milliseconds. 0 → 10 ms. The client's interrupt-level listener must
	// catch EVERY block of a chunk in one burst (its progress bitmap resets on
	// retry), so this is deliberately slower than pace_ms; real LocalTalk
	// delivers a 530-byte frame no faster than every ~18 ms (230.4 kbit/s) —
	// raise towards that if chunk reads keep retrying.
	ChainPaceMs int `toml:"chain_pace_ms,omitempty" display:"ChainBoot pace (ms)" desc:"Inter-packet delay for ChainBoot EBP bursts (0 = 10 ms)." default:"0" example:"10"`
	// Name is the NBP object name registered for display; matching is
	// any-object (clients look up their PRAM serverNum in hex), so this is
	// cosmetic. "" → "0000".
	Name string `toml:"name,omitempty" display:"NBP name" desc:"Cosmetic BootServer NBP object name. Empty = 0000." example:"0000"`
	// Zone is the NBP zone the BootServer name is registered in. "" → "*".
	Zone string `toml:"zone,omitempty" display:"Zone" desc:"NBP zone for the BootServer name. Empty = *." example:"*" widget:"zone"`
}

// Key returns the section key.
func (s *Section) Key() string { return SectionKey }

// Clone returns a deep copy (all fields are values).
func (s *Section) Clone() config.Section {
	cp := *s
	return &cp
}

// Validate checks the section in isolation. File existence and payload sizing
// are checked at the compose edge where the files are opened.
func (s *Section) Validate() error {
	if !s.Enabled {
		return nil
	}
	if s.Payload == "" {
		return errors.New("netboot: payload path is required when enabled")
	}
	if s.Image != "" && s.Disk != "" {
		return errors.New("netboot: image (RAM-disk payload) and disk (ChainBoot streaming) are mutually exclusive")
	}
	if s.BlockSize != 0 {
		if s.BlockSize%64 != 0 || s.BlockSize < 64 {
			return errors.New("netboot: block_size must be a positive multiple of 64")
		}
		// One rbImageData packet (6-byte header + block) must fit a DDP payload.
		if 6+s.BlockSize > abp.DDPMaxData {
			return errors.New("netboot: block_size too large for a DDP datagram")
		}
	}
	if s.PaceMs < 0 {
		return errors.New("netboot: pace_ms must not be negative")
	}
	if s.ChainPaceMs < 0 {
		return errors.New("netboot: chain_pace_ms must not be negative")
	}
	return nil
}

// EffectiveBlockSize resolves the ABP block size (0 → disksector 512).
func (s *Section) EffectiveBlockSize() int {
	if s.BlockSize == 0 {
		return abp.DiskSector
	}
	return s.BlockSize
}

// compile-time assertion: *Section satisfies config.Section.
var _ config.Section = (*Section)(nil)

// SectionFromModel resolves the Netboot section from the model, or nil when none is set.
func SectionFromModel(m *config.Model) *Section {
	if m != nil {
		if s, ok := m.Get(SectionKey); ok {
			if ns, ok := s.(*Section); ok {
				return ns
			}
		}
	}
	return nil
}

// RegisterSection installs the Netboot section schema so codecs round-trip it.
// Called from the compose registry wiring (kept out of an init() so a build
// excluding netboot excludes the section too).
func RegisterSection() {
	config.Register(config.SectionSchema{
		Key: SectionKey,
		New: func() config.Section { return &Section{SKey: SectionKey} },
		Validate: func(s config.Section) error {
			if ns, ok := s.(*Section); ok {
				return ns.Validate()
			}
			return nil
		},
		DisplayName: "Netboot",
		Description: "AppleTalk Boot Protocol (ABP) + ChainBoot EBP disk streaming for classic Mac netboot clients.",
	})
}
