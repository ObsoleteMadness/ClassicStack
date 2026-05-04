package rawlink

// FrameConsumer receives frames matching its filter.
type FrameConsumer interface {
	OnFrame(frame []byte)
}

// FrameFilter defines which frames a consumer wants to receive.
type FrameFilter struct {
	EtherType uint16 // Ethernet II EtherType (or 0 if IsLLC is true)
	IsLLC     bool   // True if interested in 802.2 LLC frames
	DSAP      uint8  // LLC Destination Service Access Point
	SSAP      uint8  // LLC Source Service Access Point
}
