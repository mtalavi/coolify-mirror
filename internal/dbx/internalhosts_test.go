package dbx

import (
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
