package phx

import "time"

const (
	// defaultConnectTimeout is the default handshake timeout
	defaultConnectTimeout = 10 * time.Second

	// defaultHeartbeat is the default time between heartbeats
	defaultHeartbeat = 5 * time.Second
)

func defaultReconnectAfterFunc(tries int) time.Duration {
	schedule := []time.Duration{10, 50, 100, 150, 200, 250, 500, 1000, 2000}
	if tries >= 0 && tries < len(schedule) {
		return schedule[tries] * time.Millisecond
	} else {
		return 5000 * time.Millisecond
	}
}
