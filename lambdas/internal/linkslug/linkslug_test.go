package linkslug

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	for _, s := range []string{"a", "0", "a-b", "promo2", strings.Repeat("a", 64)} {
		if msg := Validate(s); msg != "" {
			t.Errorf("Validate(%q) = %q, want accepted", s, msg)
		}
	}
	for _, s := range []string{"", "api", "admin", "health", "status", "Promo", "-a", "a-", "a--b", "a/b", "a#b", "über", strings.Repeat("a", 65)} {
		if Validate(s) == "" {
			t.Errorf("Validate(%q) accepted", s)
		}
	}
}
