package finder

import (
	"fmt"
	"strings"

	"github.com/ObsoleteMadness/ClassicStack/client"
	clientlink "github.com/ObsoleteMadness/ClassicStack/client/link"
	"github.com/ObsoleteMadness/ClassicStack/client/uri"
	"github.com/ObsoleteMadness/ClassicStack/core/config"
	"github.com/ObsoleteMadness/ClassicStack/core/csnet"
	"github.com/ObsoleteMadness/ClassicStack/core/log"
)

// SetLinkConfig overrides the outbound client [[interface]] getter New bound from
// src.Model(). Tests and a later per-protocol panel use this; production callers
// leave New's Model binding in place.
func (s *Service) SetLinkConfig(fn func() config.InterfaceSection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.defaultLink = fn
}

func (s *Service) configuredSpec() clientlink.Spec {
	return SpecFromInterface(s.configuredInterface())
}

// SpecFromInterface maps a config [[interface]] entry to the client/link Spec the
// outbound Finder client opens. nic/bridge/wifi with backend pcap (the default) become
// pcap:<PcapDevice> — Device when set (en0 / Npcap GUID), else Name. backend tap is
// KindTap. serial is TashTalk. multicast is LToUDP. tun has no client transport and
// yields a zero Spec (scheme defaults apply).
func SpecFromInterface(iface config.InterfaceSection) clientlink.Spec {
	if iface.Name == "" && iface.Device == "" && iface.Addr == "" {
		return clientlink.Spec{}
	}
	switch iface.EffectiveKind() {
	case config.IfaceKindSerial:
		return clientlink.Spec{
			Kind: clientlink.KindTashTalk,
			Name: iface.Device,
			Baud: uint(iface.Baud),
		}
	case config.IfaceKindMulticast:
		return clientlink.Spec{Kind: clientlink.KindLToUDP, Name: iface.Addr}
	default:
		// nic / bridge / wifi (and an empty kind, which EffectiveKind treats as nic).
		switch iface.EffectiveBackend() {
		case config.IfaceBackendTap:
			return clientlink.Spec{Kind: clientlink.KindTap, Name: iface.PcapDevice()}
		case config.IfaceBackendTun:
			return clientlink.Spec{}
		default:
			return clientlink.Spec{Kind: clientlink.KindPcap, Name: iface.PcapDevice()}
		}
	}
}

// resolveLink picks the client/link Spec for a scheme. Explicit request fields and a
// URI-embedded transport win; otherwise the configured [[interface]] is used when the
// scheme can ride it; otherwise the scheme's registered default (and, for raw Ethernet,
// the OS default-route NIC as a last resort).
func (s *Service) resolveLink(scheme, ifaceType, iface, transport string, target uri.Target) (clientlink.Spec, error) {
	transports := client.TransportsFor(scheme)
	def := s.configuredSpec()

	kind := strings.TrimSpace(ifaceType)
	if kind == "" && target.Transport != "" && isLinkKind(target.Transport) {
		kind = target.Transport
	}
	if kind == "" && def.Kind != "" && schemeAcceptsLink(scheme, def.Kind) {
		kind = def.Kind
	}
	if kind == "" {
		kind = transports.Default
	}
	if kind == "" {
		return clientlink.Spec{}, fmt.Errorf("finder: ifaceType required for %s", scheme)
	}
	if !schemeAcceptsLink(scheme, kind) {
		return clientlink.Spec{}, fmt.Errorf("finder: ifaceType %q is not valid for %s", kind, scheme)
	}

	name := strings.TrimSpace(iface)
	if name == "" && kind == clientlink.KindTCP {
		name = strings.TrimSpace(target.Server)
	}
	if name == "" && kindsCompatible(def.Kind, kind) {
		name = def.Name
	}
	if name == "" && clientlink.IsRawEtherKind(kind) {
		if d, err := clientlink.DefaultInterface(); err == nil {
			name = d.Name
		}
	}

	baud := uint(0)
	if kind == clientlink.KindTashTalk && kindsCompatible(def.Kind, kind) {
		baud = def.Baud
	}

	carrier := strings.TrimSpace(transport)
	if carrier == "" && target.Transport != "" && !isLinkKind(target.Transport) {
		carrier = target.Transport
	}

	spec := clientlink.Spec{Kind: kind, Name: name, Baud: baud, Carrier: carrier}
	s.log.Log(log.Debug, "finder client link",
		log.Str("scheme", scheme),
		log.Str("ifacetype", spec.Kind),
		log.Str("iface", spec.Name),
		log.Str("carrier", spec.Carrier))
	return spec, nil
}

func (s *Service) openerFor(scheme, ifaceType, iface, transport string, target uri.Target) (*clientlink.Opener, error) {
	spec, err := s.resolveLink(scheme, ifaceType, iface, transport, target)
	if err != nil {
		return nil, err
	}
	opener := clientlink.NewOpener(spec)
	if path := strings.TrimSpace(s.clientConfig().Capture); path != "" {
		opener.CapturePath = path
		if n := s.clientConfig().CaptureSnaplen; n > 0 {
			opener.CaptureSnaplen = uint32(n)
		}
	}
	// MAC precedence: an explicit [Client] mac (lets the outbound client present a
	// distinct station from the server's own NIC-bound ports on the same interface,
	// avoiding a collision when both run on one bridge); else the bound
	// [[interface]]'s hw_address; else NewOpener's own "be the host" default (the
	// host NIC's MAC), already set on opener.
	if mac := strings.TrimSpace(s.clientConfig().MAC); mac != "" {
		if parsed, err := parseMAC6(mac); err == nil {
			opener.MAC = parsed
		}
	} else if hw := s.configuredInterface().HWAddress; hw != "" {
		if mac, err := parseMAC6(hw); err == nil {
			opener.MAC = mac
		}
	}
	// The outbound client runs as part of the ClassicStack server, so its NetBIOS
	// session carriers (SMB-over-NBIPX, SMB-over-NBF) present the configured client
	// identity (or the server's own Identity.Hostname when unset) rather than a
	// throwaway MAC-derived name — one name for the whole box by default, matching
	// how a real Windows/DOS station's redirector and file-sharing server share one
	// NetBIOS name. Harmless when unset: the carrier keeps its MAC-derived default.
	if name := s.clientName(); name != "" {
		opener.CallingName = name
	}
	// When the local Browser Service already lists the target as a known server
	// (learned passively from its own Host/LocalMaster Announcements — see
	// core/service/browser), the NB-IPX carrier skips its own redundant Find-name
	// locate and goes straight to SESSION_INITIALIZE with the full establish
	// budget. Nil/unknown falls back to today's independent locate.
	if srv := strings.TrimSpace(target.Server); srv != "" && s.browserKnows(srv) {
		opener.KnownServer = true
	}
	s.log.Log(log.Debug, "finder client opener",
		log.Str("scheme", scheme),
		log.Str("ifacetype", opener.Spec.Kind),
		log.Str("iface", opener.Spec.Name),
		log.Str("mac", formatMAC6(opener.MAC)),
		log.Str("callingName", opener.CallingName),
		log.Bool("knownServer", opener.KnownServer))
	return opener, nil
}

// browserKnows reports whether the local Browser Service's browse list already
// contains name (case-insensitively) — i.e. it has already observed that server
// announce itself, independent of this connect attempt.
func (s *Service) browserKnows(name string) bool {
	b := s.browser()
	if b == nil {
		return false
	}
	for _, n := range b.BrowseList() {
		if strings.EqualFold(n, name) {
			return true
		}
	}
	return false
}

func (s *Service) configuredInterface() config.InterfaceSection {
	s.mu.Lock()
	fn := s.defaultLink
	s.mu.Unlock()
	if fn == nil {
		return config.InterfaceSection{}
	}
	return fn()
}

func parseMAC6(s string) ([6]byte, error) {
	mac, err := csnet.ParseMAC(s)
	if err != nil {
		return [6]byte{}, fmt.Errorf("finder: invalid hw_address %q", s)
	}
	return mac, nil
}

func formatMAC6(mac [6]byte) string {
	return fmt.Sprintf("%02X:%02X:%02X:%02X:%02X:%02X", mac[0], mac[1], mac[2], mac[3], mac[4], mac[5])
}

// schemeAcceptsLink reports whether scheme can ride kind. tap is accepted wherever the
// scheme lists pcap: they are interchangeable raw-Ethernet FrameLink backends.
func schemeAcceptsLink(scheme, kind string) bool {
	t := client.TransportsFor(scheme)
	if t.Accepts(kind) {
		return true
	}
	return clientlink.IsRawEtherKind(kind) && t.Accepts(clientlink.KindPcap)
}

func kindsCompatible(configured, selected string) bool {
	if configured == "" || selected == "" {
		return false
	}
	if strings.EqualFold(configured, selected) {
		return true
	}
	return clientlink.IsRawEtherKind(configured) && clientlink.IsRawEtherKind(selected)
}

func isLinkKind(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case clientlink.KindLToUDP, clientlink.KindTashTalk, clientlink.KindPcap,
		clientlink.KindTap, clientlink.KindTCP, clientlink.KindInmem:
		return true
	}
	return false
}
