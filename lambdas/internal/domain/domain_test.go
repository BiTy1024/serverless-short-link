package domain

import (
	"strings"
	"testing"
)

func TestFromHost(t *testing.T) {
	tests := map[string]string{
		"go.example.com":         "go.example.com",
		"Go.Example.COM":         "go.example.com",
		"go.example.com:443":     "go.example.com",
		"GO.EXAMPLE.COM:8443":    "go.example.com",
		"go.example.com:":        "go.example.com",
		"go.example.com.":        "go.example.com",
		"go.example.com.:443":    "go.example.com",
		"  go.example.com  ":     "go.example.com",
		"":                       "",
		"   ":                    "",
		"[::1]:443":              "::1",
		"[2001:DB8::1]":          "2001:db8::1",
		"127.0.0.1:8080":         "127.0.0.1",
		"localhost":              "localhost",
		"xn--bcher-kva.example":  "xn--bcher-kva.example",
		"a.b.c.d.example.co.uk:": "a.b.c.d.example.co.uk",
	}
	for in, want := range tests {
		if got := FromHost(in); got != want {
			t.Errorf("FromHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValid(t *testing.T) {
	valid := []string{
		"example.com",
		"go.example.com",
		"a.co",
		"xn--bcher-kva.example",
		"my-short.link",
		"123.example",
		strings.Repeat("a", 63) + ".com",
	}
	for _, name := range valid {
		if !Valid(name) {
			t.Errorf("Valid(%q) = false", name)
		}
	}

	invalid := []string{
		"",
		"localhost",
		"Example.com",
		"example.com.",
		".example.com",
		"example..com",
		"-a.example.com",
		"a-.example.com",
		"example.com:443",
		"https://example.com",
		"example.com/path",
		"ex ample.com",
		"exämple.com",
		"under_score.com",
		"127.0.0.1",
		"example.123",
		strings.Repeat("a", 64) + ".com",
		strings.Repeat("a.", 126) + "com",
	}
	for _, name := range invalid {
		if Valid(name) {
			t.Errorf("Valid(%q) = true", name)
		}
	}
}

func TestStatic(t *testing.T) {
	s := Static{FallbackURL: "https://example.com/home"}
	for _, d := range []string{"a.example", "b.example", ""} {
		url, known := s.Fallback(d)
		if url != "https://example.com/home" || !known {
			t.Errorf("Fallback(%q) = %q, %v", d, url, known)
		}
	}
}
