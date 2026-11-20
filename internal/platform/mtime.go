package platform

import "time"

// MtimeMs is the modification time as Node's stat.mtimeMs: fractional epoch milliseconds.
func MtimeMs(t time.Time) float64 {
	return float64(t.Unix())*1e3 + float64(t.Nanosecond())/1e6
}
