package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	hrotti "github.com/alsm/hrotti/broker"
	. "github.com/alsm/hrotti/packets"
)

func main() {
	r := &hrotti.MemoryPersistence{}
	h := hrotti.NewHrotti(100, r, AuthHandler)
	hrotti.ERROR = log.New(os.Stdout, "ERROR: ", log.Ldate|log.Ltime)
	hrotti.INFO = log.New(os.Stdout, "INFO: ", log.Ldate|log.Ltime)
	hrotti.DEBUG = log.New(os.Stdout, "DEBUG: ", log.Ldate|log.Ltime)
	h.AddListener("test", hrotti.NewListenerConfig("tcp://0.0.0.0:1883"))

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	h.Stop()
}

func AuthHandler(cp *ConnectPacket) (error, string) {

	if cp.UsernameFlag {

		// check the username contains a valid token
		if cp.Username == "tok_f8fe5bb2f80b22c3f11c7f489895b2f7" {
			return nil, "123"
		}

	}

	return fmt.Errorf("auth failed"), ""
}
