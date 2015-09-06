package timeseries

// TSStore store timeseries
type TSStore interface {
	Health() bool
	WritePoints(measurement string, tags map[string]string, field string) error
}
