package store

import "errors"

// ErrUserNotFound returned when the user is NOT found in the datastore
var ErrUserNotFound = errors.New("User not found in store")

// Store is a datastore used in the broker
type Store interface {
	Health() bool
	AuthUser(token string) (string, error)
	Close()
}
