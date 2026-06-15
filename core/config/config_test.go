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
	m.Bridge = InterfaceSection{Name: "br-lan"}

	// No override → inherits the bridge.
	m.Set(&barSection{})
	if got := m.EffectiveInterface("Bar"); got.Name != "br-lan" {
		t.Fatalf("Bar should inherit bridge, got %q", got.Name)
	}

	// Per-section override wins.
	m.Set(&fooSection{Iface: InterfaceSection{Name: "eth2"}})
	if got := m.EffectiveInterface("Foo"); got.Name != "eth2" {
		t.Fatalf("Foo override should win, got %q", got.Name)
	}

	// Empty override falls back to the bridge.
	m.Set(&fooSection{Iface: InterfaceSection{}})
	if got := m.EffectiveInterface("Foo"); got.Name != "br-lan" {
		t.Fatalf("empty Foo override should fall back to bridge, got %q", got.Name)
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
