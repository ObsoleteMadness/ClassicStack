package zip

import (
	"bytes"
	"sync"
	"testing"
	"time"

	"github.com/pgodw/omnitalk/appletalk"
	"github.com/pgodw/omnitalk/service"
)

func newMockPort(network uint16, node uint8, shortString string, isExtended bool) *mockPort {
	return &mockPort{
		networkFunc:         func() uint16 { return network },
		nodeFunc:            func() uint8 { return node },
		shortStringFunc:     func() string { return shortString },
		extendedNetworkFunc: func() bool { return isExtended },
		broadcastFunc:       func(datagram appletalk.Datagram) {},
		multicastFunc:       func(zoneName []byte, datagram appletalk.Datagram) {},
		unicastFunc:         func(network uint16, node uint8, datagram appletalk.Datagram) {},
	}
}

func newMockRouter() *mockRouter {
	return &mockRouter{
		routeFunc:               func(datagram appletalk.Datagram, originating bool) error { return nil },
		routingGetByNetworkFunc: func(network uint16) (*service.RouteEntry, *bool) { return nil, nil },
		zonesInNetworkRangeFunc: func(networkMin uint16, networkMax *uint16) ([][]byte, error) { return nil, nil },
		networksInZoneFunc:      func(zoneName []byte) []uint16 { return nil },
	}
}

func TestNameInformationService_BrRq(t *testing.T) {
	svc := NewNameInformationService()
	r := newMockRouter()

	// Track routed packets
	var routedPackets []appletalk.Datagram
	var mu sync.Mutex
	r.routeFunc = func(datagram appletalk.Datagram, originating bool) error {
		mu.Lock()
		routedPackets = append(routedPackets, datagram)
		mu.Unlock()
		return nil
	}

	err := svc.Start(r)
	if err != nil {
		t.Fatalf("Failed to start service: %v", err)
	}
	defer svc.Stop()

	svc.RegisterName([]byte("TestObj"), []byte("TestType"), []byte("TestZone"), 123)

	p := newMockPort(10, 15, "mock-port", false)

	// Create BrRq datagram
	// Layout: funcTupleCount(1) nbp_id(1) network(2) node(1) socket(1) enum(1)
	// obj_len(1) obj(N) type_len(1) type(M) zone_len(1) zone(K)
	data := []byte{
		(nbpCtrlBrRq << 4) | 1, 42, 0, 10, 15, 45, 0,
		7, 'T', 'e', 's', 't', 'O', 'b', 'j',
		8, 'T', 'e', 's', 't', 'T', 'y', 'p', 'e',
		8, 'T', 'e', 's', 't', 'Z', 'o', 'n', 'e',
	}

	d := appletalk.Datagram{
		DDPType: NBPDDPType,
		Data:    data,
	}

	svc.Inbound(d, p)

	// Wait briefly for the goroutine to process
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(routedPackets) != 1 {
		t.Fatalf("Expected 1 routed packet, got %d", len(routedPackets))
	}
	rply := routedPackets[0]
	if rply.DestinationNetwork != 10 || rply.DestinationNode != 15 || rply.DestinationSocket != 45 {
		t.Errorf("Routed packet has wrong destination: %+v", rply)
	}
}

func TestNameInformationService_LkUp(t *testing.T) {
	svc := NewNameInformationService()
	r := newMockRouter()

	// Track routed packets
	var routedPackets []appletalk.Datagram
	var mu sync.Mutex
	r.routeFunc = func(datagram appletalk.Datagram, originating bool) error {
		mu.Lock()
		routedPackets = append(routedPackets, datagram)
		mu.Unlock()
		return nil
	}

	err := svc.Start(r)
	if err != nil {
		t.Fatalf("Failed to start service: %v", err)
	}
	defer svc.Stop()

	svc.RegisterName([]byte("Obj2"), []byte("Type2"), []byte("Zone2"), 200)

	p := newMockPort(20, 25, "mock-port2", false)

	// Create LkUp datagram
	data := []byte{
		(nbpCtrlLkUp << 4) | 1, 99, 0, 20, 25, 55, 0,
		4, 'O', 'b', 'j', '2',
		5, 'T', 'y', 'p', 'e', '2',
		5, 'Z', 'o', 'n', 'e', '2',
	}

	d := appletalk.Datagram{
		DDPType: NBPDDPType,
		Data:    data,
	}

	svc.Inbound(d, p)

	// Wait briefly for the goroutine to process
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(routedPackets) != 1 {
		t.Fatalf("Expected 1 routed packet, got %d", len(routedPackets))
	}
	rply := routedPackets[0]
	if rply.DestinationNetwork != 20 || rply.DestinationNode != 25 || rply.DestinationSocket != 55 {
		t.Errorf("Routed packet has wrong destination: %+v", rply)
	}
}

func TestNameInformationService_LkUpZoneWildcard(t *testing.T) {
	svc := NewNameInformationService()
	r := newMockRouter()

	var routedPackets []appletalk.Datagram
	var mu sync.Mutex
	r.routeFunc = func(datagram appletalk.Datagram, originating bool) error {
		mu.Lock()
		routedPackets = append(routedPackets, datagram)
		mu.Unlock()
		return nil
	}

	err := svc.Start(r)
	if err != nil {
		t.Fatalf("Failed to start service: %v", err)
	}
	defer svc.Stop()

	// Registered in a concrete zone; query uses wildcard zone="*".
	svc.RegisterName([]byte("GoServer"), []byte("AFPServer"), []byte("EtherTalk Network"), 252)

	p := newMockPort(1, 254, "localtalk", false)

	data := []byte{
		(nbpCtrlLkUp << 4) | 1, 7, 0, 1, 1, 254, 0,
		1, '=',
		9, 'A', 'F', 'P', 'S', 'e', 'r', 'v', 'e', 'r',
		1, '*',
	}

	d := appletalk.Datagram{DDPType: NBPDDPType, Data: data}
	svc.Inbound(d, p)

	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(routedPackets) != 1 {
		t.Fatalf("Expected 1 routed packet for wildcard zone lookup, got %d", len(routedPackets))
	}
	rply := routedPackets[0]
	if rply.DestinationNetwork != 1 || rply.DestinationNode != 1 || rply.DestinationSocket != 254 {
		t.Errorf("Routed packet has wrong destination: %+v", rply)
	}
}

func TestNameInformationService_Fwd(t *testing.T) {
	svc := NewNameInformationService()
	r := newMockRouter()

	p := newMockPort(30, 35, "mock-port3", false)

	var multicastCalled bool
	var mu sync.Mutex
	p.multicastFunc = func(zoneName []byte, datagram appletalk.Datagram) {
		mu.Lock()
		multicastCalled = true
		mu.Unlock()
	}

	r.routingGetByNetworkFunc = func(network uint16) (*service.RouteEntry, *bool) {
		return &service.RouteEntry{Distance: 0, Port: p}, nil
	}

	err := svc.Start(r)
	if err != nil {
		t.Fatalf("Failed to start service: %v", err)
	}
	defer svc.Stop()

	data := []byte{
		(nbpCtrlFwd << 4) | 1, 100, 0, 30, 35, 65, 0,
		4, 'O', 'b', 'j', '3',
		5, 'T', 'y', 'p', 'e', '3',
		5, 'Z', 'o', 'n', 'e', '3',
	}

	d := appletalk.Datagram{
		DDPType:            NBPDDPType,
		DestinationNetwork: 30, // Route matching
		Data:               data,
	}

	svc.Inbound(d, p)

	// Wait briefly for the goroutine to process
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if !multicastCalled {
		t.Fatalf("Expected multicast to be called")
	}
}

func TestNameInformationService_buildCommonPayload(t *testing.T) {
	svc := NewNameInformationService()

	data := []byte{
		0, 42, 0, 10, 15, 45, 0,
		4, 'O', 'b', 'j', '1',
		5, 'T', 'y', 'p', 'e', '1',
		5, 'Z', 'o', 'n', 'e', '1',
	}
	d := appletalk.Datagram{Data: data}
	zone := []byte("Zone1")
	replyNet := uint16(10)

	lkup, fwd := svc.buildCommonPayload(d, zone, replyNet)

	if len(lkup) == 0 || lkup[0] != (nbpCtrlLkUp<<4)|1 {
		t.Errorf("Invalid lkup payload")
	}
	if len(fwd) == 0 || fwd[0] != (nbpCtrlFwd<<4)|1 {
		t.Errorf("Invalid fwd payload")
	}

	// verify common parts
	expectedCommon := []byte{
		42, 0, 10, 15, 45, 0,
		4, 'O', 'b', 'j', '1',
		5, 'T', 'y', 'p', 'e', '1',
		5, 'Z', 'o', 'n', 'e', '1',
	}
	// Common starts at index 1
	if !bytes.Equal(lkup[1:], expectedCommon) {
		t.Errorf("lkup payload common part mismatch")
	}
	if !bytes.Equal(fwd[1:], expectedCommon) {
		t.Errorf("fwd payload common part mismatch")
	}
}

func TestNameInformationService_handlePacket_invalidDDP(t *testing.T) {
	svc := NewNameInformationService()
	r := newMockRouter()
	p := newMockPort(10, 15, "mock", false)

	// test invalid DDPType
	d := appletalk.Datagram{
		DDPType: 99,
		Data:    []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
	}
	// This shouldn't crash or process
	svc.handlePacket(d, p, r)

	// test length too short
	d = appletalk.Datagram{
		DDPType: NBPDDPType,
		Data:    []byte{0, 0, 0},
	}
	svc.handlePacket(d, p, r)
}
