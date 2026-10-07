package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mtalavi/coolify-mirror/internal/archive"
	"github.com/mtalavi/coolify-mirror/internal/coolify"
)

func writeFile(t *testing.T, p string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(strings.Repeat("x", size)), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestListStored(t *testing.T) {
	home := t.TempDir()
	// A real backup with its key: its apps are read from the manifest.
	backup := filepath.Join(home, "backups", "coolify-src-selective-20261006-120000.cmb")
	_ = os.MkdirAll(filepath.Dir(backup), 0o700)
	w, err := archive.Create(backup, "test-key")
	if err != nil {
		t.Fatal(err)
	}
	man, _ := json.Marshal(Manifest{Format: FormatName, Mode: ModeSelective,
		Resources: []coolify.Resource{{Name: "Lift"}, {Name: "Norino"}}})
	if err := w.AddBytes(entryManifest, man); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(backup+".key", []byte("test-key\n"), 0o600)
	full := filepath.Join(home, "backups", "coolify-src-full-20261005-100000.cmb")
	writeFile(t, full, 100) // no key: described from its name
	writeFile(t, filepath.Join(home, "backups", "coolify-src-full-20261004-100000.cmb.part"), 50)
	writeFile(t, filepath.Join(home, "backups", "gone.cmb.key"), 5)
	writeFile(t, filepath.Join(home, "incoming", "backup-abc.cmb"), 300)
	writeFile(t, filepath.Join(home, "incoming", "backup-def.cmb.part"), 70)
	writeFile(t, filepath.Join(home, "pre-restore-20261006-130000", "coolify.dump"), 40)
	writeFile(t, filepath.Join(home, "replaced-20261006-140000", "data", "coolify", "x"), 30)
	writeFile(t, filepath.Join(home, "share-aaaaaaaa", "cert.pem"), 3) // ended share
	writeFile(t, filepath.Join(home, "share-bbbbbbbb", "cert.pem"), 3) // running share
	writeFile(t, filepath.Join(home, "logs", "old.log"), 10)
	writeFile(t, filepath.Join(home, "logs", "now.log"), 10)
	writeFile(t, filepath.Join(home, "tmp", "dump-1.sql"), 8)
	CurrentLog = filepath.Join(home, "logs", "now.log")
	defer func() { CurrentLog = "" }()

	shares := []ShareInfo{
		{ID: "bbbbbbbb", File: full, CertDir: filepath.Join(home, "share-bbbbbbbb"), Dedicated: true},
	}
	list := listStoredIn(home, shares, false)
	sortStored(list)
	got := map[string]StoredFile{}
	var order []string
	for _, f := range list {
		got[f.Name] = f
		order = append(order, f.Name)
	}
	want := []string{
		"coolify-src-selective-20261006-120000.cmb", "coolify-src-full-20261005-100000.cmb",
		"backup-abc.cmb",
		"backup-def.cmb.part", "coolify-src-full-20261004-100000.cmb.part",
		"replaced-20261006-140000", "pre-restore-20261006-130000",
		"logs",
	}
	for _, n := range want {
		if _, ok := got[n]; !ok {
			t.Errorf("missing %s in %v", n, order)
		}
	}
	if order[0] != want[0] || order[1] != want[1] || order[2] != want[2] {
		t.Errorf("backups, newest first, then downloads: %v", order)
	}
	if _, ok := got["share-bbbbbbbb"]; ok {
		t.Error("the certificate of a running share was listed")
	}
	for _, n := range []string{"share-aaaaaaaa", "gone.cmb.key", "tmp"} {
		if got[n].Kind != StoredTemp {
			t.Errorf("%s should be a leftover: %+v", n, got[n])
		}
	}
	// "All backups": the complete ones made here and downloaded, no .part.
	var ready []string
	for _, f := range ReadyBackups(list) {
		ready = append(ready, f.Name)
	}
	if strings.Join(ready, " ") != strings.Join(want[:3], " ") {
		t.Errorf("ready backups: %v", ready)
	}
	b := got["coolify-src-selective-20261006-120000.cmb"]
	if b.About != "Lift, Norino" || len(b.files) != 2 {
		t.Errorf("backup: %+v", b)
	}
	if f := got["coolify-src-full-20261005-100000.cmb"]; f.About != "FULL server" || len(f.Shares) != 1 || f.Busy != "" {
		t.Errorf("shared full backup: %+v", f)
	}
	if l := got["logs"]; l.Size != 10 || len(l.files) != 1 || l.files[0] != filepath.Join(home, "logs", "old.log") {
		t.Errorf("logs must not include this run's log: %+v", l)
	}
	if s := got["pre-restore-20261006-130000"]; s.Kind != StoredSafety || s.Size != 40 || s.ModTime.Format("2006-01-02 15:04") != "2026-10-06 13:00" {
		t.Errorf("safety copy: %+v", s)
	}

	// A share from another menu window cannot be stopped from here.
	shares[0].Dedicated, shares[0].PID = false, 4242
	for _, f := range listStoredIn(home, shares, false) {
		if f.Name == "coolify-src-full-20261005-100000.cmb" && f.Busy == "" {
			t.Error("a backup shared from another window was not marked in use")
		}
	}
	// While a backup or restore runs, nothing can be deleted.
	for _, f := range listStoredIn(home, nil, true) {
		if f.Busy == "" {
			t.Errorf("%s not marked in use while a backup runs", f.Name)
		}
	}

	// Deleting removes the backup with its key, and nothing outside home.
	if err := removeInside(home, b.files); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(backup + ".key"); !os.IsNotExist(err) {
		t.Error("the key was not deleted with its backup")
	}
	for _, p := range []string{"/etc", filepath.Dir(home), home, filepath.Join(home, "..", "x")} {
		if err := removeInside(home, []string{p}); err == nil {
			t.Errorf("deleting %s was allowed", p)
		}
	}

	// A backup in a subfolder (--output): the emptied subfolder goes, backups/ stays.
	home2 := t.TempDir()
	deep := filepath.Join(home2, "backups", "v1", "deep", "b.cmb")
	writeFile(t, deep, 1)
	if err := removeInside(home2, []string{deep}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home2, "backups", "v1")); !os.IsNotExist(err) {
		t.Error("the emptied subfolder was kept")
	}
	if _, err := os.Stat(filepath.Join(home2, "backups")); err != nil {
		t.Error("backups/ itself was removed")
	}
}

func TestStoredHelpers(t *testing.T) {
	if got := stampTime("vol.cm-old-20261006-153000", time.Time{}); got.Format("15:04") != "15:30" {
		t.Error(got)
	}
	def := time.Unix(0, 0)
	if !stampTime("short", def).Equal(def) || !stampTime("replaced-notastamp-xxx", def).Equal(def) {
		t.Error("bad stamps must fall back")
	}
	if w := DeleteWarnings(StoredFile{Kind: StoredSafety, Name: "pre-restore-x"}); len(w) != 1 {
		t.Error(w)
	}
	if w := DeleteWarnings(StoredFile{Kind: StoredBackup, Shares: []ShareInfo{{}}}); len(w) != 1 {
		t.Error(w)
	}
	if KindLabel(StoredVolume) != "old volume" || KindLabel("x") != "leftover" {
		t.Error("labels")
	}
}

func TestDescribeProjects(t *testing.T) {
	r := func(project, name string) coolify.Resource {
		return coolify.Resource{Name: name, Project: project, ProjectUUID: project, Environment: "production", EnvironmentUUID: project + "-prod"}
	}
	got := describeProjects([]coolify.Resource{r("Shop", "shop-web"), r("Shop", "shop-db"), r("Blog", "blog")})
	if got != "Blog: blog, Shop (2 resources)" {
		t.Errorf("got %q", got)
	}
}
