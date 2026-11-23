package engine

import (
	"strings"
	"testing"
)

func TestDNSNotice(t *testing.T) {
	if DNSNotice(nil, "1.2.3.4") != nil {
		t.Fatal("a notice without wildcards")
	}
	got := strings.Join(DNSNotice([]string{"*.ir.vemela.app"}, "203.0.113.7"), "\n")
	for _, want := range []string{"A  *.ir.vemela.app  →  203.0.113.7", "covers only vemela.app and *.vemela.app", "Advanced Certificate for *.ir.vemela.app", "shop.ir.vemela.app"} {
		if !strings.Contains(got, want) {
			t.Errorf("notice lacks %q:\n%s", want, got)
		}
	}
	// One level below the zone: Cloudflare's free certificate covers it.
	if got := strings.Join(DNSNotice([]string{"*.example.com"}, ""), "\n"); strings.Contains(got, "Certificate:") || !strings.Contains(got, "this server's IP") {
		t.Errorf("notice for *.example.com:\n%s", got)
	}
}
