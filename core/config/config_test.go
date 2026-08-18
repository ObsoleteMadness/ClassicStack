package config

import (
	"errors"
	"strings"
	"testing"
)

// --- two fake component sections ---

type fooSection struct {
	Enabled bool
	Iface   InterfaceSection
}

func (s *fooSection) Key() string { return "Foo" }
func (s *fooSection) Clone() Section {
	c := *s
	return &c
}
func (s *fooSection) Validate() error { return nil }

// fooSection overrides its interface (exercises EffectiveInterface).
func (s *fooSection) Interface() InterfaceSection { return s.Iface }

type barSection struct {
	Count int
}

func (s *barSection) Key() string { return "Bar" }
func (s *barSection) Clone() Section {
	c := *s
	return &c
}
func (s *barSection) Validate() error {
	if s.Count < 0 {
		return errors.New("bar: count must be >= 0")
	}
	return nil
}

func TestModelValidate(t *testing.T) {
	// Happy path: clean identity + a valid section.
	m := NewModel()
	m.Identity = Identity{Hostname: "CLASSICSTACK", Workgroup: "WG"}
	m.Set(&barSection{Count: 1})
	if err := m.Validate(ValidateOptions{}); err != nil {
		t.Fatalf("valid model should pass: %v", err)
	}

	badLog := NewModel()
	badLog.Logging.Level = "nope"
	if err := badLog.Validate(ValidateOptions{}); err == nil {
		t.Fatal("unknown log level should fail Validate")
	}

	badHTTP := NewModel()
	badHTTP.HTTP.Addr = "not-a-port"
	if err := badHTTP.Validate(ValidateOptions{}); err == nil {
		t.Fatal("invalid HTTP listen address should fail Validate")
	}

	// Bad identity (control char) → rejected regardless of NetBIOS.
	bad := NewModel()
	bad.Identity = Identity{Hostname: "bad\x01name"}
	if err := bad.Validate(ValidateOptions{}); err == nil {
		t.Fatal("identity with a control char should fail Validate")
	}

	// Bad section → rejected.
	badSec := NewModel()
	badSec.Set(&barSection{Count: -1})
	if err := badSec.Validate(ValidateOptions{}); err == nil {
		t.Fatal("a section that fails its own Validate should fail Model.Validate")
	}

	// Bad repeated instance → rejected too.
	badList := NewModel()
	badList.SetList("Bars", []Section{&barSection{Count: -1}})
	if err := badList.Validate(ValidateOptions{}); err == nil {
		t.Fatal("a repeated instance that fails Validate should fail Model.Validate")
	}
}

func TestModelValidateNetBIOSGated(t *testing.T) {
	m := NewModel()
	m.Identity = Identity{Hostname: "THIS-NAME-IS-WAY-TOO-LONG"} // > 15 bytes, baseline-legal

	if err := m.Validate(ValidateOptions{}); err != nil {
		t.Fatalf("no netbios constraint: long hostname should be allowed: %v", err)
	}
	if err := m.Validate(ValidateOptions{HostnameConstraints: []string{HostnameConstraintNetBIOS}}); err == nil {
		t.Fatal("netbios constraint active: over-length hostname should be rejected")
	}
}

func TestRegisterAndSchemas(t *testing.T) {
	Register(SectionSchema{Key: "Foo", New: func() Section { return &fooSection{} }})
	Register(SectionSchema{Key: "Bar", New: func() Section { return &barSection{} }})

	if _, ok := SchemaFor("Foo"); !ok {
		t.Fatal("Foo schema not registered")
	}
	if len(Schemas()) < 2 {
		t.Fatalf("expected >=2 schemas, got %d", len(Schemas()))
	}
}

func TestCloneIsIndependent(t *testing.T) {
	m := NewModel()
	m.Set(&fooSection{Enabled: true})
	m.Logging = LoggingSection{Level: "info"}

	c := m.Clone()
	// Mutate the clone's section and well-known field.
	c.Sections["Foo"].(*fooSection).Enabled = false
	c.Logging.Level = "debug"

	if !m.Sections["Foo"].(*fooSection).Enabled {
		t.Fatal("clone mutated the original section")
	}
	if m.Logging.Level != "info" {
		t.Fatal("clone mutated the original Logging")
	}
}

func TestEffectiveInterface(t *testing.T) {
	m := NewModel()
	// The default interface is now a namespace entry flagged Default (the former
	// singleton Bridge).
	m.SetInterface(InterfaceSection{Name: "br-lan", Kind: IfaceKindBridge, Default: true})

	// No override → inherits the default interface.
	m.Set(&barSection{})
	if got := m.EffectiveInterface("Bar"); got.Name != "br-lan" {
		t.Fatalf("Bar should inherit the default interface, got %q", got.Name)
	}

	// Per-section override wins.
	m.Set(&fooSection{Iface: InterfaceSection{Name: "eth2"}})
	if got := m.EffectiveInterface("Foo"); got.Name != "eth2" {
		t.Fatalf("Foo override should win, got %q", got.Name)
	}

	// Empty override falls back to the default interface.
	m.Set(&fooSection{Iface: InterfaceSection{}})
	if got := m.EffectiveInterface("Foo"); got.Name != "br-lan" {
		t.Fatalf("empty Foo override should fall back to the default interface, got %q", got.Name)
	}
}

func TestDefaultInterface(t *testing.T) {
	// A lone bridge entry is the default even without the flag.
	m := NewModel()
	m.SetInterface(InterfaceSection{Name: "br0", Kind: IfaceKindBridge})
	if got := m.DefaultInterface(); got.Name != "br0" {
		t.Fatalf("lone bridge should be the default, got %q", got.Name)
	}

	// An explicit Default flag wins over a bridge.
	m.SetInterface(InterfaceSection{Name: "eth0", Kind: IfaceKindNIC, Default: true})
	if got := m.DefaultInterface(); got.Name != "eth0" {
		t.Fatalf("flagged entry should win, got %q", got.Name)
	}

	// With multiple bridges and no flag, there is no default (ambiguous).
	m2 := NewModel()
	m2.SetInterface(InterfaceSection{Name: "br0", Kind: IfaceKindBridge})
	m2.SetInterface(InterfaceSection{Name: "br1", Kind: IfaceKindBridge})
	if got := m2.DefaultInterface(); got.Name != "" {
		t.Fatalf("ambiguous bridges should yield no default, got %q", got.Name)
	}
}

func TestMigrateLegacyBridge(t *testing.T) {
	// A legacy [bridge] block becomes a default bridge entry in the namespace.
	m := NewModel()
	m.MigrateLegacyBridge(InterfaceSection{Name: "br-lan", Addr: "10.0.0.1"})
	got, ok := m.Interface("br-lan")
	if !ok {
		t.Fatal("legacy bridge was not migrated into the namespace")
	}
	if !got.Default || got.EffectiveKind() != IfaceKindBridge || got.Addr != "10.0.0.1" {
		t.Fatalf("migrated entry wrong: %+v", got)
	}

	// An unnamed legacy bridge takes the canonical "bridge" name.
	m2 := NewModel()
	m2.MigrateLegacyBridge(InterfaceSection{Device: `\Device\NPF_{X}`})
	if _, ok := m2.Interface(IfaceKindBridge); !ok {
		t.Fatal("unnamed legacy bridge should take the canonical name")
	}

	// A modern [[interface]] of the same name is not clobbered.
	m3 := NewModel()
	m3.SetInterface(InterfaceSection{Name: "br-lan", Kind: IfaceKindNIC, Addr: "modern"})
	m3.MigrateLegacyBridge(InterfaceSection{Name: "br-lan", Addr: "legacy"})
	if got, _ := m3.Interface("br-lan"); got.Addr != "modern" {
		t.Fatalf("modern entry should win over legacy, got Addr %q", got.Addr)
	}

	// An empty legacy block is a no-op.
	m4 := NewModel()
	m4.MigrateLegacyBridge(InterfaceSection{})
	if len(m4.Interfaces) != 0 {
		t.Fatalf("empty legacy block should not create an entry, got %d", len(m4.Interfaces))
	}
}

// TestEffectiveInterface_ResolvesNamespace proves a port's named interface is
// resolved against the Interfaces namespace: a port that names a serial interface
// gets that entry's Kind/Device/Baud, one that names a nic gets its Addr, and a bare
// undeclared name resolves to a plain nic.
func TestEffectiveInterface_ResolvesNamespace(t *testing.T) {
	m := NewModel()
	m.SetInterface(InterfaceSection{Name: "ttyUSB-attic", Kind: IfaceKindSerial, Device: "/dev/ttyUSB0", Baud: 1000000})
	m.SetInterface(InterfaceSection{Name: "eth0", Kind: IfaceKindNIC, Addr: "10.0.0.2"})

	// Names a serial interface → full serial entry.
	m.Set(&fooSection{Iface: InterfaceSection{Name: "ttyUSB-attic"}})
	got := m.EffectiveInterface("Foo")
	if got.EffectiveKind() != IfaceKindSerial || got.Device != "/dev/ttyUSB0" || got.Baud != 1000000 {
		t.Fatalf("serial ref should resolve to the namespace entry, got %+v", got)
	}

	// Names a declared nic interface → nic entry with its pinned Addr.
	m.Set(&fooSection{Iface: InterfaceSection{Name: "eth0"}})
	got = m.EffectiveInterface("Foo")
	if got.EffectiveKind() != IfaceKindNIC || got.Addr != "10.0.0.2" {
		t.Fatalf("nic ref should resolve to the namespace entry, got %+v", got)
	}

	// A bare, undeclared name is a plain nic (no [[Interface]] block required).
	m.Set(&fooSection{Iface: InterfaceSection{Name: "eth9"}})
	got = m.EffectiveInterface("Foo")
	if got.Name != "eth9" || got.EffectiveKind() != IfaceKindNIC {
		t.Fatalf("undeclared name should resolve to a nic, got %+v", got)
	}
}

// TestInterfaceNamespaceAccessors covers Set/Interface/Clone of the namespace.
func TestInterfaceNamespaceAccessors(t *testing.T) {
	m := NewModel()
	m.SetInterface(InterfaceSection{Name: "eth0", Kind: IfaceKindNIC, Addr: "10.0.0.2"})
	m.SetInterface(InterfaceSection{}) // empty name is ignored
	if _, ok := m.Interface(""); ok {
		t.Fatal("empty-name interface should not be stored")
	}
	got, ok := m.Interface("eth0")
	if !ok || got.Addr != "10.0.0.2" {
		t.Fatalf("Interface(eth0) = %+v, %v", got, ok)
	}

	// Clone must copy the namespace entry so a mutation does not leak across the clone.
	c := m.Clone()
	got.Addr = "MUTATED"
	m.SetInterface(got)
	cl, _ := c.Interface("eth0")
	if cl.Addr == "MUTATED" {
		t.Fatal("Clone shares the interface namespace with the original")
	}
}

func TestValidate(t *testing.T) {
	if err := (&barSection{Count: -1}).Validate(); err == nil {
		t.Fatal("expected validation error for negative count")
	}
	if err := (&barSection{Count: 3}).Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

// --- in-memory Codec + Store round-trip (the B6 acceptance check) ---
//
// encodableSection is a test-only seam letting the fake sections marshal
// themselves without reflection, so the round-trip codec stays reflection-free.
type encodableSection interface {
	Section
	marshal() string
	unmarshal(string)
}

func (s *fooSection) marshal() string {
	if s.Enabled {
		return "enabled=1;iface=" + s.Iface.Name
	}
	return "enabled=0;iface=" + s.Iface.Name
}
func (s *fooSection) unmarshal(v string) {
	for kv := range strings.SplitSeq(v, ";") {
		k, val, _ := strings.Cut(kv, "=")
		switch k {
		case "enabled":
			s.Enabled = val == "1"
		case "iface":
			s.Iface.Name = val
		}
	}
}

func (s *barSection) marshal() string    { return "count=" + itoa(s.Count) }
func (s *barSection) unmarshal(v string) { _, val, _ := strings.Cut(v, "="); s.Count = atoi(val) }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func atoi(s string) int {
	n, neg := 0, false
	for i, c := range s {
		if i == 0 && c == '-' {
			neg = true
			continue
		}
		n = n*10 + int(c-'0')
	}
	if neg {
		return -n
	}
	return n
}

// memCodec serialises Logging + each registered section, line per section.
type memCodec struct{}

func (memCodec) Marshal(m *Model) ([]byte, error) {
	var sb strings.Builder
	sb.WriteString("Logging:" + m.Logging.Level + "\n")
	for _, sc := range Schemas() {
		s, ok := m.Get(sc.Key)
		if !ok {
			continue
		}
		es, ok := s.(encodableSection)
		if !ok {
			continue
		}
		sb.WriteString(sc.Key + ":" + es.marshal() + "\n")
	}
	return []byte(sb.String()), nil
}

func (memCodec) Unmarshal(data []byte, m *Model) error {
	for line := range strings.SplitSeq(strings.TrimRight(string(data), "\n"), "\n") {
		key, val, _ := strings.Cut(line, ":")
		if key == "Logging" {
			m.Logging.Level = val
			continue
		}
		sc, ok := SchemaFor(key)
		if !ok {
			continue
		}
		s := sc.New()
		s.(encodableSection).unmarshal(val)
		m.Set(s)
	}
	return nil
}

// memStore keeps the bytes in memory.
type memStore struct{ data []byte }

func (s *memStore) Load() ([]byte, error) { return s.data, nil }
func (s *memStore) Save(d []byte) (string, error) {
	s.data = append([]byte(nil), d...)
	return "rev-1", nil
}

func TestCodecStoreRoundTrip(t *testing.T) {
	Register(SectionSchema{Key: "Foo", New: func() Section { return &fooSection{} }})
	Register(SectionSchema{Key: "Bar", New: func() Section { return &barSection{} }})

	m := NewModel()
	m.Logging = LoggingSection{Level: "warn"}
	m.Set(&fooSection{Enabled: true, Iface: InterfaceSection{Name: "eth0"}})
	m.Set(&barSection{Count: 7})

	var codec Codec = memCodec{}
	var store Store = &memStore{}

	data, err := codec.Marshal(m)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	rev, err := store.Save(data)
	if err != nil || rev == "" {
		t.Fatalf("Save: rev=%q err=%v", rev, err)
	}

	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := NewModel()
	if err := codec.Unmarshal(loaded, got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if got.Logging.Level != "warn" {
		t.Fatalf("Logging round-trip: got %q", got.Logging.Level)
	}
	foo := got.Sections["Foo"].(*fooSection)
	if !foo.Enabled || foo.Iface.Name != "eth0" {
		t.Fatalf("Foo round-trip: %+v", foo)
	}
	bar := got.Sections["Bar"].(*barSection)
	if bar.Count != 7 {
		t.Fatalf("Bar round-trip: %+v", bar)
	}
}

// --- repeated (named-instance) sections ---

type volSection struct {
	VName string
	Path  string
}

func (s *volSection) Key() string          { return "Vols" }
func (s *volSection) InstanceName() string { return s.VName }
func (s *volSection) Clone() Section       { c := *s; return &c }
func (s *volSection) Validate() error      { return nil }
func (s *volSection) HostPath() string     { return s.Path }

func TestHostPathsDistinctNonEmpty(t *testing.T) {
	m := NewModel()
	m.AddInstance(&volSection{VName: "a", Path: "/srv/a"})
	m.AddInstance(&volSection{VName: "b", Path: "/srv/b"})
	m.AddInstance(&volSection{VName: "c", Path: "/srv/a"}) // duplicate path → collapsed
	m.AddInstance(&volSection{VName: "d", Path: ""})       // synthetic backend → skipped

	paths := m.HostPaths()
	if len(paths) != 2 {
		t.Fatalf("HostPaths = %v, want 2 distinct non-empty", paths)
	}
	seen := map[string]bool{}
	for _, p := range paths {
		seen[p] = true
	}
	if !seen["/srv/a"] || !seen["/srv/b"] {
		t.Fatalf("HostPaths = %v, want /srv/a and /srv/b", paths)
	}
}

func TestAddInstanceAndList(t *testing.T) {
	m := NewModel()
	m.AddInstance(&volSection{VName: "a", Path: "/a"})
	m.AddInstance(&volSection{VName: "b", Path: "/b"})
	if got := len(m.List("Vols")); got != 2 {
		t.Fatalf("List len = %d, want 2", got)
	}
	// Same-name AddInstance replaces in place (order preserved).
	m.AddInstance(&volSection{VName: "a", Path: "/a2"})
	list := m.List("Vols")
	if len(list) != 2 || list[0].(*volSection).Path != "/a2" {
		t.Fatalf("replace failed: %+v", list)
	}
}

func TestInstanceLookupAndRemove(t *testing.T) {
	m := NewModel()
	m.AddInstance(&volSection{VName: "a"})
	m.AddInstance(&volSection{VName: "b"})

	if _, ok := m.Instance("Vols", "b"); !ok {
		t.Fatal("Instance(b) not found")
	}
	if _, ok := m.Instance("Vols", "zzz"); ok {
		t.Fatal("Instance(zzz) should not be found")
	}
	if !m.RemoveInstance("Vols", "a") {
		t.Fatal("RemoveInstance(a) should report present")
	}
	if m.RemoveInstance("Vols", "a") {
		t.Fatal("second RemoveInstance(a) should report absent")
	}
	if got := len(m.List("Vols")); got != 1 {
		t.Fatalf("after remove List len = %d, want 1", got)
	}
}

func TestCloneCopiesLists(t *testing.T) {
	m := NewModel()
	m.AddInstance(&volSection{VName: "a", Path: "/a"})
	c := m.Clone()
	c.List("Vols")[0].(*volSection).Path = "/changed"
	if m.List("Vols")[0].(*volSection).Path != "/a" {
		t.Fatal("Clone aliased the repeated-section list")
	}
}
