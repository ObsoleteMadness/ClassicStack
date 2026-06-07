//go:build webui || all

package webui

import (
	"context"

	"github.com/ObsoleteMadness/ClassicStack/pkg/control"
	"github.com/ObsoleteMadness/ClassicStack/pkg/logbuf"
	"github.com/ObsoleteMadness/ClassicStack/pkg/serialport"
	"github.com/ObsoleteMadness/ClassicStack/pkg/status"
)

// ControlPlane is the subset of *control.Plane the web UI drives. Declaring
// it as an interface (satisfied by *control.Plane) keeps the HTTP adapter
// decoupled from the plane's construction and lets tests inject a fake.
type ControlPlane interface {
	Status() []status.Unit
	Config() (cfg control.ConfigModel, dirty bool)
	Stage(edit control.ConfigModel)
	Apply(ctx context.Context) error
	Save() (backupPath string, err error)
	Export() ([]byte, error)
	StartService(ctx context.Context, name string) error
	StopService(name string) error
	RestartService(ctx context.Context, name string) error
	RestartAll(ctx context.Context) error
	ListInterfaces() ([]control.InterfaceInfo, error)
	ListFSTypes() []string
	ListSerialPorts() ([]serialport.Info, error)
	ExtMap() (path string, data []byte, err error)
	SaveExtMap(data []byte) (backup string, err error)
	Subscribe() (<-chan control.Frame, func())
	LogHistory() []logbuf.Entry
	SubscribeLogs() (<-chan logbuf.Entry, func())
	Diagnostics() control.Diagnostics
}
