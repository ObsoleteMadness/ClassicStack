// Package uri parses the ClassicStack file-client URI grammar into a Target.
//
// Grammar:
//
//	<protocol>://[[username][:password]@]<server>[,<transport>]/<volume>[/<path>]
//
// The <server> and <volume> fields are PROTOCOL-NATIVE — the URI parser leaves
// them as opaque strings for the scheme's factory to resolve. This is deliberate:
//
//   - AFP's <server> may be an NBP entity "name" or "name:zone", or a literal
//     "net.node"; the colon in "name:zone" is NOT a credentials separator (which
//     only appears before an '@') nor a port.
//   - EtherDFS' <server> is a hardware address as BARE hex "021a4d112233" or
//     DASH-separated "02-1a-4d-11-22-33" (never colon-separated, so a MAC carries
//     no ':' that could be confused with an AFP zone separator or a port).
//
// So the parser only knows the OUTER shape: scheme, optional credentials (the text
// before the LAST '@' preceding the authority's first '/'), the server, an optional
// ",transport" tail on the server, and the '/'-separated volume + path. Every field
// inside <server>/<volume> stays untouched.
//
// Ring: CLIENT (top-level client/ ring; stdlib only here).
package uri

import (
	"errors"
	"strings"
)

// Target is the parsed URI. Server and Volume are opaque, protocol-native strings
// resolved by the scheme's client factory; the parser does not interpret them.
type Target struct {
	Scheme    string // afp | smb | ncp | etherdfs
	User      string // empty when no credentials were given
	Pass      string // empty when no ':' appeared in the credentials
	HasCreds  bool   // an '@' was present (distinguishes ":@" empty creds from none)
	Server    string // protocol-native: NBP entity, NetBIOS/host name, SAP name, or MAC
	Transport string // the ",<transport>" tail, empty when absent
	Volume    string // first path element after the authority (share/volume/drive letter)
	Path      string // remaining '/'-separated path, empty when only a volume was given
}

var (
	// ErrNoScheme is returned when the input lacks a "<scheme>://" prefix.
	ErrNoScheme = errors.New("uri: missing \"<scheme>://\" prefix")
	// ErrNoServer is returned when the authority (server) part is empty.
	ErrNoServer = errors.New("uri: empty server")
	// ErrEmpty is returned when the input is blank.
	ErrEmpty = errors.New("uri: empty input")
)

// Parse splits raw into a Target per the grammar above. It performs no
// protocol-specific validation of Server/Volume — that is the factory's job.
func Parse(raw string) (Target, error) {
	if strings.TrimSpace(raw) == "" {
		return Target{}, ErrEmpty
	}

	// 1. Scheme: everything up to "://".
	scheme, rest, ok := strings.Cut(raw, "://")
	if !ok || scheme == "" {
		return Target{}, ErrNoScheme
	}
	t := Target{Scheme: strings.ToLower(scheme)}

	// 2. Split the authority (creds + server + transport) from the path at the
	//    FIRST '/'. Everything before the first '/' is the authority.
	authority, pathPart, hadSlash := strings.Cut(rest, "/")

	// 3. Credentials: the text before the LAST '@' in the authority. Using the
	//    last '@' lets a password contain no '@' while keeping the split robust
	//    if a server name somehow contained one (it should not). The server field
	//    never contains '@', so the authority has at most one meaningful '@'.
	serverAndTransport := authority
	if at := strings.LastIndex(authority, "@"); at >= 0 {
		creds := authority[:at]
		serverAndTransport = authority[at+1:]
		t.HasCreds = true
		if user, pass, hasColon := strings.Cut(creds, ":"); hasColon {
			t.User, t.Pass = user, pass
		} else {
			t.User = creds
		}
	}

	// 4. Transport: the ",<transport>" tail on the server. Split on the LAST comma
	//    so a server name may (in theory) contain one; in practice servers have no
	//    comma, so this is equivalent to the first.
	server := serverAndTransport
	if c := strings.LastIndex(serverAndTransport, ","); c >= 0 {
		server = serverAndTransport[:c]
		t.Transport = strings.ToLower(serverAndTransport[c+1:])
	}
	t.Server = server
	if t.Server == "" {
		return Target{}, ErrNoServer
	}

	// 5. Volume + path: the first '/'-separated element after the authority is the
	//    volume/share/drive; the remainder is the path.
	if hadSlash {
		vol, p, _ := strings.Cut(pathPart, "/")
		t.Volume = vol
		t.Path = strings.Trim(p, "/")
	}

	return t, nil
}

// String reassembles a Target into its canonical URI form. It is the inverse of
// Parse for a well-formed Target (round-trips), used for display/logging. A
// password is included verbatim — callers that log should redact separately.
func (t Target) String() string {
	var b strings.Builder
	b.WriteString(t.Scheme)
	b.WriteString("://")
	if t.HasCreds {
		b.WriteString(t.User)
		if t.Pass != "" {
			b.WriteByte(':')
			b.WriteString(t.Pass)
		}
		b.WriteByte('@')
	}
	b.WriteString(t.Server)
	if t.Transport != "" {
		b.WriteByte(',')
		b.WriteString(t.Transport)
	}
	if t.Volume != "" {
		b.WriteByte('/')
		b.WriteString(t.Volume)
		if t.Path != "" {
			b.WriteByte('/')
			b.WriteString(t.Path)
		}
	}
	return b.String()
}

// Redacted returns the URI with any password replaced by "***", for logs.
func (t Target) Redacted() string {
	if t.Pass != "" {
		t.Pass = "***"
	}
	return t.String()
}
