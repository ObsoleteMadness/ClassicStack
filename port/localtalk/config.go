package localtalk

import (
	"fmt"
	"strings"
)

// LToUDPConfig configures the LocalTalk-over-UDP port.
type LToUDPConfig struct {
	Enabled     bool   `koanf:"enabled"`
	Interface   string `koanf:"interface"`
	SeedNetwork uint   `koanf:"seed_network"`
	SeedZone    string `koanf:"seed_zone"`
}

// DefaultLToUDPConfig returns the built-in defaults.
func DefaultLToUDPConfig() LToUDPConfig {
	return LToUDPConfig{
		Enabled:     true,
		Interface:   "0.0.0.0",
		SeedNetwork: 1,
		SeedZone:    "LToUDP Network",
	}
}

func (c *LToUDPConfig) Validate() error {
	if !c.Enabled {
		return nil
	}
	if strings.TrimSpace(c.SeedZone) == "" {
		return fmt.Errorf("LToUdp.seed_zone must not be empty")
	}
	if c.SeedNetwork == 0 || c.SeedNetwork > 0xFFFE {
		return fmt.Errorf("LToUdp.seed_network %d out of range", c.SeedNetwork)
	}
	return nil
}

// TashTalkConfig configures the TashTalk serial LocalTalk adaptor port.
type TashTalkConfig struct {
	// Port is the OS serial-device path (e.g. "COM1", "/dev/ttyAMA0").
	// Blank disables the TashTalk port entirely.
	Port        string `koanf:"port"`
	SeedNetwork uint   `koanf:"seed_network"`
	SeedZone    string `koanf:"seed_zone"`
}

func DefaultTashTalkConfig() TashTalkConfig {
	return TashTalkConfig{
		SeedNetwork: 2,
		SeedZone:    "TashTalk Network",
	}
}

func (c *TashTalkConfig) Validate() error {
	if !c.Enabled() {
		return nil
	}
	if strings.TrimSpace(c.SeedZone) == "" {
		return fmt.Errorf("TashTalk.seed_zone must not be empty")
	}
	if c.SeedNetwork == 0 || c.SeedNetwork > 0xFFFE {
		return fmt.Errorf("TashTalk.seed_network %d out of range", c.SeedNetwork)
	}
	return nil
}

// Enabled reports whether the TashTalk port should be created. A blank
// Port disables the adaptor without erroring.
func (c *TashTalkConfig) Enabled() bool {
	return strings.TrimSpace(c.Port) != ""
}
