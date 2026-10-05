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

func TestComposeDomainsSkipsServicesWithoutDomains(t *testing.T) {
	got := composeDomains(`{"web":{"domain":"https://lift.alavi.vip"},"database-backup":{"domain":null},"worker":{"domain":""},"legacy":"https://legacy.example.com"}`)
	if len(got) != 2 {
		t.Fatalf("got %d domain services: %#v", len(got), got)
	}
	if got["web"] != "https://lift.alavi.vip" {
		t.Fatalf("web domain = %q", got["web"])
	}
	if got["legacy"] != "https://legacy.example.com" {
		t.Fatalf("legacy domain = %q", got["legacy"])
	}
	if _, ok := got["database-backup"]; ok {
		t.Fatal("database-backup with a null domain must not be editable")
	}
	if _, ok := got["worker"]; ok {
		t.Fatal("worker with an empty domain must not be editable")
	}
}
