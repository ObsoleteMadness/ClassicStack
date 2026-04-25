package localtalk

import (
	"fmt"
	"io"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	serial "github.com/jacobsa/go-serial/serial"
	"github.com/pgodw/omnitalk/netlog"

	"github.com/pgodw/omnitalk/port"
)

type TashTalkPort struct {
	*Port
	serialPort string
	s          io.ReadWriteCloser
	stop       chan struct{}
	writeMu    sync.Mutex
}

func NewTashTalkPort(serialPort string, seedNetwork uint16, seedZoneName []byte) *TashTalkPort {
	base := New(seedNetwork, seedZoneName, false, 0xFE)
	base.SetSupportsRTSCTS(true)
	base.SetRTSCTSManagedByTransport(true)
	base.SetCTSResponseTimeout(25 * time.Millisecond)
	p := &TashTalkPort{Port: base, serialPort: serialPort, stop: make(chan struct{})}
	p.SetFrameSender(p)
	p.SetNodeIDChangeHook(p.setNodeID)
	return p
}

func (p *TashTalkPort) ShortString() string { return p.serialPort }

func (p *TashTalkPort) Start(router port.RouterHooks) error {
	s, err := serial.Open(serial.OpenOptions{
		PortName:              normalizeSerialPortName(p.serialPort),
		BaudRate:              1000000,
		DataBits:              8,
		StopBits:              1,
		ParityMode:            serial.PARITY_NONE,
		RTSCTSFlowControl:     true,
		InterCharacterTimeout: uint((250 * time.Millisecond) / time.Millisecond),
		MinimumReadSize:       1,
	})
	if err != nil {
		return err
	}
	p.s = s
	if err := p.writeRaw(buildInitSequence()); err != nil {
		return err
	}
	if err := p.Port.Start(router); err != nil {
		return err
	}
	go p.readRun()
	return nil
}

func normalizeSerialPortName(name string) string {
	if runtime.GOOS != "windows" {
		return name
	}
	if strings.HasPrefix(name, `\\.\`) {
		return name
	}
	upper := strings.ToUpper(strings.TrimSpace(name))
	if !strings.HasPrefix(upper, "COM") {
		return name
	}
	if _, err := strconv.Atoi(strings.TrimPrefix(upper, "COM")); err != nil {
		return name
	}
	return `\\.\` + upper
}

func (p *TashTalkPort) Stop() error {
	close(p.stop)
	if p.s != nil {
		_ = p.s.Close()
	}
	return p.Port.Stop()
}

// SendFrame implements FrameSender by transmitting frame over the
// TashTalk serial link with the protocol's framing byte and FCS
// appended.
func (p *TashTalkPort) SendFrame(frame []byte) error { return p.sendFrame(frame) }

func (p *TashTalkPort) sendFrame(frame []byte) error {
	withFCS := appendFCS(frame)
	packet := make([]byte, 0, 1+len(withFCS))
	packet = append(packet, 0x01)
	packet = append(packet, withFCS...)
	return p.writeRaw(packet)
}

func (p *TashTalkPort) readRun() {
	buf := make([]byte, 1024)
	var frame []byte
	escaped := false
	for {
		select {
		case <-p.stop:
			return
		default:
		}
		n, err := p.s.Read(buf)
		if err != nil || n == 0 {
			continue
		}
		for _, b := range buf[:n] {
			if !escaped && b == 0x00 {
				escaped = true
				continue
			}
			if escaped {
				escaped = false
				if b == 0xFF {
					frame = append(frame, 0x00)
					continue
				}
				if b == 0xFD && len(frame) >= 5 {
					if llap, ok := parseInboundTashTalkFrame(frame); ok {
						p.InboundFrame(llap)
					}
				}
				frame = frame[:0]
				continue
			}
			frame = append(frame, b)
		}
	}
}

func (p *TashTalkPort) setNodeID(node uint8) {
	if p.s == nil {
		return
	}
	cmd, err := buildSetNodeAddressCmd(node)
	if err != nil {
		netlog.Debug("%s ignoring invalid node ID %d for TashTalk command: %v", p.ShortString(), node, err)
		return
	}
	if err := p.writeRaw(cmd); err != nil {
		netlog.Debug("%s failed to send TashTalk node ID command for node %d: %v", p.ShortString(), node, err)
	}
}

func (p *TashTalkPort) writeRaw(data []byte) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	_, err := p.s.Write(data)
	return err
}

func buildInitSequence() []byte {
	buf := make([]byte, 0, 1024+33+2)
	buf = append(buf, make([]byte, 1024)...)
	buf = append(buf, 0x02)
	buf = append(buf, make([]byte, 32)...)
	buf = append(buf, 0x03, 0x00)
	return buf
}

func buildSetNodeAddressCmd(node uint8) ([]byte, error) {
	if node == 0 {
		return append([]byte{0x02}, make([]byte, 32)...), nil
	}
	if node < 1 || node > 254 {
		return nil, fmt.Errorf("node address %d not between 1 and 254", node)
	}
	cmd := make([]byte, 33)
	cmd[0] = 0x02
	idx := node / 8
	bit := node % 8
	cmd[int(idx)+1] = 1 << bit
	return cmd, nil
}

func parseInboundTashTalkFrame(frame []byte) ([]byte, bool) {
	if len(frame) < 5 {
		return nil, false
	}
	data := frame[:len(frame)-2]
	if !fcsMatches(data, frame[len(frame)-2], frame[len(frame)-1]) {
		return nil, false
	}
	return append([]byte(nil), data...), true
}

func appendFCS(frame []byte) []byte {
	b1, b2 := fcsBytes(frame)
	out := make([]byte, 0, len(frame)+2)
	out = append(out, frame...)
	out = append(out, b1, b2)
	return out
}

func fcsMatches(frame []byte, b1, b2 byte) bool {
	e1, e2 := fcsBytes(frame)
	return b1 == e1 && b2 == e2
}

func fcsBytes(frame []byte) (byte, byte) {
	crc := uint16(0xFFFF)
	for _, b := range frame {
		crc ^= uint16(b)
		for i := 0; i < 8; i++ {
			if crc&1 != 0 {
				crc = (crc >> 1) ^ 0x8408
			} else {
				crc >>= 1
			}
		}
	}
	crc = ^crc
	return byte(crc & 0xFF), byte(crc >> 8)
}
