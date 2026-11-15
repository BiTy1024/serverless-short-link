package store

import (
	"testing"
	"time"
)

func TestTimestamp(t *testing.T) {
	berlin := time.FixedZone("CET", 3600)
	tests := map[time.Time]string{
		time.Date(2026, 3, 4, 5, 6, 7, 123456789, time.UTC): "2026-03-04T05:06:07.123456+00:00",
		time.Date(2026, 3, 4, 5, 6, 7, 1000, time.UTC):      "2026-03-04T05:06:07.000001+00:00",
		time.Date(2026, 3, 4, 5, 6, 7, 999, time.UTC):       "2026-03-04T05:06:07+00:00",
		time.Date(2026, 3, 4, 6, 6, 7, 0, berlin):           "2026-03-04T05:06:07+00:00",
	}
	for in, want := range tests {
		if got := Timestamp(in); got != want {
			t.Errorf("Timestamp(%v) = %q, want %q", in, got, want)
		}
	}
}
