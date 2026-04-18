package localtalk

import (
	"time"

	"github.com/tarm/serial"

	"github.com/pgodw/omnitalk/go/port"
)

type TashTalkPort struct {
	*Port
	serialPort string
	s          *serial.Port
	stop       chan struct{}
}

func NewTashTalkPort(serialPort string, seedNetwork uint16, seedZoneName []byte) *TashTalkPort {
	base := New(seedNetwork, seedZoneName, false, 0xFE)
	base.SetSupportsRTSCTS(true)
	p := &TashTalkPort{Port: base, serialPort: serialPort, stop: make(chan struct{})}
	p.ConfigureSendFrame(p.sendFrame)
	return p
}

func (p *TashTalkPort) ShortString() string { return p.serialPort }

func (p *TashTalkPort) Start(router port.RouterHooks) error {
	c := &serial.Config{Name: p.serialPort, Baud: 1000000, ReadTimeout: time.Millisecond * 250}
	s, err := serial.OpenPort(c)
	if err != nil {
		return err
	}
	p.s = s
	_, _ = p.s.Write(append(make([]byte, 1024), []byte{0x02}...))
	if err := p.Port.Start(router); err != nil {
		return err
	}
	go p.readRun()
	return nil
}

func (p *TashTalkPort) Stop() error {
	close(p.stop)
	if p.s != nil {
		_ = p.s.Close()
	}
	return p.Port.Stop()
}

func (p *TashTalkPort) sendFrame(frame []byte) error {
	_, err := p.s.Write(append([]byte{0x01}, frame...))
	return err
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
					p.InboundFrame(append([]byte(nil), frame...))
				}
				frame = frame[:0]
				continue
			}
			frame = append(frame, b)
		}
	}
}
