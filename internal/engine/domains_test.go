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

func TestRiskyComposeDomainMovesWarnsOnCrossServiceMove(t *testing.T) {
	fields := []DomainField{
		{Resource: "app", Kind: "compose", Service: "web", Original: "https://old.example.com", Value: ""},
		{Resource: "app", Kind: "compose", Service: "database-backup", Original: "", Value: "https://new.example.com"},
	}
	got := RiskyComposeDomainMoves(fields)
	if len(got) != 1 {
		t.Fatalf("warnings = %#v", got)
	}
	if !strings.Contains(got[0], "web") || !strings.Contains(got[0], "database-backup") {
		t.Fatalf("warning does not identify both services: %q", got[0])
	}
}

func TestRiskyComposeDomainMovesAllowsIntentionalNewDomainWithoutRemoval(t *testing.T) {
	fields := []DomainField{
		{Resource: "app", Kind: "compose", Service: "web", Original: "https://old.example.com", Value: "https://old.example.com"},
		{Resource: "app", Kind: "compose", Service: "api", Original: "", Value: "https://api.example.com"},
	}
	if got := RiskyComposeDomainMoves(fields); len(got) != 0 {
		t.Fatalf("unexpected warnings = %#v", got)
	}
}
