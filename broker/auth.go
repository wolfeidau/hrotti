package hrotti

import "github.com/alsm/hrotti/packets"

// AuthContext holds the user context
type AuthContext interface {
	GetUserID() string
	CheckPublish(*packets.PublishPacket) bool
	CheckSubscription(topics []string, qoss []byte) ([]string, []byte)
}

type defaultAuthContext struct{}

func (defaultAuthContext) GetUserID() string {
	return ""
}

func (defaultAuthContext) CheckPublish(*packets.PublishPacket) bool {
	return true
}

func (defaultAuthContext) CheckSubscription(topics []string, qoss []byte) ([]string, []byte) {
	return topics, qoss
}

// AuthHandler called for each authentication
type AuthHandler func(cp *packets.ConnectPacket) (AuthContext, error)

// NoopAuthHandler default NOOP auth handler
var NoopAuthHandler = func(cp *packets.ConnectPacket) (AuthContext, error) {
	return &defaultAuthContext{}, nil
}
