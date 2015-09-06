package timeseries

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"time"

	"github.com/influxdb/influxdb/client"
)

// InfluxDBTSStore influxdb time series store
type InfluxDBTSStore struct {
	con *client.Client
	db  string
}

// NewInfluxDBTSStore create a new influxdb time series store
func NewInfluxDBTSStore() *InfluxDBTSStore {

	u, err := url.Parse(fmt.Sprintf("http://%s:%s", os.Getenv("INFLUX_HOST"), os.Getenv("INFLUX_PORT")))

	if err != nil {
		log.Fatal(err)
	}

	conf := client.Config{
		URL:      *u,
		Username: os.Getenv("INFLUX_USER"),
		Password: os.Getenv("INFLUX_PWD"),
	}

	con, err := client.NewClient(conf)
	if err != nil {
		log.Fatal(err)
	}

	db := os.Getenv("INFLUX_DB")

	return &InfluxDBTSStore{con, db}
}

// Health check the status of the client connection to influxdb
func (its *InfluxDBTSStore) Health() bool {
	dur, ver, err := its.con.Ping()
	if err != nil {
		return false
	}

	log.Printf("influx %v, %s", dur, ver)

	return true
}

// WritePoints write a data point to the database
func (its *InfluxDBTSStore) WritePoints(measurement string, tags map[string]string, field string) (err error) {

	pts := []client.Point{
		{
			Measurement: measurement,
			Tags:        tags,
			Fields: map[string]interface{}{
				"value": field,
			},
			Time:      time.Now(),
			Precision: "s",
		},
	}
	bps := client.BatchPoints{
		Points:          pts,
		Database:        its.db,
		RetentionPolicy: "default",
	}

	log.Printf("influx %s %v", measurement, pts)

	_, err = its.con.Write(bps)

	return
}
