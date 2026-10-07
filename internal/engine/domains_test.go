package engine

import (
	"strings"
	"testing"
)

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

func TestRiskyComposeDomainMoves(t *testing.T) {
	moved := []DomainField{
		{Resource: "app", Kind: "compose", Service: "web", Original: "https://old.example.com", Value: ""},
		{Resource: "app", Kind: "compose", Service: "database-backup", Original: "", Value: "https://new.example.com"},
	}
	if got := RiskyComposeDomainMoves(moved); len(got) != 1 || !strings.Contains(got[0], "web") || !strings.Contains(got[0], "database-backup") {
		t.Errorf("moved: %v", got)
	}
	for name, fields := range map[string][]DomainField{
		"changed in place": {{Resource: "app", Kind: "compose", Service: "web", Original: "https://a.example.com", Value: "https://b.example.com"}},
		"scheme only": {
			{Resource: "app", Kind: "compose", Service: "web", Original: "http://a.example.com", Value: "https://a.example.com"},
			{Resource: "app", Kind: "compose", Service: "api", Original: "", Value: "https://api.example.com"},
		},
		"added only": {{Resource: "app", Kind: "compose", Service: "api", Original: "", Value: "https://api.example.com"}},
		"other resource": {
			{Resource: "a", Kind: "compose", Service: "web", Original: "https://a.example.com", Value: ""},
			{Resource: "b", Kind: "compose", Service: "job", Original: "", Value: "https://b.example.com"},
		},
		"not compose": {
			{Resource: "app", Kind: "application", Original: "https://a.example.com", Value: ""},
			{Resource: "app", Kind: "application", Original: "", Value: "https://b.example.com"},
		},
	} {
		if got := RiskyComposeDomainMoves(fields); len(got) != 0 {
			t.Errorf("%s: %v", name, got)
		}
	}
}

func TestRiskyComposeDomainMovesOneOfSeveral(t *testing.T) {
	fields := []DomainField{
		{Resource: "app", Kind: "compose", Service: "web", Original: "https://a.example.com,https://b.example.com", Value: "https://a.example.com"},
		{Resource: "app", Kind: "compose", Service: "worker", Original: "", Value: "https://b.example.com"},
	}
	got := RiskyComposeDomainMoves(fields)
	if len(got) != 1 || !strings.Contains(got[0], "web (b.example.com)") || !strings.Contains(got[0], "worker (b.example.com)") {
		t.Errorf("one of several moved: %v", got)
	}
}
