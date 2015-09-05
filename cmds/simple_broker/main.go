package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	hrotti "github.com/alsm/hrotti/broker"
	. "github.com/alsm/hrotti/packets"
	"github.com/alsm/hrotti/store"
)

// postgres://postgres@localhost:5432/devise-doorkeeper-cancan-api-example_development
func main() {
	userStore := store.NewPostgresStore("")

	r := &hrotti.MemoryPersistence{}
	h := hrotti.NewHrotti(100, r, newHandler(userStore))
	hrotti.ERROR = log.New(os.Stdout, "ERROR: ", log.Ldate|log.Ltime)
	hrotti.INFO = log.New(os.Stdout, "INFO: ", log.Ldate|log.Ltime)
	hrotti.DEBUG = log.New(os.Stdout, "DEBUG: ", log.Ldate|log.Ltime)
	h.AddListener("test", hrotti.NewListenerConfig("tcp://0.0.0.0:18883"))

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	h.Stop()
}

func newHandler(store store.Store) hrotti.AuthHandler {
	return func(cp *ConnectPacket) (string, error) {

		if cp.UsernameFlag {

			// check the username contains a valid token
			return store.AuthUser(cp.Username)

		}

		return "", fmt.Errorf("auth failed")
	}
}
