package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hostDepsFixture builds a fake host: a policy script outside the resource
// folders, a buildx builder definition, and a temporary directory treated as
// a normal (non-runtime) location.
func hostDepsFixture(t *testing.T) (root, script string) {
	t.Helper()
	// Not under /tmp: that is runtime state the tool refuses to copy.
	root, err := os.MkdirTemp(".", ".hostdeps-test-")
	if err != nil {
		t.Fatal(err)
	}
	if root, err = filepath.Abs(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	oldRuntime, oldBuildx := runtimeRoots, buildxDir
	runtimeRoots = []string{"/proc", "/sys", "/dev", "/run", "/var/run"}
	buildxDir = filepath.Join(root, "buildx", "instances")
	t.Cleanup(func() { runtimeRoots, buildxDir = oldRuntime, oldBuildx })

	ops := filepath.Join(root, "ops")
	if err := os.MkdirAll(ops, 0o700); err != nil {
		t.Fatal(err)
	}
	script = filepath.Join(ops, "bounded-build.sh")
	// The script itself needs another host file (nested dependency).
	nested := filepath.Join(ops, "policy.json")
	if err := os.WriteFile(nested, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("#!/bin/sh\ncat "+nested+"\nexec \"$@\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(buildxDir, 0o700); err != nil {
		t.Fatal(err)
	}
	def := `{"Name":"bounded-v1","Driver":"docker-container","Nodes":[{"Name":"bounded-v10"}]}`
	if err := os.WriteFile(filepath.Join(buildxDir, "bounded-v1"), []byte(def), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, script
}

// Regression: a custom build command that runs a host script and a named
// buildx builder made the first deploy after a restore fail on the target.
func TestFindHostDepsCustomBuildCommand(t *testing.T) {
	_, script := hostDepsFixture(t)
	helper := "#!/usr/bin/env bash\ndocker run --rm --mount type=bind,src=" + script + ",dst=/ops.sh,readonly --entrypoint /bin/cat helper /ops.sh > \"$TMP\"\n" +
		"TMP=$(mktemp /tmp/build.XXXXXX)\n"
	texts := []depText{
		{owner: "app1", where: "docker compose custom build command",
			text: "bash .ops-build.sh docker compose --project-name app1 build --pull --builder bounded-v1"},
		{owner: "app1", where: "/data/coolify/applications/app1/.ops-build.sh", text: helper},
		{owner: "app1", where: "start command", text: "docker compose -f docker-compose.yaml -f /data/coolify/applications/app1/override.json up -d --builder not-on-this-host"},
	}
	deps, builders, paths := findHostDeps(texts, false)

	if len(builders) != 1 || builders[0].Name != "bounded-v1" || !json.Valid(builders[0].Def) {
		t.Fatalf("builders = %+v, want the saved bounded-v1 definition", builders)
	}
	got := map[string]HostDep{}
	for _, d := range deps {
		got[d.Kind+" "+d.Ref] = d
	}
	if d, ok := got["builder bounded-v1"]; !ok || !d.Saved || d.Owner != "app1" {
		t.Errorf("builder dependency missing or not saved: %+v", deps)
	}
	if d, ok := got["path "+script]; !ok || !d.Saved {
		t.Errorf("host script %s not recorded as saved dependency: %+v", script, deps)
	}
	nested := filepath.Join(filepath.Dir(script), "policy.json")
	if _, ok := got["path "+nested]; !ok {
		t.Errorf("file used by the host script not found: %+v", deps)
	}
	if _, ok := got["builder not-on-this-host"]; ok {
		t.Error("a builder that does not exist on the source must not become a dependency")
	}
	for _, d := range deps {
		if strings.HasPrefix(d.Ref, "/tmp") || strings.HasPrefix(d.Ref, "/data/coolify/applications") {
			t.Errorf("runtime or Coolify-managed path recorded as dependency: %+v", d)
		}
		if d.Ref == "/bin/cat" && d.Saved {
			t.Error("system files must never be copied")
		}
	}
	var pathList []string
	for _, p := range paths {
		pathList = append(pathList, p.Path)
	}
	if !contains(pathList, script) || !contains(pathList, nested) {
		t.Errorf("paths to back up = %v, want the script and its policy file", pathList)
	}
}

func TestFindHostDepsFullModeKeepsAllBuilders(t *testing.T) {
	hostDepsFixture(t)
	if err := os.WriteFile(filepath.Join(buildxDir, "other"), []byte(`{"Name":"other"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, builders, _ := findHostDeps(nil, true)
	if len(builders) != 2 {
		t.Fatalf("full mode saved %d builders, want 2", len(builders))
	}
}

func TestFindHostDepsSkipsBroadDirectories(t *testing.T) {
	deps, _, paths := findHostDeps([]depText{{owner: "a", where: "cmd", text: "volume /data:/data and /opt"}}, false)
	if len(deps) != 0 || len(paths) != 0 {
		t.Fatalf("broad directories must not become dependencies: %+v %+v", deps, paths)
	}
}

func TestRestoreBuildersAndCheck(t *testing.T) {
	root, script := hostDepsFixture(t)
	man := &Manifest{
		Builders: []BuilderEntry{{Name: "bounded-v1", Def: json.RawMessage(`{"Name":"changed"}`)}, {Name: "new-one", Def: json.RawMessage(`{"Name":"new-one"}`)}, {Name: "../evil", Def: json.RawMessage(`{}`)}},
		HostDeps: []HostDep{
			{Owner: "app1", Kind: depPath, Ref: script, Where: "build", Saved: true},
			{Owner: "app2", Kind: depPath, Ref: filepath.Join(root, "missing.sh"), Where: "build", Saved: true},
			{Owner: "skipped", Kind: depPath, Ref: filepath.Join(root, "gone.sh"), Where: "build", Saved: true},
		},
	}
	undo := newUndo()
	notes, err := restoreBuilders(man, undo)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(buildxDir, "bounded-v1")); strings.Contains(string(b), "changed") {
		t.Error("an existing builder must be kept")
	}
	if len(notes) != 1 {
		t.Errorf("notes = %v", notes)
	}
	if _, err := os.Stat(filepath.Join(buildxDir, "new-one")); err != nil {
		t.Errorf("missing builder was not installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(buildxDir), "evil")); err == nil {
		t.Error("builder name escaped the instances directory")
	}
	if len(undo.created) != 1 {
		t.Errorf("rollback must remove exactly the installed builder: %v", undo.created)
	}
	if errs := undo.rollback(); len(errs) > 0 {
		t.Fatal(errs)
	}
	if _, err := os.Stat(filepath.Join(buildxDir, "new-one")); err == nil {
		t.Error("rollback left the installed builder behind")
	}

	// Only path dependencies here: builders need docker.
	man.Builders = nil
	problems := checkHostDeps(context.Background(), man, func(s string) string { return s }, func(o string) bool { return o == "skipped" })
	if len(problems) != 1 || !strings.Contains(problems[0], "missing.sh") {
		t.Fatalf("problems = %v, want only the missing file of app2", problems)
	}
	// Renamed resources check the renamed path.
	renamed := checkHostDeps(context.Background(), &Manifest{HostDeps: man.HostDeps[:1]},
		func(s string) string { return s + ".copy" }, func(string) bool { return false })
	if len(renamed) != 1 {
		t.Errorf("rename not applied: %v", renamed)
	}
}

// Regression: a rollback removed a restored host file and builder but left the
// parent directories the restore had created.
func TestRollbackRemovesCreatedParents(t *testing.T) {
	root, _ := hostDepsFixture(t)
	buildxDir = filepath.Join(root, "home", ".docker", "buildx", "instances")
	undo := newUndo()
	if _, err := restoreBuilders(&Manifest{Builders: []BuilderEntry{{Name: "b1", Def: json.RawMessage(`{}`)}}}, undo); err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "home"); len(undo.created) != 1 || undo.created[0] != want {
		t.Fatalf("created = %v, want %s", undo.created, want)
	}
	if errs := undo.rollback(); len(errs) > 0 {
		t.Fatal(errs)
	}
	if _, err := os.Stat(filepath.Join(root, "home")); err == nil {
		t.Error("parent directories created by the restore were left behind")
	}
	if got := topMissing(filepath.Join(root, "ops", "new", "file.sh")); got != filepath.Join(root, "ops", "new") {
		t.Errorf("topMissing = %s", got)
	}
}

func TestHostDepBlockers(t *testing.T) {
	root, script := hostDepsFixture(t)
	man := &Manifest{HostDeps: []HostDep{
		{Owner: "a", Kind: depPath, Ref: script, Where: "cmd"},                      // present
		{Owner: "a", Kind: depPath, Ref: filepath.Join(root, "tool"), Where: "cmd"}, // not copied and missing
		{Owner: "a", Kind: depPath, Ref: filepath.Join(root, "x"), Saved: true},     // restored later
		{Owner: "a", Kind: depBuilder, Ref: "b", Saved: true},                       // builders are checked later
	}}
	b := hostDepBlockers(context.Background(), man)
	if len(b) != 1 || !strings.Contains(b[0], filepath.Join(root, "tool")) {
		t.Fatalf("blockers = %v", b)
	}
}

// Backups made before host dependencies and runtime were recorded still load.
func TestOldManifestCompatible(t *testing.T) {
	var m Manifest
	old := `{"format":"github.com/mtalavi/coolify-mirror/1","tool_version":"1.2.0","mode":"selective","resources":[],"volumes":[],"paths":[],"images":[],"options":{}}`
	if err := json.Unmarshal([]byte(old), &m); err != nil {
		t.Fatal(err)
	}
	if m.HostDeps != nil || m.Builders != nil || m.Runtime != nil {
		t.Fatal("unexpected defaults")
	}
	if b := hostDepBlockers(context.Background(), &m); len(b) != 0 {
		t.Fatalf("old backup blocked: %v", b)
	}
	if p := checkHostDeps(context.Background(), &m, func(s string) string { return s }, func(string) bool { return false }); len(p) != 0 {
		t.Fatal(p)
	}
}

func TestDeployedImages(t *testing.T) {
	refs := []string{
		"abc123:deadbeef", "abc123:oldcommit", "abc123_web:deadbeef", "abc123_worker:deadbeef",
		"abc1234:deadbeef", "postgres:16", "other_web:deadbeef", "abc123_web:latest",
	}
	got := deployedImages(refs, "abc123", "deadbeef")
	want := []string{"abc123:deadbeef", "abc123_web:deadbeef", "abc123_worker:deadbeef"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
