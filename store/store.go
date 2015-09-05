package store

import "errors"

var ErrUserNotFound = errors.New("User not found in store")

type Store interface {
	Health() bool
	AuthUser(token string) (string, error)
	Close()
}
