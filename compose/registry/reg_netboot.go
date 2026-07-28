//go:build (netboot && router) || all

// Netboot (ABP boot server) builds via routerFor (ddpservice.go, gated `router || all`),
// so its registration requires `router` as well as `netboot` — a `netboot`-only build has
// no routerFor and would not link. The CI matrix always pairs them ("netboot router"); the
// umbrella `all` tag satisfies both.

package registry

import (
	"fmt"
	"os"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/core/component"
	"github.com/ObsoleteMadness/ClassicStack/core/hash/snefru"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
	"github.com/ObsoleteMadness/ClassicStack/core/protocol/abp"
	"github.com/ObsoleteMadness/ClassicStack/core/service/netboot"
)

func init() {
	// Register the Netboot singleton section (payload/disk paths + serving
	// knobs) so the codec round-trips it.
	netboot.RegisterSection()

	// Build the netboot server: it rides the shared AppleTalk router on the boot
	// socket (ABP) and boot socket + 1 (ChainBoot EBP, via ExtraRouterServices),
	// serving whichever LocalTalk/EtherTalk segments are router members. File
	// I/O stays at this edge: the payload is read (and Snefru-trailered) here
	// and the writable EBP disk image is opened here; the core service consumes
	// bytes and a ReaderAt/WriterAt seam. Load failures degrade gracefully to an
	// inert service (the conformance contract) with the error logged. The
	// compose transport cross-wire injects the NBP service for the BootServer
	// registration.
	Register(netboot.Name, func(ctx *BuildContext) (component.Component, error) {
		logger := ctx.Logger(netboot.Name)
		sec := netboot.SectionFromModel(ctx.Model)

		var cfg netboot.Config
		enabled := false
		if sec != nil {
			enabled = sec.Enabled
			cfg.BlockSize = sec.EffectiveBlockSize()
			cfg.Pace = time.Duration(sec.PaceMs) * time.Millisecond
			cfg.ChainPace = time.Duration(sec.ChainPaceMs) * time.Millisecond
			cfg.NBPObject = sec.Name
			cfg.Zone = sec.Zone
			if sec.Enabled && sec.Payload != "" {
				cfg.Payload = loadPayload(sec.Payload, sec.Image, cfg.BlockSize, logger)
			}
			if sec.Enabled && sec.Disk != "" {
				cfg.Disk = openBootDisk(sec.Disk, logger)
			}
		}

		svc := netboot.New(routerFor(ctx), cfg, logger)
		svc.SetEnabled(enabled)
		return svc, nil
	})
}

// loadPayload assembles the served ABP boot payload and guarantees the Snefru
// self-authentication trailer the ROM verifies. With an imagePath the payload
// stub (BootWrapper/romdrv-style RAM-disk driver) and the disk image are
// concatenated verbatim and trailered — the dynamic equivalent of the NetBoot
// repo's `cat BootWrapper.bin disk.dsk` + snefru_hash.py build. Without one,
// a payload already ending in a valid trailer is served untouched, anything
// else is padded and trailered. Returns nil (inert service) on failure.
func loadPayload(path, imagePath string, blockSize int, logger log.Logger) []byte {
	data, err := os.ReadFile(path)
	if err != nil {
		logger.Log1(log.Error, "netboot: cannot read payload", log.Str("err", err.Error()))
		return nil
	}
	switch {
	case imagePath != "":
		img, err := os.ReadFile(imagePath)
		if err != nil {
			logger.Log1(log.Error, "netboot: cannot read image", log.Str("err", err.Error()))
			return nil
		}
		stubLen := len(data)
		data, err = snefru.AppendTrailer(append(data, img...), blockSize)
		if err != nil {
			logger.Log1(log.Error, "netboot: cannot trailer payload", log.Str("err", err.Error()))
			return nil
		}
		logger.Log(log.Info, "netboot: payload assembled from stub + image",
			log.Str("payload", path),
			log.Str("image", imagePath),
			log.Int("stub_bytes", int64(stubLen)),
			log.Int("total_bytes", int64(len(data))))
	case snefru.HasValidTrailer(data) && len(data)%blockSize == 0:
		logger.Log2(log.Info, "netboot: payload pre-trailered",
			log.Str("path", path), log.Int("bytes", int64(len(data))))
	default:
		trailered, err := snefru.AppendTrailer(data, blockSize)
		if err != nil {
			logger.Log1(log.Error, "netboot: cannot trailer payload", log.Str("err", err.Error()))
			return nil
		}
		logger.Log2(log.Info, "netboot: payload trailered",
			log.Str("path", path), log.Int("bytes", int64(len(trailered))))
		data = trailered
	}
	if blocks := len(data) / blockSize; blocks > abp.MaxImageBlocks {
		logger.Log1(log.Error, "netboot: payload exceeds the client's 512-byte request bitmap",
			log.Str("hint", fmt.Sprintf("%d blocks of %d bytes; serve a ChainLoader payload instead", blocks, blockSize)))
		return nil
	}
	return data
}

// bootDisk adapts an *os.File to the netboot.Disk seam with the size captured
// at open. The handle lives for the process lifetime (restarts reuse it).
type bootDisk struct {
	*os.File
	size int64
}

func (d *bootDisk) Size() int64 { return d.size }

// openBootDisk opens the writable EBP disk image read-write. Returns nil
// (EBP disabled) on failure.
func openBootDisk(path string, logger log.Logger) netboot.Disk {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		logger.Log1(log.Error, "netboot: cannot open disk image", log.Str("err", err.Error()))
		return nil
	}
	st, err := f.Stat()
	if err != nil {
		logger.Log1(log.Error, "netboot: cannot stat disk image", log.Str("err", err.Error()))
		_ = f.Close()
		return nil
	}
	logger.Log2(log.Info, "netboot: serving disk image",
		log.Str("path", path), log.Int("bytes", st.Size()))
	return &bootDisk{File: f, size: st.Size()}
}
