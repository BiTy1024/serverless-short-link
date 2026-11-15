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

func TestParse(t *testing.T) {
	tests := []struct {
		in, want, err string
	}{
		{"go.example.com", "go.example.com", ""},
		{"  Go.Example.COM\t", "go.example.com", ""},
		{"", "", "Domain is required"},
		{"   ", "", "Domain is required"},
		{"go.example.com:443", "", "Domain 'go.example.com:443' is not a valid hostname"},
		{"Localhost", "", "Domain 'localhost' is not a valid hostname"},
	}
	for _, tt := range tests {
		got, err := Parse(tt.in)
		gotErr := ""
		if err != nil {
			gotErr = err.Error()
		}
		if got != tt.want || gotErr != tt.err {
			t.Errorf("Parse(%q) = %q, %q; want %q, %q", tt.in, got, gotErr, tt.want, tt.err)
		}
	}
	if _, err := Parse(""); err != ErrRequired {
		t.Errorf("Parse(\"\") error = %v, want ErrRequired", err)
	}
}

func TestParseOptional(t *testing.T) {
	for _, in := range []string{"", "  "} {
		if got, err := ParseOptional(in); got != "" || err != nil {
			t.Errorf("ParseOptional(%q) = %q, %v", in, got, err)
		}
	}
	if got, err := ParseOptional(" A.Example "); got != "a.example" || err != nil {
		t.Errorf("ParseOptional = %q, %v", got, err)
	}
	if _, err := ParseOptional("nope"); err == nil {
		t.Error("ParseOptional(\"nope\") accepted an invalid domain")
	}
}
