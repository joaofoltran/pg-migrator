package lsn

import (
	"fmt"
	"time"

	"github.com/jackc/pglogrepl"
)

// Lag calculates the byte distance between two LSN positions.
func Lag(current, latest pglogrepl.LSN) uint64 {
	if latest <= current {
		return 0
	}
	return uint64(latest - current)
}

// FormatLag returns a human-friendly representation of replication lag.
func FormatLag(bytes uint64, latency time.Duration) string {
	var size string
	switch {
	case bytes >= 1<<30:
		size = fmt.Sprintf("%.2f GB", float64(bytes)/float64(1<<30))
	case bytes >= 1<<20:
		size = fmt.Sprintf("%.2f MB", float64(bytes)/float64(1<<20))
	case bytes >= 1<<10:
		size = fmt.Sprintf("%.2f KB", float64(bytes)/float64(1<<10))
	default:
		size = fmt.Sprintf("%d B", bytes)
	}
	return fmt.Sprintf("%s (latency: %s)", size, latency.Truncate(time.Millisecond))
}
