//go:build linux

package rawlink

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

// ioctl request codes and TAP flags for Linux TUN/TAP devices.
const (
	// tunsetiff is the ioctl request code for TUNSETIFF used to create/configure a TUN/TAP interface.
	tunsetiff = 0x400454ca
	// tungetiff is the ioctl request code for TUNGETIFF used to query interface flags.
	tungetiff = 0x800454d2
	// iffTap indicates a TAP (Ethernet) device.
	iffTap = 0x0002
	// iffNoPI disables the packet information header.
	iffNoPI = 0x1000
	// iffVnetHdr toggles the virtio/vnet header; cleared for macvtap devices.
	iffVnetHdr = 0x4000

	// defaultTapReadTimeoutMs is the default poll timeout (ms) used by ReadFrame.
	defaultTapReadTimeoutMs = 250
)

// ifreq is a trimmed representation of the C `ifreq` structure used with ioctl calls.
type ifreq struct {
	// Name is the interface name, zero-padded to unix.IFNAMSIZ.
	Name [unix.IFNAMSIZ]byte
	// Flags holds the interface flags such as IFF_TAP or IFF_NO_PI.
	Flags uint16
	// _ is reserved padding to match the kernel struct layout.
	_ [24]byte
}

// TunTapLink implements RawLink on top of a Linux TAP/macvtap file descriptor.
type TunTapLink struct {
	// f is the underlying file handle for the TAP or macvtap device.
	f *os.File
	// readTimeoutMs is the poll timeout in milliseconds for ReadFrame.
	readTimeoutMs int
}

// OpenTAP opens a TAP-backed raw link.
//
// Behavior on Linux:
//   - devName="tap0" (or any non-macvtap netdev): opens /dev/net/tun and
//     configures IFF_TAP|IFF_NO_PI on the requested interface name.
//   - devName="macvtap0" (or any netdev with /sys/class/net/<name>/ifindex
//     and /dev/tap<ifindex>): opens /dev/tapX directly and clears IFF_VNET_HDR.
//   - devName="/dev/tapX": opens the device directly and clears IFF_VNET_HDR.
func OpenTAP(devName string) (RawLink, error) {
	name := strings.TrimSpace(devName)
	if name == "" {
		return nil, fmt.Errorf("rawlink: tap backend requires a device/interface name")
	}

	if strings.HasPrefix(name, "/dev/tap") {
		return openMacvtapDevice(name)
	}

	if devPath, ok := macvtapDevicePathForNetdev(name); ok {
		return openMacvtapDevice(devPath)
	}

	return openTapInterface(name)
}

// openTapInterface opens /dev/net/tun and configures a TAP interface with the given name.
func openTapInterface(ifName string) (RawLink, error) {
	f, err := os.OpenFile("/dev/net/tun", os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("rawlink: open /dev/net/tun: %w", err)
	}

	var req ifreq
	copy(req.Name[:], []byte(ifName))
	req.Flags = iffTap | iffNoPI

	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), uintptr(tunsetiff), uintptr(unsafe.Pointer(&req))); errno != 0 {
		_ = f.Close()
		return nil, fmt.Errorf("rawlink: ioctl TUNSETIFF for %s: %w", ifName, errno)
	}

	return &TunTapLink{f: f, readTimeoutMs: defaultTapReadTimeoutMs}, nil
}

// openMacvtapDevice opens a macvtap device at devPath and clears IFF_VNET_HDR.
func openMacvtapDevice(devPath string) (RawLink, error) {
	f, err := os.OpenFile(devPath, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("rawlink: open %s: %w", devPath, err)
	}

	var req ifreq
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), uintptr(tungetiff), uintptr(unsafe.Pointer(&req))); errno != 0 {
		_ = f.Close()
		return nil, fmt.Errorf("rawlink: ioctl TUNGETIFF on %s: %w", devPath, errno)
	}
	req.Flags &^= iffVnetHdr
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, f.Fd(), uintptr(tunsetiff), uintptr(unsafe.Pointer(&req))); errno != 0 {
		_ = f.Close()
		return nil, fmt.Errorf("rawlink: ioctl TUNSETIFF clear IFF_VNET_HDR on %s: %w", devPath, errno)
	}

	return &TunTapLink{f: f, readTimeoutMs: defaultTapReadTimeoutMs}, nil
}

// macvtapDevicePathForNetdev returns the device path for a macvtap device corresponding
// to the named network device (e.g. /dev/tap<ifindex>) and true if found.
func macvtapDevicePathForNetdev(name string) (string, bool) {
	idxPath := filepath.Join("/sys/class/net", name, "ifindex")
	b, err := os.ReadFile(idxPath)
	if err != nil {
		return "", false
	}
	idx, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || idx <= 0 {
		return "", false
	}
	dev := fmt.Sprintf("/dev/tap%d", idx)
	if _, err := os.Stat(dev); err != nil {
		return "", false
	}
	return dev, true
}

// ReadFrame reads a single Ethernet frame from the TAP device, honoring the poll timeout.
func (l *TunTapLink) ReadFrame() ([]byte, error) {
	fd := int(l.f.Fd())
	pollFd := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	n, err := unix.Poll(pollFd, l.readTimeoutMs)
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, ErrTimeout
	}

	buf := make([]byte, 65535)
	rn, err := unix.Read(fd, buf)
	if err != nil {
		return nil, err
	}
	return buf[:rn], nil
}

// WriteFrame writes an Ethernet frame to the TAP device.
func (l *TunTapLink) WriteFrame(frame []byte) error {
	fd := int(l.f.Fd())
	_, err := unix.Write(fd, frame)
	return err
}

// Close closes the underlying TAP/macvtap device file.
func (l *TunTapLink) Close() error {
	return l.f.Close()
}

// Medium implements MediumReporter (TAP/macvtap are Ethernet-equivalent).
func (l *TunTapLink) Medium() PhysicalMedium { return MediumEthernet }
