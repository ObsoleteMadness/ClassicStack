// Embedded/TinyGo buffer sizes: small, so the static footprint fits an ESP32
// class device. Same constant set as the default file (buf.go); only the values
// differ (§1).
//
//go:build tinygo

package buf

const (
	// FrameMax on embedded covers a standard Ethernet frame plus a little
	// headroom — no jumbo frames on these links.
	FrameMax = 1600

	// ReadChunk is kept small to bound RAM use during streaming reads.
	ReadChunk = 2048

	// LogFieldMax bounds a rendered log field on a memory-constrained target.
	LogFieldMax = 128
)
