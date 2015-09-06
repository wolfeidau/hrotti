package hrotti

import "github.com/alsm/hrotti/packets"

// DefaultTapHandler this tap handler is used if none is set
var DefaultTapHandler = NoopTapHandler

// TapHandler enables tapping of messages passing through the broker.
type TapHandler func(cp packets.ControlPacket)

// NoopTapHandler default tap handler
func NoopTapHandler(cp packets.ControlPacket) {}
