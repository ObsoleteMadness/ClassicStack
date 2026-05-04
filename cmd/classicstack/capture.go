package main

import (
	"log"

	"github.com/ObsoleteMadness/ClassicStack/capture"
	"github.com/ObsoleteMadness/ClassicStack/netlog"
	"github.com/ObsoleteMadness/ClassicStack/port"
	"github.com/ObsoleteMadness/ClassicStack/port/ethertalk"
	"github.com/ObsoleteMadness/ClassicStack/port/localtalk"
)

// attachCaptureSinks opens any enabled capture files in cfg, fans them
// out to matching ports, and returns the open sinks for cleanup.
//
// LocalTalk capture covers every concrete port that embeds
// *localtalk.Port (LToUDP, TashTalk). EtherTalk capture targets the
// pcap-backed EtherTalk port.
func attachCaptureSinks(ports []port.Port, cfg capture.Config) []*capture.PcapSink {
	var sinks []*capture.PcapSink

	if cfg.LocalTalkEnabled() {
		sink, err := capture.NewPcapSink(cfg.LocalTalk, capture.LinkTypeLocalTalk, cfg.Snaplen)
		if err != nil {
			log.Fatalf("capture: open localtalk pcap: %v", err)
		}
		count := 0
		for _, p := range ports {
			if lt := localtalkBase(p); lt != nil {
				lt.SetCaptureSink(sink)
				count++
			}
		}
		netlog.Info("[CAPTURE] LocalTalk frames -> %s (%d ports)", cfg.LocalTalk, count)
		sinks = append(sinks, sink)
	}

	if cfg.EtherTalkEnabled() {
		sink, err := capture.NewPcapSink(cfg.EtherTalk, capture.LinkTypeEthernet, cfg.Snaplen)
		if err != nil {
			log.Fatalf("capture: open ethertalk pcap: %v", err)
		}
		count := 0
		for _, p := range ports {
			if ep, ok := p.(*ethertalk.PcapPort); ok {
				ep.SetCaptureSink(sink)
				count++
			}
		}
		netlog.Info("[CAPTURE] EtherTalk frames -> %s (%d ports)", cfg.EtherTalk, count)
		sinks = append(sinks, sink)
	}

	return sinks
}

func localtalkBase(p port.Port) *localtalk.Port {
	switch v := p.(type) {
	case *localtalk.LtoudpPort:
		return v.Port
	case *localtalk.TashTalkPort:
		return v.Port
	}
	return nil
}
