package dbx

import (
	"net/netip"
	"strings"
	"testing"

	"github.com/mtalavi/coolify-mirror/internal/coolify"
)

func TestAllowedBy(t *testing.T) {
	entries := parseInternalHosts(`["10.0.0.0/8", "Vault.LAN", "192.168.1.5", " "]`)
	for host, want := range map[string]bool{
		"10.2.3.4": true, "vault.lan": true, "192.168.1.5": true, "[::ffff:10.0.0.1]": true,
		"192.168.1.6": false, "minio.lan": false, "11.0.0.1": false,
	} {
		if got := allowedBy(host, entries); got != want {
			t.Errorf("allowedBy(%q) = %v, want %v", host, got, want)
		}
	}
	if got := parseInternalHosts("a.lan, 10.0.0.1"); len(got) != 2 || got[1] != "10.0.0.1" {
		t.Errorf("comma list: %v", got)
	}
	if parseInternalHosts("null") != nil || parseInternalHosts("") != nil {
		t.Error("empty setting")
	}
	for raw, want := range map[string]string{
		"http://10.0.0.5:8200": "10.0.0.5", "https://vault.lan/": "vault.lan", "minio.lan:9000": "minio.lan",
		"http://[fd00::1]:9000": "fd00::1", "": "",
	} {
		if got := urlHost(raw); got != want {
			t.Errorf("urlHost(%q) = %q, want %q", raw, got, want)
		}
	}
}

// A secret manager on a private address the old server allowed: the new
// server is told to allow it too.
func TestInternalHostWarnings(t *testing.T) {
	ex := sampleExport()
	ex.InternalHosts = []string{"10.0.0.0/8"}
	ex.Tables["integration_tokens"] = []coolify.Row{{"id": num(21), "uuid": "tokuuid00000000000000001", "name": "Vault",
		"provider": "vault", "team_id": num(5), "token": enc("s.root"), "metadata": map[string]any{"base_url": "http://10.0.0.5:8200"}}}
	ex.Tables["secret_manager_links"] = []coolify.Row{{"id": num(22), "uuid": "linkuuid0000000000000001",
		"resourceable_type": coolify.MorphApplication, "resourceable_id": num(7), "integration_token_id": num(21)}}
	warn := func(targetHosts []string, existing map[string]map[string]int64) string {
		ts := target(existing)
		ts.InternalHosts = targetHosts
		return strings.Join(InternalHostWarnings(ex, ts), "\n")
	}
	if w := warn(nil, nil); !strings.Contains(w, `secret manager "Vault" connects to 10.0.0.5`) {
		t.Errorf("warnings %q", w)
	}
	if w := warn([]string{"10.0.0.5"}, nil); w != "" {
		t.Errorf("allowed on the target, still warned: %q", w)
	}
	if w := warn(nil, map[string]map[string]int64{"integration_tokens": {"tokuuid00000000000000001": 40}}); w != "" {
		t.Errorf("the target's own secret manager was checked: %q", w)
	}
}

// Host names are resolved on this server: a private result needs an allowed
// entry here, a public one does not.
func TestInternalHostWarningsHostnames(t *testing.T) {
	saved := lookupHost
	defer func() { lookupHost = saved }()
	lookupHost = func(host string) []netip.Addr {
		switch host {
		case "vault.internal":
			return []netip.Addr{netip.MustParseAddr("10.0.0.5")}
		case "s3.example.com":
			return []netip.Addr{netip.MustParseAddr("52.1.2.3")}
		}
		return nil
	}
	warn := func(endpoint string, src, dst []string) string {
		ex := sampleExport()
		ex.InternalHosts = src
		ex.Tables["s3_storages"] = []coolify.Row{{"id": num(30), "uuid": "s3uuid000000000000000001", "name": "lab-s3", "endpoint": endpoint}}
		ts := target(nil)
		ts.InternalHosts = dst
		return strings.Join(InternalHostWarnings(ex, ts), "\n")
	}
	if w := warn("http://vault.internal:9000", []string{"10.0.0.0/8"}, nil); !strings.Contains(w, "the old server allows") {
		t.Errorf("hostname allowed by CIDR on the old server: %q", w)
	}
	if w := warn("http://vault.internal:9000", nil, []string{"10.0.0.0/8"}); w != "" {
		t.Errorf("allowed here by CIDR: %q", w)
	}
	if w := warn("http://vault.internal:9000", nil, []string{"vault.internal"}); w != "" {
		t.Errorf("allowed here by name: %q", w)
	}
	if w := warn("https://s3.example.com", []string{"s3.example.com"}, nil); w != "" {
		t.Errorf("public address warned: %q", w)
	}
	if w := warn("http://minio.lan:9000", []string{"minio.lan"}, nil); !strings.Contains(w, "minio.lan") {
		t.Errorf("unresolvable name the old server allowed: %q", w)
	}
	if w := warn("http://minio.lan:9000", nil, nil); w != "" {
		t.Errorf("unresolvable unknown name warned: %q", w)
	}
}
