package transfer

import (
	"bytes"
	"context"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testPin = "47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU="

func TestParseSource(t *testing.T) {
	loc, key, isURL, err := ParseSource(" 'https://1.2.3.4/cm/abc/backup.cmb#key=k3y&pin=" + testPin + "' ")
	if err != nil || !isURL || key != "k3y" || !strings.HasPrefix(loc, "https://1.2.3.4/cm/abc/backup.cmb#pin=") {
		t.Fatalf("%q %q %v %v", loc, key, isURL, err)
	}
	if _, _, _, err := ParseSource("http://1.2.3.4/cm/abc/backup.cmb#key=k3y"); err == nil {
		t.Fatal("plain http link accepted")
	}
	if _, _, _, err := ParseSource("https://1.2.3.4/cm/abc/backup.cmb#key=k3y"); err == nil {
		t.Fatal("link without pin accepted")
	}
	loc, key, isURL, _ = ParseSource("/data/x.cmb")
	if isURL || key != "" || loc != "/data/x.cmb" {
		t.Fatalf("file: %q %q %v", loc, key, isURL)
	}
}

// The first response is cut in the middle; Download must resume with Range.
func TestDownloadResumes(t *testing.T) {
	data := make([]byte, 3<<20)
	_, _ = rand.Read(data)
	var calls, ranged atomic.Int32
	cert, err := NewCert("tok")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if r.Header.Get("Range") != "" {
			ranged.Add(1)
		}
		if n == 1 && r.Method == http.MethodGet {
			w.Header().Set("Content-Length", "3145728")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(data[:1<<20])
			w.(http.Flusher).Flush()
			conn, _, _ := w.(http.Hijacker).Hijack()
			conn.Close()
			return
		}
		http.ServeContent(w, r, "backup.cmb", time.Now(), bytes.NewReader(data))
	}))
	srv.TLS = cert.serverConfig()
	srv.StartTLS()
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "b.cmb")
	var last int64
	err = Download(context.Background(), srv.URL+"/cm/tok/backup.cmb#pin="+cert.Pin, dest, func(done, total int64) { last = done })
	if err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, data) {
		t.Fatalf("content mismatch (%d bytes)", len(got))
	}
	if ranged.Load() == 0 {
		t.Fatal("download did not resume with a Range request")
	}
	if last == 0 {
		t.Fatal("no progress reported")
	}
}

func TestServerTokenAndRange(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "x.cmb")
	_ = os.WriteFile(file, []byte(strings.Repeat("a", 1000)), 0o600)
	cert, _ := NewCert("tok")
	s := &Server{File: file, Token: "tok", Cert: cert}
	addr, err := s.Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	base := "https://" + addr.String()
	client, _, err := newClient(base + "/cm/tok/x#pin=" + cert.Pin)
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := client.Get(base + "/cm/wrong/" + BackupName); r == nil || r.StatusCode != http.StatusNotFound {
		t.Fatalf("wrong token: %v", r)
	}
	req, _ := http.NewRequest(http.MethodGet, base+"/cm/tok/"+BackupName, nil)
	req.Header.Set("Range", "bytes=990-")
	r, err := client.Do(req)
	if err != nil || r.StatusCode != http.StatusPartialContent || r.ContentLength != 10 {
		t.Fatalf("range: %v %d %d", err, r.StatusCode, r.ContentLength)
	}
	if n, err := RemoteSize(context.Background(), base+"/cm/tok/"+BackupName+"#pin="+cert.Pin); err != nil || n != 1000 {
		t.Fatalf("size %d %v", n, err)
	}
	// A different certificate (pin mismatch) is refused.
	other, _ := NewCert("tok")
	if _, err := RemoteSize(context.Background(), base+"/cm/tok/"+BackupName+"#pin="+other.Pin); err == nil || !strings.Contains(err.Error(), "pin mismatch") {
		t.Fatalf("pin mismatch not detected: %v", err)
	}
}
