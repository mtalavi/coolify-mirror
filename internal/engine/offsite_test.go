package engine

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSourceKinds(t *testing.T) {
	remote := []string{"gdrive:coolify", "gdrive:", "s3:bucket/backups/x.cmb", "my-store:a/b"}
	share := []string{"1.2.3.4/hi4i-2dzx-hmeg-42qp-palx-52s7-zq", "cm-e2e-src:8123/4jxt-tgzb-d4m3-3pe4-osyi-7trr-jm", "[2001:db8::1]:8123/abcd-efgh-ijkl-mnop"}
	neither := []string{"/data/backups", "./x.cmb", "https://example.com/b.cmb"}
	for _, s := range remote {
		if !IsRemote(s) || IsShareSource(s) {
			t.Errorf("%q: remote=%v share=%v, want remote", s, IsRemote(s), IsShareSource(s))
		}
	}
	for _, s := range share {
		if IsRemote(s) || !IsShareSource(s) {
			t.Errorf("%q: remote=%v share=%v, want share code", s, IsRemote(s), IsShareSource(s))
		}
	}
	for _, s := range neither {
		if IsRemote(s) || IsShareSource(s) {
			t.Errorf("%q: remote=%v share=%v, want neither", s, IsRemote(s), IsShareSource(s))
		}
	}
	if !IsOffsiteSource("https://example.com/b.cmb") || IsOffsiteSource("https://1.2.3.4/cm/tok/backup.cmb#key=x") {
		t.Error("https sources")
	}
}

func TestBackupFileName(t *testing.T) {
	ts := time.Date(2026, 10, 27, 3, 30, 0, 0, time.Local)
	got := BackupFileName("Shop-1.example", ModeFull, "4.5.0", ts)
	if got != "coolify-shop-1-example-full-coolify4.5.0-20261027-033000.cmb" {
		t.Fatalf("name %q", got)
	}
	if !strings.HasPrefix(got, BackupPrefix("Shop-1.example", ModeFull)) {
		t.Fatal("prefix")
	}
	if !stampTime(strings.TrimSuffix(got, ".cmb"), time.Time{}).Equal(ts) {
		t.Fatal("the time is no longer read from the name")
	}
	if !strings.Contains(BackupFileName("h", ModeSelective, "4.0.0-beta.420", ts), "coolify4.0.0.beta.420") {
		t.Fatal("beta version")
	}
}

func TestOnCalendarAndUnits(t *testing.T) {
	for in, want := range map[string]string{"03:30": "*-*-* 03:30:00", "3:05": "*-*-* 03:05:00"} {
		if got, err := OnCalendar(in); err != nil || got != want {
			t.Errorf("OnCalendar(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := OnCalendar("25:00"); err == nil {
		t.Error("25:00 accepted")
	}
	svc, tmr := ScheduleUnits("nightly", "*-*-* 03:30:00", "/usr/local/bin/coolify-mirror", []string{"--full", "--to", "gdrive:my backups", "--keep-remote", "7"})
	if !strings.Contains(svc, `ExecStart=/usr/local/bin/coolify-mirror backup --full --to "gdrive:my backups" --keep-remote 7 --schedule-name nightly`) {
		t.Fatalf("service:\n%s", svc)
	}
	if !strings.Contains(tmr, "OnCalendar=*-*-* 03:30:00") || !strings.Contains(tmr, "Persistent=true") {
		t.Fatalf("timer:\n%s", tmr)
	}
}

func TestLocalDestination(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(src, 0o700))
	uploadedList = filepath.Join(dir, "uploaded.txt")
	prefix := BackupPrefix("h", ModeFull)
	var files []string
	for i := 0; i < 4; i++ {
		name := BackupFileName("h", ModeFull, "4.5.0", time.Date(2026, 10, 20+i, 3, 30, 0, 0, time.Local))
		p := filepath.Join(src, name)
		must(os.WriteFile(p, []byte(strings.Repeat("x", 1000+i)), 0o600))
		must(os.WriteFile(p+".key", []byte("k"), 0o600))
		files = append(files, p)
	}
	manual := filepath.Join(src, BackupFileName("h", ModeFull, "4.5.0", time.Date(2026, 10, 1, 3, 30, 0, 0, time.Local)))
	must(os.WriteFile(manual, []byte("made by hand"), 0o600))
	other := filepath.Join(dst, BackupFileName("other-server", ModeFull, "4.5.0", time.Date(2026, 9, 1, 1, 0, 0, 0, time.Local)))
	must(os.MkdirAll(dst, 0o700))
	must(os.WriteFile(other, []byte("another server's backup"), 0o600))

	for _, f := range files {
		must(Upload(context.Background(), f, dst, nil))
		if _, err := AfterUpload(f, prefix, 1); err != nil {
			t.Fatal(err)
		}
	}
	del, err := PruneBackupsAt(context.Background(), dst, prefix, 2)
	must(err)
	if len(del) != 2 {
		t.Fatalf("pruned %v", del)
	}
	list, err := ListBackupsAt(context.Background(), dst)
	must(err)
	if len(list) != 3 || list[0].Name != filepath.Base(files[3]) {
		t.Fatalf("destination after prune: %+v", list)
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("another server's backup was deleted")
	}
	left, _ := os.ReadDir(src)
	var names []string
	for _, e := range left {
		names = append(names, e.Name())
	}
	want := map[string]bool{filepath.Base(files[3]): true, filepath.Base(files[3]) + ".key": true, filepath.Base(manual): true}
	if len(names) != len(want) {
		t.Fatalf("left here: %v", names)
	}
	for _, n := range names {
		if !want[n] {
			t.Fatalf("left here: %v", names)
		}
	}
	// a folder gives its newest backup
	got, err := FetchOffsite(context.Background(), dst, nil)
	must(err)
	if filepath.Base(got) != filepath.Base(files[3]) {
		t.Fatalf("newest: %s", got)
	}
}

func TestTelegram(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		got = r.URL.Path + " " + r.Form.Get("chat_id") + " " + r.Form.Get("text")
		if r.Form.Get("chat_id") == "bad" {
			_, _ = io.WriteString(w, `{"ok":false,"description":"Bad Request: chat not found"}`)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()
	TelegramAPI = srv.URL
	sent, errs := Notify(context.Background(), nil, NotifyConfig{TelegramChat: "12345", TelegramToken: "T0KEN"}, "subj", "body")
	if len(errs) != 0 || len(sent) != 1 || got != "/botT0KEN/sendMessage 12345 subj\n\nbody" {
		t.Fatalf("sent %v errs %v got %q", sent, errs, got)
	}
	_, errs = Notify(context.Background(), nil, NotifyConfig{TelegramChat: "bad", TelegramToken: "T0KEN"}, "s", "b")
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "chat not found") || strings.Contains(errs[0].Error(), "T0KEN") {
		t.Fatalf("errors %v", errs)
	}
}

// A tiny SMTP server: enough for net/smtp without TLS.
func TestSMTP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		w := func(s string) { _, _ = io.WriteString(c, s+"\r\n") }
		w("220 test")
		var data strings.Builder
		inData := false
		for {
			l, err := r.ReadString('\n')
			if err != nil {
				return
			}
			if inData {
				if l == ".\r\n" {
					inData = false
					w("250 ok")
					continue
				}
				data.WriteString(l)
				continue
			}
			switch {
			case strings.HasPrefix(l, "EHLO"), strings.HasPrefix(l, "HELO"):
				w("250 test")
			case strings.HasPrefix(l, "DATA"):
				inData = true
				w("354 go")
			case strings.HasPrefix(l, "QUIT"):
				w("221 bye")
				got <- data.String()
				return
			default:
				w("250 ok")
			}
		}
	}()
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	var p int
	for _, ch := range port {
		p = p*10 + int(ch-'0')
	}
	n := NotifyConfig{Email: "me@example.com", SMTP: &SMTPConfig{Host: "127.0.0.1", Port: p, From: "Backups <backups@example.com>", Security: "none"}}
	sent, errs := Notify(context.Background(), nil, n, "Recovery key", "AGE-SECRET-KEY-1TEST")
	if len(errs) != 0 || len(sent) != 1 {
		t.Fatalf("sent %v errs %v", sent, errs)
	}
	msg := <-got
	if !strings.Contains(msg, "Subject: Recovery key") || !strings.Contains(msg, "AGE-SECRET-KEY-1TEST") || !strings.Contains(msg, "To: me@example.com") {
		t.Fatalf("message:\n%s", msg)
	}
}
