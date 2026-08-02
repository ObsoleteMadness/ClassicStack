package port

// SerialFields is the serial binding a TashTalk port embeds and opens directly.
type SerialFields struct {
	Device string `toml:"device,omitempty" display:"Serial device" desc:"OS serial path (COM3, /dev/ttyUSB0)." example:"/dev/ttyUSB0" widget:"serial" capability:"serial"`
	Baud   int    `toml:"baud,omitempty" display:"Baud rate" desc:"Serial line speed. 0 = adapter default." default:"0" example:"57600" capability:"serial"`
}
