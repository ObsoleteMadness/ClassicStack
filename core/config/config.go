package config

import (
	"errors"
	"slices"
	"strconv"
	"strings"
	"sync"
)

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
	Identity  Identity  // server hostname/workgroup/description (§4-bis); owned by no service
	AdminAuth AdminAuth // web-management-interface admin credential (§4-ter); username + salted hash
	Logging   LoggingSection
	HTTP      HTTPSection   // web-admin listen address; default enabled on :1984
	Client    ClientSection // in-process file client (LAN scan / remote sessions); default disabled
	FUSE      FUSESection   // host FUSE/WinFsp mounts (connect timeout); auto-mounts in Lists[FUSEVolumes]
	Router    RouterSection
	// Interfaces is the named interface namespace (§M11): NIC / serial / bridge
	// entries a port references by name. A bridge is just one entry here; the entry
	// flagged Default (see DefaultInterface) is the shared interface an un-bound port
	// inherits — which is what the former singleton Model.Bridge did.
	Interfaces map[string]InterfaceSection
	Sections   map[string]Section   // registered singleton component sections
	Lists      map[string][]Section // registered repeated (named-instance) sections
}

// NewModel returns an empty model with initialised Sections / Lists maps.
func NewModel() *Model {
	return &Model{
		HTTP:     DefaultHTTP(),
		FUSE:     DefaultFUSE(),
		Sections: make(map[string]Section),
		Lists:    make(map[string][]Section),
	}
}

// Clone returns a deep copy. Each component Section deep-copies via its own Clone, so staging
// a change never mutates the live model.
func (m *Model) Clone() *Model {
	c := &Model{
		Identity:  m.Identity.Clone(),
		AdminAuth: m.AdminAuth.Clone(),
		Logging:   m.Logging,
		HTTP:      m.HTTP.Clone(),
		Client:    m.Client.Clone(),
		FUSE:      m.FUSE.Clone(),
		Router:    m.Router.Clone(),
		Sections:  make(map[string]Section, len(m.Sections)),
		Lists:     make(map[string][]Section, len(m.Lists)),
	}
	if m.Interfaces != nil {
		c.Interfaces = make(map[string]InterfaceSection, len(m.Interfaces))
		for k, iface := range m.Interfaces {
			c.Interfaces[k] = iface.Clone()
		}
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

// ValidateOptions carries the cross-cutting facts Model.Validate needs that the
// model alone cannot determine — chiefly which CONSUMER services are enabled, so a
// consumer-gated rule (the NetBIOS ≤15-byte hostname limit, §4-bis) applies only when
// that consumer is in play. The caller (the control plane / compose, which knows
// which components are built and enabled) supplies it; core/config has no service
// knowledge of its own. The zero value validates with no consumer constraints — the
// right default for an SMB-over-:445 / AFP-only server.
type ValidateOptions struct {
	// HostnameConstraints names the active CONSUMER-GATED hostname rules — the constraint
	// keys (e.g. HostnameConstraintNetBIOS) reported by the live components that impose
	// them. core/config gates each consumer rule on its key WITHOUT the caller naming a
	// service: the management plane aggregates the keys from the components implementing
	// component.HostnameConstrainer and passes them here. The zero value (no keys)
	// validates with only the always-on baseline — the right default for an
	// SMB-over-:445 / AFP-only server with no NetBIOS.
	HostnameConstraints []string
}

// Hostname-constraint keys for ValidateOptions.HostnameConstraints. A consumer that
// imposes a hostname rule (declared via component.HostnameConstrainer) uses its key here;
// core/config applies the matching rule. NetBIOS is the only one today (the ≤15-byte
// NetBIOS-name limit), but the key-set is the seam for any future consumer rule.
const HostnameConstraintNetBIOS = "netbios"

// hasConstraint reports whether key is among the active hostname constraints.
func (o ValidateOptions) hasConstraint(key string) bool {
	for _, k := range o.HostnameConstraints {
		if k == key {
			return true
		}
	}
	return false
}

// Validate checks the whole model before it is committed (the control-plane Apply /
// Save path, §4 / §4-bis). It runs, in order:
//
//  1. Identity.Validate — the always-on baseline hostname check.
//  2. well-known fields — AdminAuth, Client, FUSE, Logging, HTTP, Router, Interfaces.
//  3. every registered section's Validate — singletons in Sections and each repeated
//     instance in Lists, via the schema registry's Validate when one is registered
//     (it may wrap the section's own), else the section's own Validate.
//  4. Identity.ValidateForNetBIOS — the consumer-gated rule, only when the
//     HostnameConstraintNetBIOS key is among opts.HostnameConstraints, with NetBIOS
//     named as the constraint source in its error.
//
// It returns the first error encountered, so a bad section or an over-length hostname
// under NetBIOS is rejected before it goes live, rather than mangling a name on the
// wire. A nil/empty model validates clean.
func (m *Model) Validate(opts ValidateOptions) error {
	if err := m.Identity.Validate(); err != nil {
		return err
	}
	if err := m.AdminAuth.Validate(); err != nil {
		return err
	}
	if err := m.Client.Validate(); err != nil {
		return err
	}
	if err := m.FUSE.Validate(); err != nil {
		return err
	}
	if err := m.Logging.Validate(); err != nil {
		return err
	}
	if err := m.HTTP.Validate(); err != nil {
		return err
	}
	if err := m.Router.Validate(); err != nil {
		return err
	}
	for name, iface := range m.Interfaces {
		if strings.TrimSpace(iface.Name) == "" {
			iface.Name = name
		}
		if err := iface.Validate(); err != nil {
			return err
		}
	}
	for _, s := range m.Sections {
		if err := validateSection(s); err != nil {
			return err
		}
	}
	for _, list := range m.Lists {
		for _, s := range list {
			if err := validateSection(s); err != nil {
				return err
			}
		}
	}
	if opts.hasConstraint(HostnameConstraintNetBIOS) {
		if err := m.Identity.ValidateForNetBIOS(); err != nil {
			return err
		}
	}
	return nil
}

// validateSection runs the schema-registered Validate for a section's key when one
// exists (it may apply richer cross-field checks), else the section's own Validate.
func validateSection(s Section) error {
	if sch, ok := SchemaFor(s.Key()); ok && sch.Validate != nil {
		return sch.Validate(s)
	}
	return s.Validate()
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

// RedactedSecret is the placeholder a masked secret value is replaced with on the
// way OUT to a management front-end (control.Plane.Config). It is deliberately a
// fixed, recognisable sentinel: a UI that round-trips the model and submits it back
// unchanged sends RedactedSecret for any field it did not edit, and the inbound
// unmask (SecretMasker.Unmask) restores the real stored value rather than persisting
// the placeholder. A user who genuinely wants a literal value of these asterisks is
// not a case the compatibility-server posture needs to serve.
const RedactedSecret = "********"

// SecretMasker is the optional capability a Section implements when it carries
// secret-valued fields (a backend password in an AFP volume / SMB share's options).
// The control plane masks on the way out and unmasks on the way back in, so a secret
// never leaves the process in clear and a blind round-trip never overwrites it with
// the placeholder. A section that knows its own schema (which option keys its fs_type
// marks fs.Param.Secret) implements this; core/config and core/control stay free of
// any fs-type knowledge.
//
// The interface is value-clean: both methods return a fresh Section (a clone), never
// mutating the receiver — mirroring Clone — so masking the model for display never
// disturbs the live model.
type SecretMasker interface {
	// MaskedClone returns a deep copy with every secret-valued field replaced by
	// RedactedSecret. A field that is empty (no secret set) is left empty, not
	// masked, so the UI can tell "no password" from "password hidden".
	MaskedClone() Section
	// Unmask returns a deep copy in which any field still holding RedactedSecret is
	// restored from the corresponding field of prev (the live stored section). A
	// field the caller actually changed (anything other than the sentinel) is kept
	// verbatim. prev may be nil (a brand-new instance with no prior value), in which
	// case a sentinel-valued field is cleared rather than restored.
	Unmask(prev Section) Section
}

// MaskSecrets returns a clone of the model in which every SecretMasker section has its
// secret fields redacted (RedactedSecret). It is the shape the control plane hands to
// a front-end: the model is faithfully reproduced except that secrets read as the
// placeholder. Non-masking sections are copied unchanged.
func (m *Model) MaskSecrets() *Model {
	c := m.Clone()
	for k, s := range c.Sections {
		if sm, ok := s.(SecretMasker); ok {
			c.Sections[k] = sm.MaskedClone()
		}
	}
	for k, list := range c.Lists {
		for i, s := range list {
			if sm, ok := s.(SecretMasker); ok {
				list[i] = sm.MaskedClone()
			}
		}
		c.Lists[k] = list
	}
	return c
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

// EffectiveInterface resolves a component's interface, folding the named interface
// namespace + per-section override + default-interface inheritance (§4/§9d, §M11) —
// a PURE function, re-runnable on every reconfigure.
//
// Resolution order:
//  1. If the section carries an InterfaceProvider override with a non-empty Name,
//     that name is the reference; otherwise the section inherits — fall through to
//     the namespace's default interface (DefaultInterface).
//  2. The reference name is looked up in the Interfaces NAMESPACE: a matching entry
//     (with its Kind/params) wins, so a port that names "ttyUSB-attic" gets the
//     serial interface's device/baud, and one that names "br-lan" gets the bridge.
//  3. A name with no namespace entry resolves to a bare nic-kind InterfaceSection of
//     that name (a plain "eth0" needs no [[Interface]] block — back-compat).
func (m *Model) EffectiveInterface(sectionKey string) InterfaceSection {
	if s, ok := m.Sections[sectionKey]; ok {
		return m.EffectiveInterfaceFor(s)
	}
	return m.DefaultInterface()
}

// DefaultInterface returns the namespace's DEFAULT interface — the one a port
// inherits when it names no iface of its own (§M11). It replaces the former
// singleton Model.Bridge. Resolution:
//
//  1. the entry explicitly flagged Default (lowest name wins on a tie);
//  2. else the sole bridge-kind entry, if there is exactly one;
//  3. else the zero InterfaceSection (no default — an un-bound port runs on no
//     interface, the same inert-but-routed degradation a nil opener gives).
//
// It is a pure function over Interfaces, re-runnable on every reconfigure.
func (m *Model) DefaultInterface() InterfaceSection {
	var flagged, bridge InterfaceSection
	var bridgeCount int
	// Iterate in name order so a (misconfigured) multi-default set resolves
	// deterministically to the lowest name.
	names := make([]string, 0, len(m.Interfaces))
	for name := range m.Interfaces {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		iface := m.Interfaces[name]
		if iface.Default && flagged.Name == "" {
			flagged = iface
		}
		if iface.EffectiveKind() == IfaceKindBridge {
			bridge = iface
			bridgeCount++
		}
	}
	if flagged.Name != "" {
		return flagged
	}
	if bridgeCount == 1 {
		return bridge
	}
	return InterfaceSection{}
}

// EffectiveInterfaceFor resolves the effective interface for a SPECIFIC section
// value — the form a repeated transport INSTANCE needs, since instances live in
// Model.Lists keyed by schema key (several share one key) and so cannot be found by
// key alone. Resolution is identical to EffectiveInterface: the section's
// InterfaceProvider override (its named iface) wins, else inherit the namespace's
// default interface; the chosen reference is then resolved through the namespace.
func (m *Model) EffectiveInterfaceFor(s Section) InterfaceSection {
	if ip, ok := s.(InterfaceProvider); ok {
		if ov := ip.Interface(); ov.Name != "" {
			return m.ResolveInterface(ov)
		}
	}
	// No override: inherit the namespace's default interface.
	return m.DefaultInterface()
}

// ResolveInterface resolves a partial interface reference (typically just a Name)
// against the Interfaces namespace: a registered entry of that name wins (returning
// its full Kind/params), otherwise the reference is returned as-is (a bare,
// un-declared name is a plain nic). A reference with no Name is returned unchanged.
func (m *Model) ResolveInterface(ref InterfaceSection) InterfaceSection {
	if ref.Name == "" {
		return ref
	}
	if m.Interfaces != nil {
		if got, ok := m.Interfaces[ref.Name]; ok {
			return got
		}
	}
	return ref
}

// MigrateLegacyBridge folds a pre-M11 singleton [bridge] section into the interface
// namespace as a default entry, so an old config keeps working after the singleton
// Model.Bridge was removed. It is a no-op when the legacy section is empty (Name and
// Device both unset) or when a namespace entry of the same name already exists (the
// new-form [[interface]] wins — no clobbering an explicit modern config). The
// migrated entry is flagged Default and, lacking any other kind, typed as a bridge,
// preserving the old "un-bound ports inherit the bridge" behaviour. Codecs call this
// from Unmarshal after reading both the legacy block and the namespace.
func (m *Model) MigrateLegacyBridge(legacy InterfaceSection) {
	if legacy.Name == "" && legacy.Device == "" && legacy.Addr == "" && legacy.HWAddress == "" {
		return // nothing configured in the legacy block
	}
	name := legacy.Name
	if name == "" {
		name = IfaceKindBridge // an unnamed legacy bridge takes the canonical name
	}
	if _, ok := m.Interface(name); ok {
		return // a modern [[interface]] of this name already exists — do not clobber
	}
	legacy.Name = name
	legacy.Default = true
	if legacy.Kind == "" {
		legacy.Kind = IfaceKindBridge
	}
	m.SetInterface(legacy)
}

// Interface returns the named namespace entry, if present.
func (m *Model) Interface(name string) (InterfaceSection, bool) {
	if m.Interfaces == nil {
		return InterfaceSection{}, false
	}
	got, ok := m.Interfaces[name]
	return got, ok
}

// SetInterface adds or replaces a namespace entry under its Name.
func (m *Model) SetInterface(s InterfaceSection) {
	if s.Name == "" {
		return
	}
	if m.Interfaces == nil {
		m.Interfaces = make(map[string]InterfaceSection)
	}
	m.Interfaces[s.Name] = s
}

// --- Well-known section value types (typed fields on Model for ergonomics). ---

// LoggingSection is the logging config (level, sinks).
type LoggingSection struct {
	Level string `toml:"level,omitempty"` // "trace"|"debug"|"info"|"warn"|"error"
}

// Validate checks the log level is a known threshold (empty = info).
func (s LoggingSection) Validate() error {
	switch strings.ToLower(strings.TrimSpace(s.Level)) {
	case "", "trace", "debug", "info", "warn", "warning", "error":
		return nil
	default:
		return errors.New("config: unknown log level " + strconv.Quote(s.Level) + " (want trace, debug, info, warn, error)")
	}
}

// RouterSection is the AppleTalk router config (default zone) and — §3d/D8 — the
// EXPLICIT membership list naming which AppleTalk PORTS join the router. Members
// are PORT instance names (which default to the transport schema key — "EtherTalk",
// "LToUDP", "TashTalk" — unless a port sets its own Name). Each member port carries
// its OWN seed zone + network range on its port Section (a seed is an RTMP property
// of the seed-router port on that segment); the router does not store per-member
// seed here. Membership is opt-IN by name: an enabled port NOT listed comes up
// standalone (sends/receives on its own segment, but no RTMP/ZIP/forwarding). An
// empty Members means NONE join (D9) — the greenfield stance is
// explicit-over-implicit, so first-run setup seeds Members rather than defaulting
// to every enabled transport.
type RouterSection struct {
	DefaultZone string   `toml:"default_zone,omitempty"`
	Members     []string `toml:"members,omitempty"` // instance names of the ports that join this router
}

// Clone returns a deep copy (Members is the only reference-typed field).
func (s RouterSection) Clone() RouterSection {
	cp := s
	if s.Members != nil {
		cp.Members = append([]string(nil), s.Members...)
	}
	return cp
}

// Validate checks the default zone has no control characters and members are named.
func (s RouterSection) Validate() error {
	for _, r := range s.DefaultZone {
		if r < 0x20 || r == 0x7f {
			return errors.New("config: router default_zone contains an illegal character")
		}
	}
	for _, name := range s.Members {
		if strings.TrimSpace(name) == "" {
			return errors.New("config: router members must not contain an empty name")
		}
	}
	return nil
}

// IsMember reports whether the named port instance is declared a member of the
// router (§3d). Unlisted instances run standalone; an empty Members lists none.
func (s RouterSection) IsMember(instance string) bool {
	return slices.Contains(s.Members, instance)
}

// Interface kinds (Model.Interfaces / InterfaceSection.Kind). A port references an
// interface by name; the interface's KIND — not the port type — selects which link
// opener the compose layer uses (pcap for nic, adapter/serial for serial). An empty
// Kind on a NIC is the historical default and is treated as IfaceKindNIC.
const (
	IfaceKindNIC    = "nic"    // a network interface (eth0); opened via pcap/rawsock/tap
	IfaceKindSerial = "serial" // a UART/serial device (COM3, /dev/ttyUSB0); opened via adapter/serial
	IfaceKindWifi   = "wifi"   // a wireless interface; opened via wifi driver
	IfaceKindBridge = "bridge" // a virtual interface aggregating member NICs (the former singleton Bridge)
	// IfaceKindMulticast is the LToUDP segment's interface: it rides UDP multicast
	// (239.192.76.84:1954) rather than binding a specific device, so its "device" is
	// the host itself — there is no NIC/serial to pick. The runtime joins the group
	// on every multicast-capable interface; the namespace entry exists only so the
	// UI can present LToUDP alongside the other segments.
	IfaceKindMulticast = "multicast"
)

// InterfaceSection names an interface a component binds to. It is a SUPERSET across
// kinds (the same "placeholder accepts anything" stance the port Section takes): a
// nic reads Name/Addr, a serial reads Device/Baud, a wifi reads SSID/Key; each
// ignores the fields that do not apply to its Kind. Every field is omitempty so a
// given kind's config emits only the fields it uses. (Aggregating "members" is a
// property of the AppleTalk router — RouterSection.Members — not of an interface.)
type InterfaceSection struct {
	Name string `toml:"name,omitempty"` // namespace key the interface is referenced by ("eth0", "ttyUSB-attic"); "" = unset
	Kind string `toml:"kind,omitempty"` // "" / "nic" / "serial" / "wifi" (see IfaceKind*); "" == nic
	Addr string `toml:"addr,omitempty"` // nic: optional pinned address
	// Default marks this entry the namespace's DEFAULT interface: the one a port
	// inherits when it names no iface of its own (§M11). It replaces the former
	// singleton Model.Bridge — a bridge is now just an ordinary namespace entry, and
	// the one flagged Default is the shared interface un-bound ports fall through to.
	// At most one entry should carry it; Model.DefaultInterface resolves ties by name
	// order and falls back to a lone bridge entry when none is flagged.
	Default bool `toml:"default,omitempty"`
	// Backend selects the LINK IMPLEMENTATION used to open a kind=nic interface:
	// "pcap" (libpcap/Npcap raw capture — the default and only backend wired today),
	// "tap" (an L2 TAP virtual device), or "tun" (an L3 TUN device). It is meaningful
	// only for nic interfaces; serial ignores it. Empty defaults to pcap (the
	// historical behaviour). The cmd-edge opener dispatches on it; an unimplemented
	// backend falls back to inert-but-routed, the same graceful degradation as a nil
	// opener (see IfaceBackend*).
	Backend string `toml:"backend,omitempty"`

	// HWAddress is the station hardware (MAC) address shared by every port bound to
	// this interface that does not pin its own MAC. It is the successor to the legacy
	// [Bridge] hw_address: a NIC-bound transport (NetBEUI, IPX, EtherDFS, EtherTalk)
	// stamps it as the Ethernet source when its own section's mac is empty, so a
	// bridge/interface can carry one identity for all its raw-link consumers. Empty =
	// auto-detect the NIC's own hardware address (required on WiFi / Npcap: APs drop
	// frames sourced from any other MAC). Setting a value is opt-in spoofing for wired
	// bridges (e.g. "DE:AD:BE:EF:CA:FE"). Six hex octets, colon/dash-separated.
	HWAddress string `toml:"hw_address,omitempty"`

	// Embedded network configuration (IP configuration)
	Proto      string `toml:"proto,omitempty"`      // "dhcp" or "static"
	Controller string `toml:"controller,omitempty"` // ethernet: "lan8720" or "w5500"
	IP         string `toml:"ip,omitempty"`         // static IP address (e.g. "192.168.1.200")
	Netmask    string `toml:"netmask,omitempty"`    // subnet mask (e.g. "255.255.255.0")
	Gateway    string `toml:"gateway,omitempty"`    // gateway address (e.g. "192.168.1.1")
	DNS        string `toml:"dns,omitempty"`        // DNS server address (e.g. "8.8.8.8")

	// Wireless (SSID/Key) parameters.
	SSID string `toml:"ssid,omitempty"` // WiFi SSID
	Key  string `toml:"key,omitempty"`  // WiFi Key/Password

	// Serial-kind parameters.
	Device string `toml:"device,omitempty"` // serial: OS device path ("COM3", "/dev/ttyUSB0")
	Baud   int    `toml:"baud,omitempty"`   // serial: line speed (0 → adapter default)
	// NoFlowControl disables RTS/CTS, which is ON by default (TashTalk needs it to
	// throttle the host link; see adapter/serial.DefaultRTSCTS).
	NoFlowControl bool `toml:"no_flow_control,omitempty"`
}

// NIC link-backend identifiers (InterfaceSection.Backend, kind=nic). pcap is the only
// backend wired today; tap/tun are accepted in config and resolved by the cmd-edge
// opener when their adapters land, falling back to inert until then.
const (
	IfaceBackendPcap = "pcap" // libpcap / Npcap raw capture (default)
	IfaceBackendTap  = "tap"  // L2 TAP virtual device
	IfaceBackendTun  = "tun"  // L3 TUN device
)

// EffectiveBackend returns the nic link backend, defaulting an empty Backend to pcap.
func (s InterfaceSection) EffectiveBackend() string {
	if s.Backend == "" {
		return IfaceBackendPcap
	}
	return s.Backend
}

// EffectiveKind returns the interface's kind, defaulting an empty Kind to nic (the
// historical meaning of a bare interface name).
func (s InterfaceSection) EffectiveKind() string {
	if s.Kind == "" {
		return IfaceKindNIC
	}
	return s.Kind
}

// PcapDevice returns the string libpcap/Npcap must be handed to open this nic
// interface: the explicit Device when set, otherwise the namespace Name. On Linux
// the friendly name IS the pcap device (Name = "eth0"), so Device is left empty and
// this falls through to Name; on Windows Npcap wants the "\Device\NPF_{GUID}" string,
// which does not match any friendly name, so it is stored in Device and returned here.
// This is the nic analogue of serial reading Device for the OS path.
func (s InterfaceSection) PcapDevice() string {
	if s.Device != "" {
		return s.Device
	}
	return s.Name
}

// Clone returns a copy. All fields are value types, so a plain struct copy suffices.
func (s InterfaceSection) Clone() InterfaceSection {
	return s
}

// Validate checks the interface has a name and a known kind.
func (s InterfaceSection) Validate() error {
	if strings.TrimSpace(s.Name) == "" {
		return errors.New("config: interface name is required")
	}
	switch strings.ToLower(strings.TrimSpace(s.Kind)) {
	case "", IfaceKindNIC, IfaceKindSerial, IfaceKindWifi, IfaceKindBridge, IfaceKindMulticast:
		return nil
	default:
		return errors.New("config: unknown interface kind " + strconv.Quote(s.Kind))
	}
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
//
// DisplayName / Description / Capabilities are optional management metadata a front-end
// discovers via the schema API so new protocols light up without dedicated UI code.
// Fields, when set, are the explicit field schema; when empty, adapters may reflect them
// from New()'s concrete type (core itself never reflects).
type SectionSchema struct {
	Key          string
	New          func() Section
	Validate     func(Section) error
	Repeated     bool
	DisplayName  string
	Description  string
	Capabilities []string    // CapCapture, CapIPXNetwork, … — see fieldinfo.go
	Fields       []FieldInfo // optional explicit field list; else adapter-reflected
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
