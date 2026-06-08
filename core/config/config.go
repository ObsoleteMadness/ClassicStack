package config

import "sync"

// Section is one component's typed config (e.g. *EtherTalkSection). Clone returns a deep
// copy so staging never mutates the live section. Validate checks the section in isolation.
type Section interface {
	Key() string // "EtherTalk", "AFP", … (matches the component/registry name)
	Clone() Section
	Validate() error
}

// Model is the single in-memory source of truth. Well-known sections are typed fields for
// ergonomics; component sections live in Sections keyed by Section.Key().
type Model struct {
	Logging  LoggingSection
	Router   RouterSection
	Bridge   InterfaceSection
	Sections map[string]Section // registered component sections
}

// NewModel returns an empty model with an initialised Sections map.
func NewModel() *Model {
	return &Model{Sections: make(map[string]Section)}
}

// Clone returns a deep copy. Each component Section deep-copies via its own Clone, so staging
// a change never mutates the live model.
func (m *Model) Clone() *Model {
	c := &Model{
		Logging:  m.Logging,
		Router:   m.Router,
		Bridge:   m.Bridge,
		Sections: make(map[string]Section, len(m.Sections)),
	}
	for k, s := range m.Sections {
		c.Sections[k] = s.Clone()
	}
	return c
}

// Get returns the registered section under key, if present.
func (m *Model) Get(key string) (Section, bool) {
	s, ok := m.Sections[key]
	return s, ok
}

// Set installs (or replaces) a component section, keyed by its own Key().
func (m *Model) Set(s Section) {
	if m.Sections == nil {
		m.Sections = make(map[string]Section)
	}
	m.Sections[s.Key()] = s
}

// EffectiveInterface resolves a component's interface, folding bridge inheritance +
// per-section override (§4/§9d) — a PURE function, re-runnable on every reconfigure.
//
// Resolution: start from the global Bridge interface; if the named section carries an
// InterfaceProvider override with a non-empty Name, that override wins (per-section override
// beats bridge inheritance).
func (m *Model) EffectiveInterface(sectionKey string) InterfaceSection {
	eff := m.Bridge
	if s, ok := m.Sections[sectionKey]; ok {
		if ip, ok := s.(InterfaceProvider); ok {
			if ov := ip.Interface(); ov.Name != "" {
				return ov
			}
		}
	}
	return eff
}

// --- Well-known section value types (typed fields on Model for ergonomics). ---

// LoggingSection is the logging config (level, sinks).
type LoggingSection struct {
	Level string // "debug"|"info"|"warn"|"error"
}

// RouterSection is the router config (zone defaults, seed ranges).
type RouterSection struct {
	DefaultZone string
}

// InterfaceSection names a network interface a component (or the bridge) binds to.
type InterfaceSection struct {
	Name string // "eth0", "br-lan", "" = unset
	Addr string // optional pinned address
}

// InterfaceProvider is the optional capability a component Section implements when it can
// override the inherited bridge interface (§4/§9d). EffectiveInterface type-asserts it.
type InterfaceProvider interface {
	Interface() InterfaceSection
}

// --- Section schema registry (lets a component add config without editing a central struct). ---

// SectionSchema registers a component's config shape so codecs can round-trip it without
// knowing the type. New returns a zero section; Validate may wrap Section.Validate.
type SectionSchema struct {
	Key      string
	New      func() Section
	Validate func(Section) error
}

var (
	schemaMu sync.RWMutex
	schemas  = map[string]SectionSchema{}
)

// Register adds a section schema. Call from a component package init() or explicit wiring.
// A later Register for the same key replaces the earlier one (last wins), so a build can
// override a default schema.
func Register(s SectionSchema) {
	schemaMu.Lock()
	defer schemaMu.Unlock()
	schemas[s.Key] = s
}

// Schemas returns the registered schemas (codecs iterate these). Order is unspecified;
// callers that need determinism should sort by Key.
func Schemas() []SectionSchema {
	schemaMu.RLock()
	defer schemaMu.RUnlock()
	out := make([]SectionSchema, 0, len(schemas))
	for _, s := range schemas {
		out = append(out, s)
	}
	return out
}

// SchemaFor returns the schema registered under key, if any.
func SchemaFor(key string) (SectionSchema, bool) {
	schemaMu.RLock()
	defer schemaMu.RUnlock()
	s, ok := schemas[key]
	return s, ok
}

// --- Adapter seams (core ships none of these; adapters implement them). ---

// Codec converts the model to/from a byte representation (TOML, UCI, JSON) — ADAPTERS
// implement this; core ships none. Round-trip is the contract: Unmarshal(Marshal(m)) == m.
type Codec interface {
	Marshal(*Model) ([]byte, error)
	Unmarshal([]byte, *Model) error
}

// Store is where config bytes live and how they're versioned (file w/ numbered backups,
// UCI tree, in-mem) — ADAPTERS implement this. Save returns a revision id (backup path / commit).
type Store interface {
	Load() ([]byte, error)
	Save(data []byte) (revision string, err error)
}
