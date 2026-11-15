// Package domain normalises and validates the host names short links are
// served on, and provides the per-domain redirect settings.
package domain

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

const (
	maxNameLength  = 253
	maxLabelLength = 63
)

// FromHost returns the domain name a request was sent to, given a Host header
// or API Gateway domain name: trimmed, lowercased, without port and without a
// trailing dot. An empty host gives "".
func FromHost(host string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	} else if strings.Count(host, ":") == 1 {
		// "example.com:" has an empty port, which SplitHostPort rejects.
		host, _, _ = strings.Cut(host, ":")
	}
	host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	return strings.TrimSuffix(host, ".")
}

// Valid reports whether name is a lowercase DNS hostname of at least two
// labels: letters, digits and inner hyphens, at most 63 characters per label
// and 253 in total. The last label must contain a letter, which rules out IP
// addresses.
func Valid(name string) bool {
	if name == "" || len(name) > maxNameLength {
		return false
	}
	labels := strings.Split(name, ".")
	if len(labels) < 2 {
		return false
	}
	for _, l := range labels {
		if !validLabel(l) {
			return false
		}
	}
	return strings.ContainsFunc(labels[len(labels)-1], func(r rune) bool { return r >= 'a' && r <= 'z' })
}

// ErrRequired is returned by Parse for an empty domain.
var ErrRequired = errors.New("Domain is required")

// Parse returns a domain given in an API request in its stored form: trimmed
// and lowercased. It fails with ErrRequired when the domain is empty and with
// a descriptive error when it is not a valid hostname. Error texts are meant
// for API responses.
func Parse(raw string) (string, error) {
	name := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case name == "":
		return "", ErrRequired
	case !Valid(name):
		return "", fmt.Errorf("Domain '%s' is not a valid hostname", name)
	}
	return name, nil
}

// ParseOptional is Parse for an optional domain: an empty or blank value
// gives "" and no error.
func ParseOptional(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", nil
	}
	return Parse(raw)
}

func validLabel(l string) bool {
	if l == "" || len(l) > maxLabelLength || l[0] == '-' || l[len(l)-1] == '-' {
		return false
	}
	for _, r := range l {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

// Static serves every domain with the same fallback URL.
type Static struct {
	FallbackURL string
}

// Fallback returns the configured URL for any domain; known is always true.
func (s Static) Fallback(string) (url string, known bool) {
	return s.FallbackURL, true
}
