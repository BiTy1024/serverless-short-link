// Package store holds the records the Lambdas persist and their DynamoDB access.
package store

import (
	"errors"
	"time"
)

// Link is one row of the links table, keyed by (Domain, Slug). Slugs are
// unique per domain; the same slug can exist on several domains.
type Link struct {
	Domain    string `json:"domain" dynamodbav:"domain"`
	Slug      string `json:"slug" dynamodbav:"slug"`
	TargetURL string `json:"target_url" dynamodbav:"target_url"`
	CreatedAt string `json:"created_at" dynamodbav:"created_at"`
	UpdatedAt string `json:"updated_at" dynamodbav:"updated_at"`
}

// Click is one row of the redirect stats table, keyed by (LinkKey,
// Timestamp). Domain and Slug repeat the parts of LinkKey so the by-domain
// index and the stats responses can use them. TargetURL is nil when the row
// has no target_url.
type Click struct {
	LinkKey   string  `dynamodbav:"link_key"`
	Timestamp string  `dynamodbav:"timestamp"`
	Domain    string  `dynamodbav:"domain"`
	Slug      string  `dynamodbav:"slug"`
	TargetURL *string `dynamodbav:"target_url,omitempty"`
}

// LinkKey is the stats partition key of a link: "<domain>#<slug>". Neither a
// domain nor a slug can contain "#", so the key is unambiguous.
func LinkKey(domain, slug string) string {
	return domain + "#" + slug
}

// NewClick builds the click row for one redirect of (domain, slug).
func NewClick(domain, slug, timestamp string, targetURL *string) Click {
	return Click{
		LinkKey:   LinkKey(domain, slug),
		Timestamp: timestamp,
		Domain:    domain,
		Slug:      slug,
		TargetURL: targetURL,
	}
}

// TimeRange bounds a click query by timestamp. Both bounds are inclusive; an
// empty bound is open.
type TimeRange struct {
	Start string
	End   string
}

// Contains reports whether timestamp lies within r.
func (r TimeRange) Contains(timestamp string) bool {
	return (r.Start == "" || timestamp >= r.Start) && (r.End == "" || timestamp <= r.End)
}

var (
	// ErrLinkExists is returned when creating a link whose slug is taken on its domain.
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
