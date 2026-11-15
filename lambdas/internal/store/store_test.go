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

func TestLinkKeyAndNewClick(t *testing.T) {
	if got := LinkKey("go.example.com", "promo"); got != "go.example.com#promo" {
		t.Errorf("LinkKey = %q", got)
	}
	target := "https://example.com/promo"
	c := NewClick("go.example.com", "promo", "2026-03-04T05:06:07+00:00", &target)
	if c.LinkKey != "go.example.com#promo" || c.Domain != "go.example.com" || c.Slug != "promo" ||
		c.Timestamp != "2026-03-04T05:06:07+00:00" || c.TargetURL != &target {
		t.Errorf("NewClick = %+v", c)
	}
}

func TestTimeRangeContains(t *testing.T) {
	const ts = "2026-03-04T05:06:07+00:00"
	tests := []struct {
		r    TimeRange
		want bool
	}{
		{TimeRange{}, true},
		{TimeRange{Start: ts}, true},
		{TimeRange{End: ts}, true},
		{TimeRange{Start: "2026-03-05T00:00:00+00:00"}, false},
		{TimeRange{End: "2026-03-04T00:00:00+00:00"}, false},
		{TimeRange{Start: "2026-03-01T00:00:00+00:00", End: "2026-03-31T23:59:59+00:00"}, true},
	}
	for _, tt := range tests {
		if got := tt.r.Contains(ts); got != tt.want {
			t.Errorf("%+v.Contains = %v, want %v", tt.r, got, tt.want)
		}
	}
}
