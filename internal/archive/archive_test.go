package archive

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// buildTree creates files with the metadata a docker volume typically has.
func buildTree(t *testing.T, root string) {
	must(t, os.MkdirAll(filepath.Join(root, "pgdata", "base"), 0o700))
	must(t, os.WriteFile(filepath.Join(root, "pgdata", "base", "1234"), bytes.Repeat([]byte("x"), 300000), 0o600))
	must(t, os.WriteFile(filepath.Join(root, "index.html"), []byte("<h1>hi</h1>\n"), 0o644))
	must(t, os.WriteFile(filepath.Join(root, "empty"), nil, 0o640))
	must(t, os.Symlink("index.html", filepath.Join(root, "home.html")))
	must(t, os.Symlink("/etc/passwd", filepath.Join(root, "abs-link")))
	must(t, os.Link(filepath.Join(root, "index.html"), filepath.Join(root, "hard.html")))
	must(t, os.WriteFile(filepath.Join(root, "suid"), []byte("#!/bin/sh\n"), 0o755))
	must(t, os.Chmod(filepath.Join(root, "suid"), 0o4755))
	must(t, unix.Mkfifo(filepath.Join(root, "fifo"), 0o600))
	if os.Geteuid() == 0 {
		must(t, os.Chown(filepath.Join(root, "pgdata"), 70, 70))
		must(t, os.Chown(filepath.Join(root, "pgdata", "base", "1234"), 70, 70))
		must(t, os.Lchown(filepath.Join(root, "home.html"), 101, 101))
	}
	_ = unix.Setxattr(filepath.Join(root, "index.html"), "user.coolify", []byte("mirror"), 0)
}

func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	buildTree(t, src)

	file := filepath.Join(dir, "b.cmb")
	w, err := Create(file, "correct horse")
	must(t, err)
	must(t, w.AddBytes("manifest.json", []byte(`{"ok":true}`)))
	st, err := w.AddTree("volumes/test", src, TreeOptions{})
	must(t, err)
	if st.Files < 4 || st.Links < 2 {
		t.Fatalf("unexpected stats %+v", st)
	}
	must(t, w.Close())

	if _, err := Open(file, "wrong"); !errors.Is(err, ErrBadPassphrase) {
		t.Fatalf("wrong passphrase: %v", err)
	}
	r, err := Open(file, "correct horse")
	must(t, err)
	defer r.Close()
	dst := filepath.Join(dir, "dst")
	x, err := NewExtractor(dst)
	must(t, err)
	for {
		h, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		must(t, err)
		if h.Name == "manifest.json" {
			b, _ := io.ReadAll(r)
			if string(b) != `{"ok":true}` {
				t.Fatalf("manifest %q", b)
			}
			continue
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(h.Name, "volumes/test"), "/")
		link := strings.TrimPrefix(strings.TrimPrefix(h.Linkname, "volumes/test"), "/")
		must(t, x.Entry(rel, h, r, link))
	}
	must(t, x.Finish())

	for _, rel := range []string{"pgdata", "pgdata/base/1234", "index.html", "empty", "home.html", "abs-link", "hard.html", "suid", "fifo"} {
		a, err := os.Lstat(filepath.Join(src, rel))
		must(t, err)
		b, err := os.Lstat(filepath.Join(dst, rel))
		must(t, err)
		if a.Mode() != b.Mode() {
			t.Errorf("%s: mode %v != %v", rel, b.Mode(), a.Mode())
		}
		as, bs := a.Sys().(*syscall.Stat_t), b.Sys().(*syscall.Stat_t)
		if as.Uid != bs.Uid || as.Gid != bs.Gid {
			t.Errorf("%s: owner %d:%d != %d:%d", rel, bs.Uid, bs.Gid, as.Uid, as.Gid)
		}
		if a.Mode().IsRegular() && !a.ModTime().Equal(b.ModTime()) {
			t.Errorf("%s: mtime %v != %v", rel, b.ModTime(), a.ModTime())
		}
	}
	got, _ := os.ReadFile(filepath.Join(dst, "pgdata", "base", "1234"))
	if len(got) != 300000 {
		t.Fatalf("content size %d", len(got))
	}
	if l, _ := os.Readlink(filepath.Join(dst, "abs-link")); l != "/etc/passwd" {
		t.Fatalf("symlink target %q", l)
	}
	a, _ := os.Stat(filepath.Join(dst, "index.html"))
	b, _ := os.Stat(filepath.Join(dst, "hard.html"))
	if !os.SameFile(a, b) {
		t.Fatal("hardlink not preserved")
	}
	buf := make([]byte, 16)
	if n, err := unix.Getxattr(filepath.Join(src, "index.html"), "user.coolify", buf); err == nil && n > 0 {
		n2, err := unix.Getxattr(filepath.Join(dst, "index.html"), "user.coolify", buf)
		if err != nil || string(buf[:n2]) != "mirror" {
			t.Fatalf("xattr not preserved: %v", err)
		}
	}
}

func TestExtractorRejectsEscapes(t *testing.T) {
	x, err := NewExtractor(t.TempDir())
	must(t, err)
	if _, err := x.safeJoin("../../etc/passwd"); err == nil {
		t.Fatal("escape accepted")
	}
	x.symlinks["/link"] = true
	if _, err := x.safeJoin("link/inside"); err == nil {
		t.Fatal("write through symlink accepted")
	}
}
