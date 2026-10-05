package lcrypt

import (
	"errors"
	"strings"
	"testing"
)

// Vectors produced by Laravel 12 (Illuminate\Encryption\Encrypter, AES-256-CBC)
// inside a real Coolify 4.3.23 container.
const (
	testKey        = "base64:q9X8m2Gv4hL0cQ1sR7tY3uW6eZ5aB8dF1gH2jK4lM0o="
	laravelString  = "eyJpdiI6IktkYUZVSCt0YlU0ZGdHQ2wvd0tMSXc9PSIsInZhbHVlIjoiTlNta2hha2VXSVBOdTFyZlZNSDNNdE9wMEFkQkJ4ZWt6a2FiMTh5M0JFOD0iLCJtYWMiOiI0NDdmNjcwMzlkNGE3MmYzMDg2YTVhM2I4ZjAwNzIzYmQzYjE1NTNlYTA0ZjJjMDVkY2I0ZDdmMDgxYjcxNjEwIiwidGFnIjoiIn0="
	laravelEncrypt = "eyJpdiI6IlpINDRTZ25FUlpEZDF6ZmdNS09vakE9PSIsInZhbHVlIjoibFJxbzk3SjFsRVlhK0xiYmo4R1pkN3RWTE5xbTFidDNWMS9iMlJBYXh3Zz0iLCJtYWMiOiIyNjNjZWYyN2VhY2Y5M2U2ODYxNTQzNjEyODFiNDMxY2ZkOGM4OTA2ODI0ZTJhNmU4OGY2MDIyZGMwODAxMGY2IiwidGFnIjoiIn0="
)

func TestDecryptLaravelVectors(t *testing.T) {
	e, err := New(testKey, "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := e.DecryptString(laravelString)
	if err != nil || string(got) != "hello from laravel" {
		t.Fatalf("encryptString vector: %q %v", got, err)
	}
	got, err = e.DecryptString(laravelEncrypt)
	if err != nil || string(got) != `s:16:"serialized value";` {
		t.Fatalf("encrypt() vector: %q %v", got, err)
	}
}

func TestRoundTripAndPreviousKeys(t *testing.T) {
	oldE, _ := New(testKey, "")
	payload, err := oldE.EncryptString([]byte("secret\nwith newline"))
	if err != nil {
		t.Fatal(err)
	}
	if !LooksEncrypted(payload) {
		t.Fatal("own payload not recognized")
	}
	newKey := "base64:" + strings.Repeat("A", 43) + "="
	newE, err := New(newKey, testKey)
	if err != nil {
		t.Fatal(err)
	}
	got, err := newE.DecryptString(payload)
	if err != nil || string(got) != "secret\nwith newline" {
		t.Fatalf("previous key decrypt: %q %v", got, err)
	}
	onlyNew, _ := New(newKey, "")
	if _, err := onlyNew.DecryptString(payload); !errors.Is(err, ErrWrongKey) {
		t.Fatalf("want ErrWrongKey, got %v", err)
	}
}

func TestTamperAndPlainText(t *testing.T) {
	e, _ := New(testKey, "")
	bad := strings.Replace(laravelString, "eyJpdiI6Ik", "eyJpdiI6Il", 1)
	if _, err := e.DecryptString(bad); err == nil {
		t.Fatal("tampered payload decrypted")
	}
	for _, s := range []string{"", "hello", "eyJfoo", strings.Repeat("x", 300)} {
		if LooksEncrypted(s) {
			t.Fatalf("%q looks encrypted", s)
		}
	}
}

func TestParseKey(t *testing.T) {
	if _, err := ParseKey("base64:abc"); err == nil {
		t.Fatal("short key accepted")
	}
	if _, err := ParseKey(`"` + testKey + `"`); err != nil {
		t.Fatal(err)
	}
}
