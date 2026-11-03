package dbx

import (
	"strings"
	"testing"

	"github.com/mtalavi/coolify-mirror/internal/coolify"
	"github.com/mtalavi/coolify-mirror/internal/lcrypt"
)

// Coolify 4.6 adds encrypted HTTPS Git credentials. They must use the target
// key after both an ordinary selective import and an independent copy.
func TestHTTPSCredentialsReencrypted(t *testing.T) {
	for _, decision := range []Decision{KeepUUID, NewCopy} {
		t.Run(map[Decision]string{KeepUUID: "move", NewCopy: "copy"}[decision], func(t *testing.T) {
			source, err := lcrypt.New(strings.Repeat("s", 32), "")
			if err != nil {
				t.Fatal(err)
			}
			dest, err := lcrypt.New(strings.Repeat("d", 32), "")
			if err != nil {
				t.Fatal(err)
			}
			const secret = "git-token-with-'quotes'-and-\\slashes"
			cipher, err := source.EncryptString([]byte(secret))
			if err != nil {
				t.Fatal(err)
			}
			ex := sampleExport()
			c := &collector{in: &coolify.Instance{Crypt: source}, ex: ex}
			ex.Tables["applications"][0]["git_https_username"] = "git-user"
			ex.Tables["applications"][0]["git_https_password"] = c.norm("applications.git_https_password", cipher)
			ts := target(nil)
			ts.Columns["applications"]["git_https_username"] = "text"
			ts.Columns["applications"]["git_https_password"] = "text"
			p, err := BuildPlan(ex, ts, func(coolify.Resource) Decision { return decision }, allocator(), dest.EncryptString)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, row := range p.rows {
				if row.table != "applications" {
					continue
				}
				found = true
				value, ok := row.row["git_https_password"].(string)
				if !ok || value == cipher || value == secret || row.row["git_https_username"] != "git-user" {
					t.Fatal("credentials were lost or copied without re-encryption")
				}
				plain, err := dest.DecryptString(value)
				if err != nil || string(plain) != secret {
					t.Fatal("target key cannot recover the original Git credential")
				}
				if _, err := source.DecryptString(value); err == nil {
					t.Fatal("restored credential still accepts the source key")
				}
			}
			if !found || len(ex.Warnings) != 0 {
				t.Fatal("application missing or export emitted an encryption warning")
			}
		})
	}
}
