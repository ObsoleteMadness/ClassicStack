//go:build webui || all

// Package webui is a thin HTTP/SSE adapter over the transport-agnostic
// management API in pkg/control. It owns no management logic of its own:
// every handler delegates to the ControlPlane it is given, so a future
// text/telnet UI can drive the same operations without HTTP.
package webui

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/ObsoleteMadness/ClassicStack/netlog"
)

// Options configures the web UI server.
type Options struct {
	// Bind is the listen address, e.g. "127.0.0.1:8080".
	Bind string
	// TLS enables HTTPS. When CertPEM/KeyPEM are blank a self-signed
	// certificate is generated for the lifetime of the process.
	TLS     bool
	CertPEM string
	KeyPEM  string
	// Plane is the management API the server adapts. May be nil in
	// degraded/diagnostic configurations; handlers guard for it.
	Plane ControlPlane
}

// Server is the web UI HTTP(S) listener.
type Server struct {
	opts Options
	mux  *http.ServeMux

	mu     sync.Mutex
	httpd  *http.Server
	ln     net.Listener
	closed bool
}

// NewServer constructs the server and wires its routes. It does not bind a
// socket until Start.
func NewServer(opts Options) (*Server, error) {
	if opts.Bind == "" {
		return nil, errors.New("webui: bind address is required")
	}
	s := &Server{opts: opts, mux: http.NewServeMux()}
	s.routes()
	return s, nil
}

// Start binds the listener and serves in a background goroutine. It
// returns once the socket is open (or immediately on bind failure).
func (s *Server) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("webui: server already stopped")
	}

	ln, err := net.Listen("tcp", s.opts.Bind)
	if err != nil {
		return err
	}

	httpd := &http.Server{
		Handler:           s.mux,
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	if s.opts.TLS {
		tlsCfg, err := s.tlsConfig()
		if err != nil {
			_ = ln.Close()
			return err
		}
		httpd.TLSConfig = tlsCfg
		ln = tls.NewListener(ln, tlsCfg)
	}

	s.httpd = httpd
	s.ln = ln

	go func() {
		if err := httpd.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			netlog.Warn("[WebUI] serve error: %v", err)
		}
	}()

	scheme := "http"
	if s.opts.TLS {
		scheme = "https"
	}
	netlog.Info("[WebUI] listening on %s://%s", scheme, s.opts.Bind)
	return nil
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() error {
	s.mu.Lock()
	httpd := s.httpd
	s.closed = true
	s.mu.Unlock()
	if httpd == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return httpd.Shutdown(ctx)
}

// tlsConfig loads the configured cert/key, or generates a self-signed
// certificate when both are blank.
func (s *Server) tlsConfig() (*tls.Config, error) {
	if s.opts.CertPEM != "" && s.opts.KeyPEM != "" {
		cert, err := tls.LoadX509KeyPair(s.opts.CertPEM, s.opts.KeyPEM)
		if err != nil {
			return nil, err
		}
		return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}, nil
	}
	cert, err := selfSignedCert(s.opts.Bind)
	if err != nil {
		return nil, err
	}
	netlog.Info("[WebUI] using generated self-signed certificate")
	return &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}, nil
}
