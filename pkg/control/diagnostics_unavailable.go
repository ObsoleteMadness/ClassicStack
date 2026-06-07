package control

import (
	"context"
	"errors"
)

// ErrDiagUnavailable is returned by probes that are not compiled into this
// build (e.g. SMBBrowse without the smb tag) or not wired up.
var ErrDiagUnavailable = errors.New("control: diagnostic unavailable in this build")

// unavailableDiagnostics is the fallback used when no Diagnostics
// implementation is installed. Every probe reports ErrDiagUnavailable.
type unavailableDiagnostics struct{}

func (unavailableDiagnostics) ListZones(context.Context) ([]ZoneInfo, error) {
	return nil, ErrDiagUnavailable
}

func (unavailableDiagnostics) AEPEcho(context.Context, uint16, uint8) (EchoResult, error) {
	return EchoResult{}, ErrDiagUnavailable
}

func (unavailableDiagnostics) ZIPEnumerate(context.Context) ([]ZoneInfo, error) {
	return nil, ErrDiagUnavailable
}

func (unavailableDiagnostics) DDPEnumerate(context.Context) ([]NetworkInfo, error) {
	return nil, ErrDiagUnavailable
}

func (unavailableDiagnostics) RTMPTable(context.Context) ([]RTMPEntry, error) {
	return nil, ErrDiagUnavailable
}

func (unavailableDiagnostics) SMBBrowse(context.Context) ([]ServerInfo, error) {
	return nil, ErrDiagUnavailable
}

func (unavailableDiagnostics) MacIPLeases(context.Context) ([]LeaseInfo, error) {
	return nil, ErrDiagUnavailable
}
