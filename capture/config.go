package capture

import (
	"fmt"
	"strings"
)

// Config selects which transports get capture files written. Empty
// path disables capture for that transport.
type Config struct {
	LocalTalk string `koanf:"localtalk"`
	EtherTalk string `koanf:"ethertalk"`
	IPX       string `koanf:"ipx"`
	Snaplen   uint32 `koanf:"snaplen"`
}

func DefaultConfig() Config {
	return Config{Snaplen: 65535}
}

func (c *Config) Validate() error {
	c.LocalTalk = strings.TrimSpace(c.LocalTalk)
	c.EtherTalk = strings.TrimSpace(c.EtherTalk)
	c.IPX = strings.TrimSpace(c.IPX)
	if c.Snaplen == 0 {
		c.Snaplen = 65535
	}
	if c.Snaplen < 64 {
		return fmt.Errorf("Capture.snaplen %d too small", c.Snaplen)
	}
	return nil
}

func (c *Config) LocalTalkEnabled() bool { return c.LocalTalk != "" }
func (c *Config) EtherTalkEnabled() bool { return c.EtherTalk != "" }
func (c *Config) IPXEnabled() bool       { return c.IPX != "" }
