package link

import (
	"sync"

	"github.com/ObsoleteMadness/ClassicStack/adapter/capture/pcapfile"
	"github.com/ObsoleteMadness/ClassicStack/core/link"
)

var clientCaptureSinks = struct {
	mu    sync.Mutex
	byKey map[string]*pcapfile.Sink
}{byKey: map[string]*pcapfile.Sink{}}

// maybeCapture wraps fl with a pcap tee when path is non-empty. Capture is
// best-effort: a bad path returns fl unchanged.
func maybeCapture(fl link.FrameLink, path string, lt pcapfile.LinkType, snaplen uint32) link.FrameLink {
	path = trimPath(path)
	if path == "" || fl == nil {
		return fl
	}
	if snaplen == 0 {
		snaplen = 65535
	}
	clientCaptureSinks.mu.Lock()
	sink, ok := clientCaptureSinks.byKey[path]
	if !ok {
		var err error
		sink, err = pcapfile.New(path, lt, snaplen)
		if err != nil {
			clientCaptureSinks.mu.Unlock()
			return fl
		}
		clientCaptureSinks.byKey[path] = sink
	}
	clientCaptureSinks.mu.Unlock()
	return link.Capture(fl, sink)
}

func trimPath(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\'' || s[0] == '"') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\'' || s[len(s)-1] == '"') {
		s = s[:len(s)-1]
	}
	return s
}
