package lsn

import (
	"strings"
	"testing"
	"time"

	"github.com/jackc/pglogrepl"
)

func TestLag(t *testing.T) {
	tests := []struct {
		name    string
		current pglogrepl.LSN
		latest  pglogrepl.LSN
		want    uint64
	}{
		{"zero lag", pglogrepl.LSN(100), pglogrepl.LSN(100), 0},
		{"positive lag", pglogrepl.LSN(100), pglogrepl.LSN(200), 100},
		{"current ahead", pglogrepl.LSN(200), pglogrepl.LSN(100), 0},
		{"both zero", pglogrepl.LSN(0), pglogrepl.LSN(0), 0},
		{"large lag", pglogrepl.LSN(0), pglogrepl.LSN(1 << 30), 1 << 30},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Lag(tt.current, tt.latest)
			if got != tt.want {
				t.Errorf("Lag(%d, %d) = %d, want %d", tt.current, tt.latest, got, tt.want)
			}
		})
	}
}

func TestFormatLag(t *testing.T) {
	tests := []struct {
		name    string
		bytes   uint64
		latency time.Duration
		want    string
	}{
		{"zero", 0, 0, "0 B (latency: 0s)"},
		{"bytes", 512, 5 * time.Millisecond, "512 B (latency: 5ms)"},
		{"kilobytes", 1024, 10 * time.Millisecond, "1.00 KB (latency: 10ms)"},
		{"megabytes", 1 << 20, 150 * time.Millisecond, "1.00 MB (latency: 150ms)"},
		{"gigabytes", 1 << 30, 30 * time.Second, "1.00 GB (latency: 30s)"},
		{"fractional MB", 1572864, 0, "1.50 MB"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatLag(tt.bytes, tt.latency)
			if !strings.Contains(got, tt.want) && got != tt.want {
				t.Errorf("FormatLag(%d, %v) = %q, want to contain %q", tt.bytes, tt.latency, got, tt.want)
			}
		})
	}
}

func TestFormatLag_LatencyTruncation(t *testing.T) {
	got := FormatLag(0, 1234567*time.Nanosecond)
	if !strings.Contains(got, "latency: 1ms") {
		t.Errorf("FormatLag should truncate to milliseconds, got %q", got)
	}
}
