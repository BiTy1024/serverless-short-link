// Package store holds the records the Lambdas persist and their DynamoDB access.
package store

import (
	"errors"
	"time"
)

// Link is one row of the links table.
type Link struct {
	ShortPath string `json:"short_path" dynamodbav:"short_path"`
	TargetURL string `json:"target_url" dynamodbav:"target_url"`
	CreatedAt string `json:"created_at" dynamodbav:"created_at"`
	UpdatedAt string `json:"updated_at" dynamodbav:"updated_at"`
}

// Click is one row of the redirect stats table. RedirectPath carries the
// leading slash ("/abc"). TargetURL is nil when the row has no target_url.
type Click struct {
	RedirectPath string  `dynamodbav:"redirect_path"`
	Timestamp    string  `dynamodbav:"timestamp"`
	TargetURL    *string `dynamodbav:"target_url,omitempty"`
}

// TimeRange bounds a click query by timestamp. Both bounds are inclusive; an
// empty bound is open.
type TimeRange struct {
	Start string
	End   string
}

var (
	// ErrLinkExists is returned when creating a link whose short path is taken.
	ErrLinkExists = errors.New("link already exists")
	// ErrLinkNotFound is returned when updating or deleting a missing link.
	ErrLinkNotFound = errors.New("link not found")
)

// Timestamp formats t the way timestamps are stored: UTC ISO 8601 with
// microseconds and a "+00:00" offset. The fraction is left out when it is
// zero, so stored values keep sorting as strings.
func Timestamp(t time.Time) string {
	t = t.UTC()
	if t.Nanosecond()/1000 == 0 {
		return t.Format("2006-01-02T15:04:05+00:00")
	}
	return t.Format("2006-01-02T15:04:05.000000+00:00")
}
