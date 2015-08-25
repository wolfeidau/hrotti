package hrotti

import (
	. "github.com/alsm/hrotti/packets"
)

type AuthHandler func(cp *ConnectPacket) (error, string)

var NoopAuthHandler = func(cp *ConnectPacket) (error, string) {
	return nil, ""
}
