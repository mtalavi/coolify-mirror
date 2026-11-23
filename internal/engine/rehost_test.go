package engine

import (
	"strings"
	"testing"
)

func TestRehost(t *testing.T) {
	pairs := map[string]string{
		"vemela.app":     "ir.vemela.app",
		"api.vemela.app": "irapi.vemela.app",
		"s3.vemela.app":  "irs3.vemela.app",
	}
	cases := map[string]string{
		"APP_URL=https://vemela.app":                               "APP_URL=https://ir.vemela.app",
		"COOLIFY_URL=https://api.vemela.app":                       "COOLIFY_URL=https://irapi.vemela.app",
		"S3_PUBLIC_ENDPOINT=https://s3.vemela.app":                 "S3_PUBLIC_ENDPOINT=https://irs3.vemela.app",
		"PLATFORM_ROOT_DOMAIN=vemela.app":                          "PLATFORM_ROOT_DOMAIN=ir.vemela.app",
		"COOKIE_DOMAIN=.vemela.app":                                "COOKIE_DOMAIN=.ir.vemela.app",
		"CORS=https://vemela.app,https://*.vemela.app":             "CORS=https://ir.vemela.app,https://*.ir.vemela.app",
		"EMAIL_FROM=Vemela <no-reply@vemela.app>":                  "EMAIL_FROM=Vemela <no-reply@vemela.app>",
		"rule=Host(`vemela.app`) && PathPrefix(`/`)":               "rule=Host(`ir.vemela.app`) && PathPrefix(`/`)",
		"'caddy_0=https://api.vemela.app'":                         "'caddy_0=https://irapi.vemela.app'",
		"OTHER=https://shop.vemela.app https://vemela.application": "OTHER=https://shop.vemela.app https://vemela.application",
		"X=https://VEMELA.app:3000/x":                              "X=https://ir.vemela.app:3000/x",
		"a.b.vemela.app notvemela.app vemela.app-x":                "a.b.vemela.app notvemela.app vemela.app-x",
	}
	for in, want := range cases {
		got := Rehost(in, pairs)
		if got != want {
			t.Errorf("Rehost(%q) = %q, want %q", in, got, want)
		}
		if again := Rehost(got, pairs); again != got {
			t.Errorf("Rehost is not idempotent on %q: %q", got, again)
		}
	}
}

func TestInterpolate(t *testing.T) {
	env := parseDotEnv("SOURCE_COMMIT=\"abc\"\nEMPTY=\nexport X='y'\n# c\n")
	cases := map[string]string{
		"img:${SOURCE_COMMIT:?SOURCE_COMMIT is required}": "img:abc",
		"img:${SOURCE_COMMIT}":                            "img:abc",
		"img:$SOURCE_COMMIT":                              "img:abc",
		"${MISSING:-def}":                                 "def",
		"${EMPTY:-def}":                                   "def",
		"${EMPTY-def}":                                    "",
		"${X}":                                            "y",
		"a$$b":                                            "a$b",
		"plain":                                           "plain",
	}
	for in, want := range cases {
		if got := interpolate(in, env); got != want {
			t.Errorf("interpolate(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLastDeployError(t *testing.T) {
	logs := `[{"type":"stdout","output":"Pulling & building required images."},
{"type":"stderr","output":"Traceback (most recent call last):","hidden":true},
{"type":"stderr","output":"========================================"},
{"type":"stderr","output":"Deployment failed: Command execution failed (exit code 1): docker exec x bash -c 'cd /artifacts/x && sh build.sh'\nError:   * If you need ICU, install\n  * package icu-data-full.\nTraceback (most recent call last):\n  File \"/artifacts/x/build.py\", line 48, in need\n    raise RuntimeError(message)\n    ~~~~^^^^\nRuntimeError: Vemela runtime is not running\n"},
{"type":"stderr","output":"Error type: DeploymentException","hidden":true},
{"type":"stderr","output":"Error code: 1","hidden":true},
{"type":"stderr","output":"Location: /var/www/html/app/Traits/ExecuteRemoteCommand.php:259","hidden":true},
{"type":"stderr","output":"========================================"},
{"type":"stdout","output":"Gracefully shutting down build container: x"}]`
	if got, want := lastDeployError(logs), "RuntimeError: Vemela runtime is not running"; got != want {
		t.Fatalf("lastDeployError = %q, want %q", got, want)
	}
	if got := lastDeployError("not json"); got != "" {
		t.Fatalf("lastDeployError(not json) = %q", got)
	}
}

func TestRehostRegex(t *testing.T) {
	pairs := map[string]string{
		"vemela.app":     "ir.vemela.app",
		"api.vemela.app": "irapi.vemela.app",
	}
	cases := []struct {
		in, want string
		wild     []string
	}{
		{"rule=HostRegexp(`^[a-z0-9-]+[.]vemela[.]app$`)", "rule=HostRegexp(`^[a-z0-9-]+[.]ir[.]vemela[.]app$`)", []string{"*.ir.vemela.app"}},
		{`rule=HostRegexp(` + "`" + `^[a-z0-9-]+\.vemela\.app$` + "`" + `)`, `rule=HostRegexp(` + "`" + `^[a-z0-9-]+\.ir\.vemela\.app$` + "`" + `)`, []string{"*.ir.vemela.app"}},
		{`- "traefik.http.routers.t.rule=HostRegexp(` + "`" + `^[a-z0-9-]+\.vemela\.app$` + "`" + `)"`, `- "traefik.http.routers.t.rule=HostRegexp(` + "`" + `^[a-z0-9-]+\.ir\.vemela\.app$` + "`" + `)"`, []string{"*.ir.vemela.app"}},
		{"HostRegexp(`^(?:[a-z]+[.])?vemela[.]app$`)", "HostRegexp(`^(?:[a-z]+[.])?ir[.]vemela[.]app$`)", []string{"*.ir.vemela.app"}},
		// An exact name written as a pattern: no subdomains.
		{"HostRegexp(`^vemela[.]app$`) HostRegexp(`^api[.]vemela[.]app$`)", "HostRegexp(`^ir[.]vemela[.]app$`) HostRegexp(`^irapi[.]vemela[.]app$`)", nil},
		// The subdomain of another name, and names already moved, stay.
		{"^shop[.]vemela[.]app$ ^irapi[.]vemela[.]app$ ^x[.]ir[.]vemela[.]app$", "^shop[.]vemela[.]app$ ^irapi[.]vemela[.]app$ ^x[.]ir[.]vemela[.]app$", nil},
		{"^notvemela[.]app$ ^vemela[.]application$", "^notvemela[.]app$ ^vemela[.]application$", nil},
		{"CORS=https://*.vemela.app", "CORS=https://*.ir.vemela.app", []string{"*.ir.vemela.app"}},
		{"COOKIE_DOMAIN=.vemela.app", "COOKIE_DOMAIN=.ir.vemela.app", nil},
	}
	for _, c := range cases {
		got, wild := RehostWildcards(c.in, pairs)
		if got != c.want {
			t.Errorf("Rehost(%q) = %q, want %q", c.in, got, c.want)
		}
		if strings.Join(wild, ",") != strings.Join(c.wild, ",") {
			t.Errorf("wildcards of %q = %v, want %v", c.in, wild, c.wild)
		}
		if again, w2 := RehostWildcards(got, pairs); again != got || len(w2) != 0 {
			t.Errorf("Rehost is not idempotent on %q: %q %v", got, again, w2)
		}
	}
}
