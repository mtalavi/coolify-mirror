package archive

import (
	"errors"
	"io"
	"path/filepath"
	"testing"
)

// A backup made with a recovery key opens with its own key and with the
// recovery key, and with nothing else.
func TestRecoveryKey(t *testing.T) {
	backupKey, _, err := NewIdentityKey()
	must(t, err)
	recoverySecret, recoveryPublic, err := NewIdentityKey()
	must(t, err)
	if pub, err := PublicKeyOf(recoverySecret); err != nil || pub != recoveryPublic {
		t.Fatalf("PublicKeyOf: %q %v", pub, err)
	}
	file := filepath.Join(t.TempDir(), "b.cmb")
	w, err := Create(file, backupKey, recoveryPublic)
	must(t, err)
	must(t, w.AddBytes("manifest.json", []byte(`{"ok":true}`)))
	must(t, w.Close())

	for name, key := range map[string]string{"backup key": backupKey, "recovery key": recoverySecret} {
		r, err := Open(file, key)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		h, err := r.Next()
		if err != nil || h.Name != "manifest.json" {
			t.Fatalf("%s: first entry %v %v", name, h, err)
		}
		b, _ := io.ReadAll(r)
		if string(b) != `{"ok":true}` {
			t.Fatalf("%s: content %q", name, b)
		}
		r.Close()
	}
	other, _, _ := NewIdentityKey()
	if _, err := Open(file, other); !errors.Is(err, ErrBadPassphrase) {
		t.Fatalf("another key: %v", err)
	}
	if _, err := Open(file, "a passphrase"); !errors.Is(err, ErrBadPassphrase) {
		t.Fatalf("a passphrase: %v", err)
	}
	if _, err := Create(filepath.Join(t.TempDir(), "x.cmb"), "passphrase", recoveryPublic); err == nil {
		t.Fatal("a passphrase key with a recovery key was accepted")
	}
	if ValidPublicKey(recoveryPublic) != nil || ValidPublicKey("age1nope") == nil {
		t.Fatal("ValidPublicKey")
	}
}
