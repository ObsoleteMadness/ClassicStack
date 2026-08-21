package port

// SerialFields is the serial binding a TashTalk port embeds and opens directly.
type SerialFields struct {
	Device string `toml:"device,omitempty" display:"Serial device" desc:"OS serial path (COM3, /dev/ttyUSB0)." example:"/dev/ttyUSB0" widget:"serial" capability:"serial"`
	Baud   int    `toml:"baud,omitempty" display:"Baud rate" desc:"Serial line speed. 0 = adapter default." default:"0" example:"57600" capability:"serial"`
	// NoFlowControl disables RTS/CTS. Hardware flow control is ON by default because
	// TashTalk needs to throttle the 1 Mbit/s host link while it clocks a LocalTalk
	// frame out at 230.4 kbaud; without it the adapter's receive buffer overruns and
	// frames are lost. Only turn it off for a cable/adapter with no CTS line wired.
	// Keep the tag under 255 bytes: TinyGo rejects longer struct tags (tinygo-gate).
	NoFlowControl bool `toml:"no_flow_control,omitempty" display:"Disable RTS/CTS" desc:"Disable RTS/CTS flow control. Leave off: TashTalk needs it to avoid dropped frames. Only enable for an adapter with no CTS line wired." default:"false" example:"false" capability:"serial"`
}
