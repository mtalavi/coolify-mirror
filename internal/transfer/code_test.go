package transfer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A share code alone is enough: the downloader derives the token and the
// certificate pin, and fetches the backup key from the share.
func TestShareCode(t *testing.T) {
	secret := NewSecret()
	token, err := CodeToken(secret)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := NewCodeCert(secret)
	if err != nil {
		t.Fatal(err)
	}
	if pin, _ := CodePin(secret); pin != cert.Pin {
		t.Fatalf("pin of the code %s != certificate pin %s", pin, cert.Pin)
	}
	blob, err := WrapKey(secret, "the-backup-key")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "x.cmb")
	_ = os.WriteFile(file, []byte(strings.Repeat("a", 1000)), 0o600)
	s := &Server{File: file, Token: token, Cert: cert, KeyBlob: blob}
	addr, err := s.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	code := FormatCode(addr.String(), secret)
	if strings.Count(code, "-") != 6 {
		t.Fatalf("code not grouped: %s", code)
	}
	for _, typed := range []string{code, " " + strings.ToUpper(code) + " ", strings.ReplaceAll(code, "-", "")} {
		loc, key, isURL, err := ParseSource(typed)
		if err != nil || !isURL {
			t.Fatalf("%q: %v %v", typed, isURL, err)
		}
		got, err := FetchKey(context.Background(), loc, key)
		if err != nil || got != "the-backup-key" {
			t.Fatalf("%q: key %q %v", typed, got, err)
		}
		if n, err := RemoteSize(context.Background(), loc); err != nil || n != 1000 {
			t.Fatalf("size %d %v", n, err)
		}
	}

	// Another code for the same server: refused by the pin before anything is read.
	other := FormatCode(addr.String(), NewSecret())
	loc, key, _, _ := ParseSource(other)
	if _, err := FetchKey(context.Background(), loc, key); err == nil {
		t.Fatal("a wrong code was accepted")
	}
	// Proxy mode: no port in the code means 443.
	loc, _, _, _ = ParseSource(FormatCode("203.0.113.10:443", secret))
	if !strings.HasPrefix(loc, "https://203.0.113.10:443/cm/"+token+"/") {
		t.Fatalf("proxy code: %s", loc)
	}
	// Files and links are not codes.
	if _, _, isURL, _ := ParseSource("backups/x.cmb"); isURL {
		t.Fatal("file path taken for a code")
	}
}
