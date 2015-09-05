package hrotti

import (
	. "github.com/alsm/hrotti/packets"
)

type AuthHandler func(cp *ConnectPacket) (string, error)

var NoopAuthHandler = func(cp *ConnectPacket) (string, error) {
	return "", nil
}
