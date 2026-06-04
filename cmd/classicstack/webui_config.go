package main

import (
	"fmt"
	"strings"
)

// WebUIConfigOptions is the user-facing configuration for the management
// web UI. It is populated from the [WebUI] TOML section or the
// -webui-* flags. The HTTP server itself lives behind //go:build webui
// (service/webui); this struct is always compiled so the disabled stub
// can still report a misconfiguration.
type WebUIConfigOptions struct {
	// Enabled turns the web UI listener on. When the binary was built
	// without -tags webui, setting this only produces a warning.
	Enabled bool `koanf:"enabled"`
	// Bind is the listen address for the web UI, e.g. "127.0.0.1:8080".
	Bind string `koanf:"bind"`
	// TLS enables HTTPS. When true and CertPEM/KeyPEM are blank a
	// self-signed certificate is generated at startup.
	TLS bool `koanf:"tls"`
	// CertPEM is the path to a PEM-encoded certificate. Blank selects
	// the self-signed certificate.
	CertPEM string `koanf:"cert_pem"`
	// KeyPEM is the path to a PEM-encoded private key. Blank selects the
	// self-signed certificate.
	KeyPEM string `koanf:"key_pem"`
}

// DefaultWebUIConfig returns the built-in defaults. The UI is disabled by
// default and, when enabled, binds to loopback with TLS on so a fresh
// install is not exposed to the network in plaintext.
func DefaultWebUIConfig() WebUIConfigOptions {
	return WebUIConfigOptions{
		Enabled: false,
		Bind:    "127.0.0.1:8080",
		TLS:     true,
	}
}

// Validate enforces logical rules the type system cannot express.
func (c *WebUIConfigOptions) Validate() error {
	if !c.Enabled {
		return nil
	}
	if strings.TrimSpace(c.Bind) == "" {
		return fmt.Errorf("WebUI.bind must not be empty when WebUI is enabled")
	}
	// Cert and key are an all-or-nothing pair: supplying one without the
	// other is a configuration mistake rather than a self-signed fallback.
	hasCert := strings.TrimSpace(c.CertPEM) != ""
	hasKey := strings.TrimSpace(c.KeyPEM) != ""
	if hasCert != hasKey {
		return fmt.Errorf("WebUI.cert_pem and WebUI.key_pem must be set together (or both left blank for a self-signed certificate)")
	}
	return nil
}
