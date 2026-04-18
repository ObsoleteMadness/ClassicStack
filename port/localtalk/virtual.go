package localtalk

import "sync"

type VirtualNetwork struct {
	mu      sync.RWMutex
	plugged []func([]byte)
}

func (n *VirtualNetwork) Plug(f func([]byte)) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.plugged = append(n.plugged, f)
}

func (n *VirtualNetwork) Unplug(f func([]byte)) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for i, x := range n.plugged {
		if &x == &f {
			n.plugged = append(n.plugged[:i], n.plugged[i+1:]...)
			return
		}
	}
}

func (n *VirtualNetwork) SendFrame(frame []byte, sender func([]byte)) {
	n.mu.RLock()
	defer n.mu.RUnlock()
	for _, f := range n.plugged {
		if &f == &sender {
			continue
		}
		f(frame)
	}
}
