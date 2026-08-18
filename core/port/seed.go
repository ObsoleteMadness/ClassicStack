package port

// SeedFields is the AppleTalk seed configuration an EtherTalk/LocalTalk port embeds.
type SeedFields struct {
	SeedNetwork    uint16 `toml:"seed_network,omitempty" display:"Seed network start" desc:"First AppleTalk network number this port asserts. 0 = non-seed (learn from a peer)." default:"0" example:"3" capability:"appletalk_seed"`
	SeedNetworkEnd uint16 `toml:"seed_network_end,omitempty" display:"Seed network end" desc:"Last network number of the seed range. 0 = a single number (== start)." default:"0" example:"5" capability:"appletalk_seed"`
	// SeedZone is the zone name this port publishes (EtherTalk / LToUDP / TashTalk
	// seed the zone list). It is free-form on purpose — not a picker of existing
	// zones, which services like AFP/MacIP/IPXGW use via widget:"zone".
	SeedZone string `toml:"seed_zone,omitempty" display:"Seed zone" desc:"Zone name this port seeds and advertises. Empty = non-seed / inherit." example:"EtherTalk Network" capability:"appletalk_seed"`
}

// SeedProvider is the capability a section implements when it seeds an AppleTalk network.
type SeedProvider interface {
	SeedNetworkRange() (start, end uint16)
	SeedZoneName() string
}

// SeedNetworkRange returns the configured seed network bounds.
func (s SeedFields) SeedNetworkRange() (start, end uint16) {
	return s.SeedNetwork, s.SeedNetworkEnd
}

// SeedZoneName returns the seeded zone name.
func (s SeedFields) SeedZoneName() string { return s.SeedZone }

func validateSeed(s SeedFields) error {
	if s.SeedNetworkEnd != 0 && s.SeedNetworkEnd < s.SeedNetwork {
		return ErrSeedRange
	}
	return nil
}

var _ SeedProvider = SeedFields{}
