package engine

import "testing"

func TestNormalizeDomains(t *testing.T) {
	good := map[string]string{
		"":                            "",
		"a.com":                       "https://a.com",
		" a.com, http://b.com:3000/ ": "https://a.com,http://b.com:3000",
		"https://x.example.org/api":   "https://x.example.org/api",
	}
	for in, want := range good {
		if got, err := NormalizeDomains(in); err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"nodot", "a.com;rm", "https://"} {
		if _, err := NormalizeDomains(bad); err == nil {
			t.Errorf("%q: expected an error", bad)
		}
	}
}
