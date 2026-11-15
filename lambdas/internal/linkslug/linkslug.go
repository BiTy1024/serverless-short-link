// Package linkslug holds the rules a link's slug must follow. The links and
// stats handlers apply them to every slug a request names.
package linkslug

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// MaxLength is the longest accepted slug, in characters.
const MaxLength = 64

var (
	pattern  = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)
	reserved = []string{"api", "admin", "health", "status"}
)

// Validate returns why slug is not acceptable, or "" when it is. Slugs are
// not lowercased: an uppercase letter is an error.
func Validate(slug string) string {
	switch {
	case slug == "":
		return "Slug is required"
	case slices.Contains(reserved, slug):
		return fmt.Sprintf("Slug '%s' is reserved and cannot be used as a short link", slug)
	case utf8.RuneCountInString(slug) > MaxLength:
		return fmt.Sprintf("Slug must be at most %d characters", MaxLength)
	case strings.Contains(slug, "--"):
		return "Slug must not contain consecutive hyphens"
	case !pattern.MatchString(slug):
		return "Slug must contain only lowercase letters, numbers, and hyphens. Must start and end with a letter or number."
	}
	return ""
}
