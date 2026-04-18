package main

import (
	"fmt"
	"io"
	"log"
	"os"

	"github.com/pgodw/omnitalk/go/service"
)

// PacketDumper is a generic sink used by services to emit parsed packet logs.
type PacketDumper struct {
	logger *log.Logger
}

var _ service.PacketDumper = (*PacketDumper)(nil)

// newPacketDumper creates a PacketDumper. If outputPath is non-empty the
// parsed packet logs are also written to that file (in addition to stdout).
// The caller is responsible for invoking the returned cleanup func.
func newPacketDumper(outputPath string) (*PacketDumper, func(), error) {
	writers := []io.Writer{os.Stdout}
	cleanup := func() {}

	if outputPath != "" {
		f, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			return nil, nil, fmt.Errorf("open parse-output %q: %w", outputPath, err)
		}
		writers = append(writers, f)
		cleanup = func() { _ = f.Close() }
		log.Printf("[DUMP] writing parsed packets to %q", outputPath)
	}

	logger := log.New(io.MultiWriter(writers...), "", log.LstdFlags|log.Lmicroseconds)
	return &PacketDumper{logger: logger}, cleanup, nil
}

func (pd *PacketDumper) LogPacket(message string) {
	pd.logger.Printf("[PACKET] %s", message)
}
