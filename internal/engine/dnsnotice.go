package engine

import (
	"fmt"
	"strings"
)

// DNSNoticeTitle heads the warning shown when every subdomain of a domain
// moved with a restore (see DNSNotice).
const DNSNoticeTitle = "Action needed in your DNS (Cloudflare): subdomains moved"

// DNSNotice tells what a restore cannot do itself after it moved a route for
// every subdomain (wild, e.g. "*.ir.example.com" from a HostRegexp rule such
// as ^[a-z0-9-]+[.]example[.]com$): the names only answer once the DNS has a
// record for them and a certificate covers them. ip is this server's public
// address ("" when unknown).
func DNSNotice(wild []string, ip string) []string {
	if len(wild) == 0 {
		return nil
	}
	target := ip
	if target == "" {
		target = "this server's IP"
	}
	lines := []string{
		"coolify-mirror found a route for every subdomain (a HostRegexp rule or a *.wildcard) and moved it to the new domain:",
	}
	for _, w := range wild {
		lines = append(lines, "  "+w)
	}
	lines = append(lines, "These subdomains do not open until you do this at your DNS provider (Cloudflare):")
	step := 1
	for _, w := range wild {
		base := strings.TrimPrefix(w, "*.")
		lines = append(lines, fmt.Sprintf("%d. DNS → Records: add  A  %s  →  %s  (proxied like your other records).", step, w, target))
		step++
		labels := strings.Split(base, ".")
		if len(labels) >= 3 {
			zone := strings.Join(labels[len(labels)-2:], ".")
			lines = append(lines, fmt.Sprintf("%d. Certificate: Cloudflare's free certificate covers only %s and *.%s, not %s. "+
				"In SSL/TLS → Edge Certificates order an Advanced Certificate for %s (or turn on Total TLS). "+
				"Without Cloudflare's proxy this server needs a wildcard certificate for %s (DNS challenge).",
				step, zone, zone, w, w, w))
			step++
		}
	}
	lines = append(lines, "Until then, e.g. shop."+strings.TrimPrefix(wild[0], "*.")+" does not open. Everything else is not affected.")
	return lines
}
