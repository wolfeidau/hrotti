package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"regexp"
	"syscall"

	hrotti "github.com/alsm/hrotti/broker"
	"github.com/alsm/hrotti/packets"
	"github.com/alsm/hrotti/store"
	"github.com/alsm/hrotti/timeseries"
)

// postgres://postgres@localhost:5432/devise-doorkeeper-cancan-api-example_development
func main() {
	userStore := store.NewPostgresStore("")
	tsStore := timeseries.NewInfluxDBTSStore()

	r := &hrotti.MemoryPersistence{}
	h := hrotti.NewHrotti(100, r, newAuthHandler(userStore))
	hrotti.DefaultTapHandler = newTapHandler(tsStore)
	hrotti.ERROR = log.New(os.Stdout, "ERROR: ", log.Ldate|log.Ltime)
	hrotti.INFO = log.New(os.Stdout, "INFO: ", log.Ldate|log.Ltime)
	hrotti.DEBUG = log.New(os.Stdout, "DEBUG: ", log.Ldate|log.Ltime)
	h.AddListener("test", hrotti.NewListenerConfig("tcp://0.0.0.0:18883"))

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	<-c
	h.Stop()
}

func newAuthHandler(store store.Store) hrotti.AuthHandler {
	return func(cp *packets.ConnectPacket) (hrotti.AuthContext, error) {

		if cp.UsernameFlag {

			// check the username contains a valid token
			uid, err := store.AuthUser(cp.Username)

			if err != nil {
				return nil, err
			}

			hrotti.DEBUG.Printf("Authenticated uid=%s", uid)

			return &localAuthContext{uid}, nil

		}

		return nil, fmt.Errorf("auth failed")
	}
}

type localAuthContext struct {
	UserID string
}

func (lac *localAuthContext) GetUserID() string {
	return lac.UserID
}

var checkPubRe = regexp.MustCompile(`^(?P<uid>[\w-]+)\/.*$`)

func (lac *localAuthContext) CheckPublish(pp *packets.PublishPacket) bool {

	hrotti.DEBUG.Printf("Check Publish topic=%s", pp.TopicName)

	md := checkTopicName(checkPubRe, pp.TopicName)

	if md["uid"] == lac.GetUserID() {
		return true
	}

	hrotti.INFO.Printf("Skipped Publish topic=%s", pp.TopicName)

	return false
}

func (lac *localAuthContext) CheckSubscription(topics []string, qoss []byte) ([]string, []byte) {

	for i, t := range topics {
		md := checkTopicName(checkPubRe, t)

		if md["uid"] != lac.GetUserID() {
			hrotti.INFO.Printf("Skipped Subscription topic=%s qos=%x", t, qoss[i])
			topics = append(topics[:i], topics[i+1:]...)
			qoss = append(qoss[:i], qoss[i+1:]...)
		}
	}
	return topics, qoss
}

func checkTopicName(re *regexp.Regexp, topic string) map[string]string {
	n1 := re.SubexpNames()
	r2 := re.FindAllStringSubmatch(topic, -1)

	md := map[string]string{}

	// no match returned
	if len(r2) > 0 {
		// iterate over the matches and translate them to a map
		for i, n := range r2[0] {
			md[n1[i]] = n
		}
		delete(md, "")
	}

	return md
}

var metricRe = regexp.MustCompile(`^(?P<uid>[\w-]+)\/(?P<serial_no>[\w-]+)\/(?P<device>[\w-]+)\/(?P<device_index>[\w-]+)\/(?P<type>[\w-]+)$`)

func newTapHandler(tss timeseries.TSStore) hrotti.TapHandler {
	return func(cp packets.ControlPacket) {
		hrotti.DEBUG.Printf("msg=%q", cp.UUID())

		switch cp.(type) {
		case *packets.PublishPacket:
			pp := cp.(*packets.PublishPacket)
			md := checkTopicName(metricRe, pp.TopicName)
			mm := md["type"]
			delete(md, "type")
			hrotti.DEBUG.Printf("md=%q", md)
			if md["uid"] != "" {
				tss.WritePoints(mm, md, string(pp.Payload))
			}
		}
	}

}
