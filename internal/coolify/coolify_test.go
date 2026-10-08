package coolify

import (
	"strings"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"4.3.23", "4.3.23", 0},
		{"4.3.23", "4.3.9", 1},
		{"4.3.9", "4.3.23", -1},
		{"4.4-rc.1", "4.3.23", 1},
		{"4.4-rc.1", "4.4.0", -1},
		{"4.0.0-beta.474", "4.1.0", -1},
		{"4.0.0-beta.474", "4.0.0-beta.99", 1},
		{"v4.2.0", "4.2", 0},
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%s,%s)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestVersionFromImage(t *testing.T) {
	for img, want := range map[string]string{
		"coollabsio/coolify:4.3.23":                 "4.3.23",
		"ghcr.io/coollabsio/coolify:4.0.0-beta.474": "4.0.0-beta.474",
		"docker.io/coollabsio/coolify:4.4-rc.1":     "4.4-rc.1",
		"localhost:5000/coollabsio/coolify:latest":  "",
	} {
		if got := versionFromImage(img); got != want {
			t.Errorf("%s: %q want %q", img, got, want)
		}
	}
}

func TestEnvFile(t *testing.T) {
	e := ParseEnv([]byte("# comment\nAPP_KEY=base64:abc=\nDB_PASSWORD=\"x y\"\nEMPTY=\n"))
	if v, _ := e.Get("APP_KEY"); v != "base64:abc=" {
		t.Fatal(v)
	}
	if v, _ := e.Get("DB_PASSWORD"); v != "x y" {
		t.Fatal(v)
	}
	e.Set("APP_KEY", "base64:new=")
	e.Set("APP_PREVIOUS_KEYS", "base64:abc=")
	e.Delete("EMPTY")
	out := string(e.Bytes())
	if !strings.Contains(out, "# comment\nAPP_KEY=base64:new=\n") || !strings.HasSuffix(out, "APP_PREVIOUS_KEYS=base64:abc=\n") || strings.Contains(out, "EMPTY") {
		t.Fatalf("unexpected output:\n%s", out)
	}
}

func TestDomains(t *testing.T) {
	d := SplitDomains("https://a.com, http://b.com:3000/api ,")
	if len(d) != 2 || Host(d[1]) != "b.com:3000/api" {
		t.Fatalf("%v", d)
	}
	c := composeDomains(`{"web":{"domain":"https://x.com,https://y.com"},"api":"https://z.com"}`)
	if strings.Join(c, " ") != "https://z.com https://x.com https://y.com" {
		t.Fatalf("%v", c)
	}
}

// Regression: a Docker Compose application was probed on the fqdn Coolify
// generates at creation, which is never routed for compose applications.
func TestAppDomains(t *testing.T) {
	compose := `{"web":{"domain":"http://app.example.com:8080"}}`
	if d := appDomains("dockercompose", "http://uuid.1.2.3.4.sslip.io", compose); len(d) != 1 || d[0] != "http://app.example.com:8080" {
		t.Fatalf("compose app domains = %v", d)
	}
	if d := appDomains("dockerfile", "https://a.com,https://b.com", ""); len(d) != 2 {
		t.Fatalf("dockerfile app domains = %v", d)
	}
}

// In Coolify a domain's port is the container's port: displays leave it out.
func TestShowHost(t *testing.T) {
	for in, want := range map[string]string{
		"https://vemela.app:3000":      "vemela.app",
		"http://api.example.com:4000/": "api.example.com",
		"https://example.com/api":      "example.com/api",
		"https://example.com:8080/api": "example.com/api",
		"shop.cmlab.test":              "shop.cmlab.test",
	} {
		if got := ShowHost(in); got != want {
			t.Errorf("ShowHost(%q) = %q, want %q", in, got, want)
		}
	}
}
