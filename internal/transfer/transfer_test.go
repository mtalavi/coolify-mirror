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

func TestParseSource(t *testing.T) {
	loc, key, isURL, err := ParseSource(" 'http://1.2.3.4/cm/abc/backup.cmb#key=k3y' ")
	if err != nil || !isURL || key != "k3y" || loc != "http://1.2.3.4/cm/abc/backup.cmb" {
		t.Fatalf("%q %q %v %v", loc, key, isURL, err)
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "b.cmb")
	var last int64
	err := Download(context.Background(), srv.URL+"/cm/tok/backup.cmb", dest, func(done, total int64) { last = done })
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
	s := &Server{File: file, Token: "tok"}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	if r, _ := http.Get(ts.URL + "/cm/wrong/" + BackupName); r.StatusCode != http.StatusNotFound {
		t.Fatalf("wrong token: %d", r.StatusCode)
	}
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/cm/tok/"+BackupName, nil)
	req.Header.Set("Range", "bytes=990-")
	r, err := http.DefaultClient.Do(req)
	if err != nil || r.StatusCode != http.StatusPartialContent || r.ContentLength != 10 {
		t.Fatalf("range: %v %d %d", err, r.StatusCode, r.ContentLength)
	}
	if n, err := RemoteSize(context.Background(), ts.URL+"/cm/tok/"+BackupName); err != nil || n != 1000 {
		t.Fatalf("size %d %v", n, err)
	}
}
