package engine

import (
	"strings"
	"testing"

	"github.com/mtalavi/coolify-mirror/internal/docker"
)

func ctr(service, status string, exit int, health, restart string, restarts int) docker.Details {
	var d docker.Details
	d.ID = service + "-id"
	d.Name = "/" + service + "-x"
	d.RestartCount = restarts
	d.State.Status = status
	d.State.Running = status == "running"
	d.State.ExitCode = exit
	if health != "" {
		d.State.Health = &struct {
			Status string `json:"Status"`
		}{health}
	}
	d.HostConfig.RestartPolicy.Name = restart
	d.Config.Labels = map[string]string{"com.docker.compose.service": service}
	return d
}

func TestContainerState(t *testing.T) {
	cases := []struct {
		name string
		d    docker.Details
		want int
	}{
		{"healthy", ctr("web", "running", 0, "healthy", "unless-stopped", 0), stateUp},
		{"no healthcheck", ctr("web", "running", 0, "", "unless-stopped", 0), stateUp},
		{"health starting", ctr("web", "running", 0, "starting", "unless-stopped", 0), stateStarting},
		{"unhealthy", ctr("web", "running", 0, "unhealthy", "unless-stopped", 0), stateBad},
		{"crashed", ctr("web", "exited", 1, "", "unless-stopped", 0), stateBad},
		{"stopped service", ctr("web", "exited", 0, "", "unless-stopped", 0), stateBad},
		{"finished job", ctr("migrate", "exited", 0, "", "no", 0), stateDone},
		{"failed job", ctr("migrate", "exited", 2, "", "no", 0), stateBad},
		{"restarting", ctr("web", "restarting", 1, "", "always", 3), stateBad},
		{"paused", func() docker.Details { d := ctr("web", "paused", 0, "", "always", 0); d.State.Running = true; return d }(), stateBad},
		{"created", ctr("web", "created", 0, "", "always", 0), stateStarting},
	}
	for _, c := range cases {
		if got := containerState(c.d); got != c.want {
			t.Errorf("%s: state %d, want %d", c.name, got, c.want)
		}
	}
}

func TestJudge(t *testing.T) {
	healthy := ctr("app", "running", 0, "healthy", "unless-stopped", 0)
	// Multi-service compose: database, a one-shot migration and two services.
	compose := []docker.Details{
		ctr("db", "running", 0, "healthy", "unless-stopped", 0),
		ctr("migrate", "exited", 0, "", "no", 0),
		ctr("web", "running", 0, "healthy", "unless-stopped", 0),
		ctr("worker", "running", 0, "", "unless-stopped", 0),
	}
	cases := []struct {
		name   string
		ds     []docker.Details
		expect []string
		ready  bool
		detail string
	}{
		{"healthy application", []docker.Details{healthy}, []string{"app"}, true, ""},
		{"no containers", nil, []string{"app"}, false, "waiting for containers"},
		// Regression: an application that exited right after the restore was reported running.
		{"exits after restore", []docker.Details{ctr("app", "exited", 1, "", "unless-stopped", 0)}, nil, false, "exited (1)"},
		{"unhealthy application", []docker.Details{ctr("app", "running", 0, "unhealthy", "unless-stopped", 0)}, nil, false, "unhealthy"},
		{"multi-service compose with finished job", compose, []string{"db", "migrate", "web", "worker"}, true, ""},
		{"compose service missing", compose[:3], []string{"db", "migrate", "web", "worker"}, false, "not created: worker"},
		{"compose one service unhealthy", append(append([]docker.Details{}, compose[:3]...), ctr("worker", "running", 0, "unhealthy", "unless-stopped", 0)), nil, false, "worker unhealthy"},
		// Single-container applications: the service label carries the deploy time.
		{"application redeployed", []docker.Details{ctr("abc-213329158788", "running", 0, "healthy", "unless-stopped", 0)}, []string{"abc-194208935083"}, true, ""},
		// Coolify 4.4 names containers <name>-<YYYYMMDD>T<HHMMSS> (regression: "not created").
		{"application redeployed (4.4 names)", []docker.Details{ctr("abc-20261008T010203", "running", 0, "healthy", "unless-stopped", 0)}, []string{"abc-20261007T102424"}, true, ""},
		{"4.4 container name prefix", []docker.Details{ctr("shop-api-20261008T010203", "running", 0, "healthy", "unless-stopped", 0)}, []string{"shop-api-20260908T141530"}, true, ""},
		{"still starting", []docker.Details{ctr("app", "running", 0, "starting", "unless-stopped", 0)}, nil, false, "starting"},
	}
	for _, c := range cases {
		ready, detail := judge(c.ds, c.expect)
		if ready != c.ready || !strings.Contains(detail, c.detail) {
			t.Errorf("%s: ready=%v detail=%q, want ready=%v detail containing %q", c.name, ready, detail, c.ready, c.detail)
		}
	}
}

func TestRestarted(t *testing.T) {
	if restarted(map[string]int{"a": 1}, map[string]int{"a": 1}) {
		t.Error("same counts reported as restart")
	}
	if !restarted(map[string]int{"a": 1}, map[string]int{"a": 2}) {
		t.Error("restart not detected")
	}
	if !restarted(map[string]int{"a": 0}, map[string]int{"b": 0}) {
		t.Error("replaced container not detected")
	}
}

// Regression: the domain answered "no available server" from Traefik while the
// restore had been reported as a success.
func TestProxyError(t *testing.T) {
	cases := []struct {
		code int
		body string
		bad  bool
	}{
		{503, "no available server\n", true},
		{404, "404 page not found\n", true},
		{502, "Bad Gateway", true},
		{504, "Gateway Timeout", true},
		{0, "", true},
		{200, "<html>ok</html>", false},
		{302, "", false},
		{404, "<html>Not found</html>", false}, // the application's own page
		{503, "maintenance", false},            // the application answered
		{401, "", false},
	}
	for _, c := range cases {
		if got := proxyError(c.code, c.body) != ""; got != c.bad {
			t.Errorf("%d %q: proxy error = %v, want %v", c.code, c.body, got, c.bad)
		}
	}
}

func TestProbeTarget(t *testing.T) {
	cases := []struct{ domain, target, resolve string }{
		{"https://shop.example.com", "https://shop.example.com:443/", "shop.example.com:443:127.0.0.1"},
		// Regression guard: the port of a Coolify domain is the container port.
		{"http://app.example.com:8080", "http://app.example.com:80/", "app.example.com:80:127.0.0.1"},
		{"https://example.com/api", "https://example.com:443/api", "example.com:443:127.0.0.1"},
	}
	for _, c := range cases {
		target, resolve, ok := probeTarget(c.domain)
		if !ok || target != c.target || resolve != c.resolve {
			t.Errorf("%s: %s %s %v", c.domain, target, resolve, ok)
		}
	}
	if _, _, ok := probeTarget("not a url"); ok {
		t.Error("invalid domain accepted")
	}
}

func TestVerdict(t *testing.T) {
	if v := Verdict(0, 0, nil); v != "" {
		t.Errorf("clean restore: %q", v)
	}
	if v := Verdict(1, 0, nil); !strings.Contains(v, "NOT operational") {
		t.Errorf("failed resource: %q", v)
	}
	if v := Verdict(0, 1, nil); v == "" {
		t.Error("a resource held back by a domain clash is not a success")
	}
	if v := Verdict(0, 0, []string{"x: host file missing"}); !strings.Contains(v, "dependency") {
		t.Errorf("missing deploy dependency: %q", v)
	}
}
