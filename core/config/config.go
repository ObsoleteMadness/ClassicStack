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
// ergonomics; singleton component sections live in Sections keyed by Section.Key(); repeated
// (named-instance) sections — e.g. one AFP volume per share — live in Lists keyed by the schema
// key, each instance distinguished by its InstanceName().
type Model struct {
	Identity Identity // server hostname/workgroup/description (§4-bis); owned by no service
	Logging  LoggingSection
	Router   RouterSection
	Bridge   InterfaceSection
	Sections map[string]Section   // registered singleton component sections
	Lists    map[string][]Section // registered repeated (named-instance) sections
}

// NewModel returns an empty model with initialised Sections / Lists maps.
func NewModel() *Model {
	return &Model{
		Sections: make(map[string]Section),
		Lists:    make(map[string][]Section),
	}
}

// Clone returns a deep copy. Each component Section deep-copies via its own Clone, so staging
// a change never mutates the live model.
func (m *Model) Clone() *Model {
	c := &Model{
		Identity: m.Identity.Clone(),
		Logging:  m.Logging,
		Router:   m.Router,
		Bridge:   m.Bridge,
		Sections: make(map[string]Section, len(m.Sections)),
		Lists:    make(map[string][]Section, len(m.Lists)),
	}
	for k, s := range m.Sections {
		c.Sections[k] = s.Clone()
	}
	for k, list := range m.Lists {
		cp := make([]Section, len(list))
		for i, s := range list {
			cp[i] = s.Clone()
		}
		c.Lists[k] = cp
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

// --- Repeated (named-instance) sections ----------------------------------------------------

// NamedSection is the capability a Section implements when it is one instance of a repeated
// section (e.g. a single AFP volume among several). InstanceName is the per-instance key the
// codec writes as the section name (UCI `config volume 'public'`, TOML array-of-tables) and the
// supervisor addresses the share by. Key() still returns the shared schema key ("AFPVolumes").
type NamedSection interface {
	Section
	InstanceName() string
}

// HostPathProvider is the optional capability a Section implements when it backs a
// host directory (an AFP volume / SMB share): HostPath returns that directory, or ""
// for a synthetic backend (memfs) that has none. Model.HostPaths collects them for
// the §10e host watcher, with no dependency on the file-service packages.
type HostPathProvider interface {
	HostPath() string
}

// HostPaths returns the distinct, non-empty host directories backing the model's
// repeated sections (AFP volumes / SMB shares), for the §10e host-filesystem watcher
// to watch. Order follows registration; duplicates (an AFP volume and SMB share on
// one path) are collapsed so the watcher adds each directory once.
func (m *Model) HostPaths() []string {
	seen := make(map[string]bool)
	var out []string
	for _, list := range m.Lists {
		for _, s := range list {
			hp, ok := s.(HostPathProvider)
			if !ok {
				continue
			}
			p := hp.HostPath()
			if p == "" || seen[p] {
				continue
			}
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// List returns the repeated sections registered under key (the registered instances of a
// repeated schema), or nil if none. The slice is the live one; callers that mutate it should
// Clone the model first.
func (m *Model) List(key string) []Section {
	if m.Lists == nil {
		return nil
	}
	return m.Lists[key]
}

// SetList replaces the whole instance set for a repeated section key.
func (m *Model) SetList(key string, sections []Section) {
	if m.Lists == nil {
		m.Lists = make(map[string][]Section)
	}
	m.Lists[key] = sections
}

// AddInstance appends (or, when an instance of the same InstanceName already exists, replaces)
// one named instance under its Key(). It is the repeated-section analogue of Set.
func (m *Model) AddInstance(s NamedSection) {
	if m.Lists == nil {
		m.Lists = make(map[string][]Section)
	}
	key := s.Key()
	list := m.Lists[key]
	for i, existing := range list {
		if ns, ok := existing.(NamedSection); ok && ns.InstanceName() == s.InstanceName() {
			list[i] = s
			m.Lists[key] = list
			return
		}
	}
	m.Lists[key] = append(list, s)
}

// Instance returns the named instance under key, if present.
func (m *Model) Instance(key, name string) (Section, bool) {
	for _, s := range m.List(key) {
		if ns, ok := s.(NamedSection); ok && ns.InstanceName() == name {
			return s, true
		}
	}
	return nil, false
}

// RemoveInstance drops the named instance under key, reporting whether it was present.
func (m *Model) RemoveInstance(key, name string) bool {
	list := m.List(key)
	for i, s := range list {
		if ns, ok := s.(NamedSection); ok && ns.InstanceName() == name {
			m.Lists[key] = append(list[:i:i], list[i+1:]...)
			return true
		}
	}
	return false
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
//
// Repeated marks a schema whose key carries MANY named instances (e.g. one AFP volume per
// share) rather than a single section. The codec then reads/writes the instances from/to
// Model.Lists[Key] (UCI: repeated `config <type> '<name>'` blocks; TOML: an array-of-tables),
// and New() must return a NamedSection. A singleton schema (Repeated == false) lives in
// Model.Sections[Key] as before.
type SectionSchema struct {
	Key      string
	New      func() Section
	Validate func(Section) error
	Repeated bool
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
