// Command coolify-mirror backs up Coolify resources (or a whole Coolify
// server) into one encrypted file, shares it over HTTP, and restores it on
// another Coolify server as an exact mirror.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/mtalavi/coolify-mirror/internal/archive"
	"github.com/mtalavi/coolify-mirror/internal/coolify"
	"github.com/mtalavi/coolify-mirror/internal/dbx"
	"github.com/mtalavi/coolify-mirror/internal/docker"
	"github.com/mtalavi/coolify-mirror/internal/engine"
	"github.com/mtalavi/coolify-mirror/internal/run"
	"github.com/mtalavi/coolify-mirror/internal/transfer"
	"github.com/mtalavi/coolify-mirror/internal/ui"

	"golang.org/x/term"
)

func main() {
	os.Exit(realMain(os.Args[1:]))
}

func realMain(args []string) int {
	// SIGHUP (the SSH connection dropped) cancels like Ctrl+C, so paused
	// containers are resumed and a failed restore is rolled back.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()

	cmd := ""
	if len(args) > 0 {
		cmd = args[0]
		args = args[1:]
	}
	if cmd != "serve-internal" {
		openLog(cmd)
	}
	var err error
	switch cmd {
	case "":
		if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
			usage()
			return 2
		}
		err = ui.Run(ctx)
	case "list", "ls":
		err = cmdList(ctx, args)
	case "backup":
		err = cmdBackup(ctx, args)
	case "restore":
		err = cmdRestore(ctx, args)
	case "serve", "share":
		err = cmdServe(ctx, args)
	case "serve-internal":
		err = cmdServeInternal(ctx, args)
	case "start-all":
		err = cmdStartAll(ctx)
	case "version", "--version", "-v":
		fmt.Println("coolify-mirror", engine.Version)
		return 0
	case "help", "--help", "-h":
		usage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", cmd)
		usage()
		return 2
	}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			fmt.Fprintln(os.Stderr, "\ncancelled")
			return 130
		}
		fmt.Fprintln(os.Stderr, "\nERROR:", err)
		if logPath != "" {
			fmt.Fprintln(os.Stderr, "details:", logPath)
		}
		return 1
	}
	return 0
}

func usage() {
	fmt.Print(`coolify-mirror ` + engine.Version + ` - full backup & restore / mirror for Coolify

Run it as root on a Coolify server:

  ./coolify-mirror                      interactive menu (arrow keys)

  ./coolify-mirror list                 show resources and their domains
  ./coolify-mirror backup [flags]       create a backup
        --full                          whole Coolify server (instead of selected resources)
        --domain a.com,b.com            back up the resources serving these domains
        --uuid x,y                      back up these resources (uuid)
        --all                           every resource on this server
        --no-deps                       do not add databases the resources depend on
        --consistency pause|stop|live   how running containers are handled (default pause)
        --images apps|all|none          which docker images to include (default apps)
        --include-backups               full mode: include /data/coolify/backups
        --output DIR                    where to write the file (default /data/coolify-mirror/backups)
        --serve                         share the file over HTTPS when done
  ./coolify-mirror serve FILE [flags]   share an existing backup file
        --mode direct|proxy             direct HTTPS port (default 8123) or through Coolify's proxy on port 443
        --port 8123  --host IP  --open-firewall  --detach  --ttl 24h
  ./coolify-mirror restore LINK|FILE    download, verify and restore a backup
        --key KEY                       if the link has no #key=... part
        --yes                           do not ask for confirmation
        --on-conflict copy|skip         selective restore: resource already exists here
        --team ID                       selective restore: target team (default 0)
        --no-start                      do not start/deploy restored resources
        --verify-redeploy               after starting, rebuild every application Coolify builds
                                        (same commit, no cache) to prove the next deploy works here
        --keep-download                 keep the downloaded file after a successful restore
        --set-domain OLD=NEW            replace a restored domain (repeatable, e.g. a.com=b.com);
                                        without --yes every domain is asked for at the end
  ./coolify-mirror start-all            ask Coolify to start every resource on this server
                                        that has no running container (recovery helper)
`)
}

var logPath string

func openLog(cmd string) {
	if os.Geteuid() != 0 {
		return
	}
	if err := os.MkdirAll(engine.LogsDir, 0o700); err != nil {
		return
	}
	if cmd == "" {
		cmd = "menu"
	}
	logPath = filepath.Join(engine.LogsDir, time.Now().Format("20060102-150405")+"-"+cmd+".log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		logPath = ""
		return
	}
	run.SetLog(f)
	archive.SetDebugLog(func(s string) { run.Logf("%s", s) })
	ui.LogPath = logPath
	run.Logf("coolify-mirror %s %s", engine.Version, cmd)
}

func detect(ctx context.Context) (*coolify.Instance, error) {
	in, err := coolify.Detect(ctx)
	if err != nil {
		return nil, err
	}
	if msg := engine.RecoverPaused(ctx); msg != "" {
		fmt.Println("note:", msg)
	}
	return in, nil
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// --- list ------------------------------------------------------------------------

func cmdList(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	in, err := detect(ctx)
	if err != nil {
		return err
	}
	rs, err := in.ListResources(ctx)
	if err != nil {
		return err
	}
	if *asJSON {
		b, _ := json.MarshalIndent(rs, "", "  ")
		fmt.Println(string(b))
		return nil
	}
	fmt.Printf("Coolify %s on %s - %d resource(s)\n\n", in.Version, in.Hostname, len(rs))
	for _, r := range rs {
		doms := "-"
		if len(r.Domains) > 0 {
			hs := make([]string, len(r.Domains))
			for i, d := range r.Domains {
				hs[i] = coolify.Host(d)
			}
			doms = strings.Join(hs, ", ")
		}
		where := ""
		if !r.Local() {
			where = "  [remote: " + r.ServerName + "]"
		}
		fmt.Printf("%-40s %-12s %-28s %s/%s  %s  %s%s\n", doms, r.Label(), r.Name, r.Project, r.Environment, r.Status, r.UUID, where)
	}
	return nil
}

// --- backup ----------------------------------------------------------------------

func cmdBackup(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	full := fs.Bool("full", false, "")
	domains := fs.String("domain", "", "")
	uuids := fs.String("uuid", "", "")
	all := fs.Bool("all", false, "")
	noDeps := fs.Bool("no-deps", false, "")
	consistency := fs.String("consistency", engine.ConsistencyPause, "")
	images := fs.String("images", engine.ImagesApps, "")
	includeBackups := fs.Bool("include-backups", false, "")
	output := fs.String("output", engine.BackupsDir, "")
	serve := fs.Bool("serve", false, "")
	mode := fs.String("mode", engine.ShareDirect, "")
	port := fs.Int("port", engine.DefaultPort, "")
	host := fs.String("host", "", "")
	openFW := fs.Bool("open-firewall", false, "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	in, err := detect(ctx)
	if err != nil {
		return err
	}
	req := engine.BackupRequest{Mode: engine.ModeSelective, OutputDir: *output, Consistency: *consistency,
		Images: *images, IncludeBackups: *includeBackups}
	if *full {
		req.Mode = engine.ModeFull
	} else {
		rs, err := in.ListResources(ctx)
		if err != nil {
			return err
		}
		sel, err := selectResources(rs, *all, splitList(*domains), splitList(*uuids))
		if err != nil {
			return err
		}
		if !*noDeps {
			deps, err := engine.ResolveDependencies(ctx, in, sel, rs)
			if err != nil {
				return err
			}
			for _, d := range deps {
				fmt.Printf("+ adding %s %q - %s\n", d.Resource.Label(), d.Resource.Name, d.Reason)
				sel = append(sel, d.Resource)
			}
		}
		req.Resources = sel
	}
	pr := engine.NewProgress("Backup")
	stopPrint := printProgress(pr)
	res, err := engine.Backup(ctx, in, req, pr)
	stopPrint()
	if err != nil {
		return err
	}
	fmt.Printf("\nBackup complete in %s\n  file: %s\n  size: %s (data: %s)\n  key:  %s\n",
		engine.HumanDuration(res.Duration), res.Path, engine.HumanBytes(res.Size), engine.HumanBytes(res.RawBytes), res.Key)
	if !*serve {
		fmt.Printf("\nShare it with:  %s serve %s\n", os.Args[0], res.Path)
		return nil
	}
	return serveLoop(ctx, in, res.Path, res.Key, engine.ShareOptions{Mode: *mode, Port: *port, Host: *host, OpenFirewall: *openFW}, 0)
}

func selectResources(rs []coolify.Resource, all bool, domains, uuids []string) ([]coolify.Resource, error) {
	var sel []coolify.Resource
	seen := map[string]bool{}
	add := func(r coolify.Resource) {
		if !seen[r.UUID] {
			seen[r.UUID] = true
			sel = append(sel, r)
		}
	}
	for _, r := range rs {
		if all && r.Local() {
			add(r)
		}
	}
	for _, d := range domains {
		found := false
		for _, r := range rs {
			for _, rd := range r.Domains {
				if strings.EqualFold(coolify.Host(rd), coolify.Host(d)) || strings.EqualFold(strings.Split(coolify.Host(rd), "/")[0], d) {
					add(r)
					found = true
				}
			}
		}
		if !found {
			return nil, fmt.Errorf("no resource serves domain %q", d)
		}
	}
	for _, u := range uuids {
		found := false
		for _, r := range rs {
			if r.UUID == u {
				add(r)
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("no resource with uuid %q", u)
		}
	}
	if len(sel) == 0 {
		return nil, errors.New("nothing selected: use --domain, --uuid, --all or --full")
	}
	return sel, nil
}

// printProgress prints step changes and a percentage line (non-interactive mode).
func printProgress(pr *engine.Progress) func() {
	done := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		states := map[int]engine.StepState{}
		lastPct := time.Now()
		tick := time.NewTicker(500 * time.Millisecond)
		defer tick.Stop()
		emit := func(v engine.View) {
			for i, s := range v.Steps {
				prev, seen := states[i]
				if seen && prev == s.State {
					continue
				}
				states[i] = s.State
				switch s.State {
				case engine.Running:
					fmt.Printf("  … %s %s\n", s.Title, s.Detail)
				case engine.Done:
					fmt.Printf("  ✓ %s  %s\n", s.Title, s.Note)
				case engine.Failed:
					fmt.Printf("  ✗ %s  %v\n", s.Title, s.Err)
				case engine.Skipped:
					fmt.Printf("  - %s  %s\n", s.Title, s.Note)
				}
			}
		}
		for {
			v := pr.Snapshot()
			emit(v)
			if time.Since(lastPct) > 5*time.Second && !v.Finished && v.TotalBytes > 0 {
				lastPct = time.Now()
				fmt.Printf("  [%3.0f%%] %s / %s  (%s)\n", v.Fraction()*100, engine.HumanBytes(v.DoneBytes), engine.HumanBytes(v.TotalBytes), engine.HumanDuration(v.Elapsed))
			}
			select {
			case <-done:
				v := pr.Snapshot()
				emit(v)
				for _, w := range v.Warnings {
					fmt.Println("  ! " + w)
				}
				return
			case <-tick.C:
			}
		}
	}()
	return func() {
		close(done)
		<-finished
	}
}

// --- serve -----------------------------------------------------------------------

func cmdServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	key := fs.String("key", "", "")
	mode := fs.String("mode", engine.ShareDirect, "")
	port := fs.Int("port", engine.DefaultPort, "")
	host := fs.String("host", "", "")
	openFW := fs.Bool("open-firewall", false, "")
	detach := fs.Bool("detach", false, "")
	ttl := fs.Duration("ttl", 24*time.Hour, "")
	token := fs.String("token", "", "")
	_ = fs.Bool("foreground", false, "")
	file, rest := firstArg(args)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if file == "" {
		return errors.New("usage: coolify-mirror serve FILE")
	}
	if *key == "" {
		*key = os.Getenv(engine.KeyEnv)
	}
	if *key == "" {
		if b, err := os.ReadFile(file + ".key"); err == nil {
			*key = strings.TrimSpace(string(b))
		}
	}
	if *key == "" {
		return errors.New("no key: pass --key (it was printed when the backup was created)")
	}
	opt := engine.ShareOptions{Mode: *mode, Port: *port, Host: *host, OpenFirewall: *openFW, Token: *token}
	if *detach {
		pid, logf, err := engine.Detach(file, *key, opt, *ttl)
		if err != nil {
			return err
		}
		fmt.Printf("sharing in the background (pid %d, stops after %s); the link is in %s\n", pid, *ttl, logf)
		return nil
	}
	in, err := detect(ctx)
	if err != nil {
		return err
	}
	return serveLoop(ctx, in, file, *key, opt, *ttl)
}

func firstArg(args []string) (string, []string) {
	for i, a := range args {
		if !strings.HasPrefix(a, "-") {
			return a, append(append([]string{}, args[:i]...), args[i+1:]...)
		}
	}
	return "", args
}

func serveLoop(ctx context.Context, in *coolify.Instance, file, key string, opt engine.ShareOptions, ttl time.Duration) error {
	if opt.Mode == engine.ShareDirect && engine.UFWBlocks(ctx, opt.Port) && !opt.OpenFirewall {
		fmt.Printf("note: ufw is active and port %d is not open; add --open-firewall or run: ufw allow %d/tcp\n", opt.Port, opt.Port)
	}
	sh, err := engine.StartShare(ctx, in, file, key, opt)
	if err != nil {
		return err
	}
	defer sh.Stop()
	fmt.Printf("\nSharing %s (%s mode)\n\n  Restore link (paste it on the other server):\n    %s\n\n", filepath.Base(file), sh.Mode, sh.Link)
	fmt.Printf("  On the other server, download this tool (checksum verified) and open its menu:\n    %s\n  then choose \"Restore a backup\" and paste the link.\n\n", sh.ToolCommand())
	fmt.Println("Waiting for downloads - press Ctrl+C to stop sharing.")
	var expire <-chan time.Time
	if ttl > 0 {
		expire = time.After(ttl)
	}
	last := time.Time{}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-expire:
			fmt.Println("share expired")
			return nil
		case e := <-sh.Events:
			if e.Complete {
				fmt.Printf("  ✓ %s downloaded the complete backup (%s)\n", e.Remote, engine.HumanBytes(e.Total))
			} else if time.Since(last) > 2*time.Second {
				last = time.Now()
				fmt.Printf("  … %s: %s / %s\n", e.Remote, engine.HumanBytes(e.Sent), engine.HumanBytes(e.Total))
			}
		}
	}
}

func cmdServeInternal(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve-internal", flag.ContinueOnError)
	file := fs.String("file", "", "")
	binary := fs.String("binary", "", "")
	token := fs.String("token", "", "")
	listen := fs.String("listen", ":8080", "")
	ttl := fs.Duration("ttl", 24*time.Hour, "")
	tlsDir := fs.String("tls-dir", "", "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	certPEM, err := os.ReadFile(filepath.Join(*tlsDir, "cert.pem"))
	if err != nil {
		return err
	}
	keyPEM, err := os.ReadFile(filepath.Join(*tlsDir, "key.pem"))
	if err != nil {
		return err
	}
	cert, err := transfer.LoadCert(certPEM, keyPEM)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	srv := &transfer.Server{File: *file, Token: *token, Binary: *binary, Cert: cert, OnEvent: func(e transfer.Event) { _ = enc.Encode(e) }}
	if _, err := srv.Listen(*listen); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
	case <-time.After(*ttl): // never share forever, even if the parent is gone
	}
	srv.Close()
	return nil
}

// --- restore ---------------------------------------------------------------------

func cmdRestore(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	key := fs.String("key", "", "")
	yes := fs.Bool("yes", false, "")
	onConflict := fs.String("on-conflict", "copy", "")
	team := fs.Int64("team", 0, "")
	noStart := fs.Bool("no-start", false, "")
	verifyRedeploy := fs.Bool("verify-redeploy", false, "")
	keepDownload := fs.Bool("keep-download", false, "")
	var setDomains multiFlag
	fs.Var(&setDomains, "set-domain", "")
	src, rest := firstArg(args)
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if src == "" {
		return errors.New("usage: coolify-mirror restore LINK|FILE")
	}
	if *key == "" {
		*key = os.Getenv(engine.KeyEnv)
	}
	domainMap, err := parseSetDomains(setDomains)
	if err != nil {
		return err
	}
	in, err := detect(ctx)
	if err != nil {
		return err
	}
	pr := engine.NewProgress("Download")
	stop := printProgress(pr)
	f, err := engine.Fetch(ctx, src, *key, pr)
	stop()
	if err != nil {
		return err
	}
	m := f.Manifest
	fmt.Printf("\nBackup from %s (%s, Coolify %s) taken %s - %s, %d resource(s), %s of data\n",
		m.Source.Hostname, m.Source.IPv4, m.Source.CoolifyVersion, m.CreatedAt.Local().Format("2006-01-02 15:04"),
		m.Mode, len(m.Resources), engine.HumanBytes(m.TotalBytes))
	for _, r := range m.Resources {
		fmt.Printf("  - %-12s %-28s %s\n", r.Label(), r.Name, strings.Join(hosts(r.Domains), ", "))
	}

	var report *engine.RestoreReport
	shown := map[string]bool{}
	if m.Mode == engine.ModeFull {
		chk, err := engine.CheckFull(ctx, in, f)
		if err != nil {
			return err
		}
		if len(chk.Blockers) > 0 {
			return errors.New("restore not allowed:\n  - " + strings.Join(chk.Blockers, "\n  - "))
		}
		for _, w := range chk.CoolifyWarnings {
			fmt.Println("Coolify:", w)
		}
		if chk.ArchProblem != "" {
			fmt.Println("WARNING:", chk.ArchProblem)
		}
		if len(chk.ExistingVolumes) > 0 {
			fmt.Printf("Volumes that already exist here get the backup's data (their current data is kept in *.cm-old-* volumes): %s\n", strings.Join(chk.ExistingVolumes, ", "))
		}
		fmt.Println("\nFULL RESTORE: this empty Coolify becomes an exact copy of the source (users, settings, keys, every resource).")
		if len(chk.RemoteServers) > 0 {
			fmt.Printf("The restored Coolify will also manage these remote servers: %s - stop the old Coolify to avoid two controllers.\n", strings.Join(chk.RemoteServers, ", "))
		}
		if !*yes && !confirm("Type 'yes' to replace this Coolify") {
			return errors.New("aborted")
		}
		pr := engine.NewProgress("Restore")
		stop := printProgress(pr)
		report, err = engine.ApplyFull(ctx, in, f, pr)
		stop()
		if err != nil {
			return err
		}
		// Coolify now runs with the source APP_KEY.
		if in, err = coolify.Detect(ctx); err != nil {
			return err
		}
	} else {
		if b := engine.PreflightSelective(ctx, in, f); len(b) > 0 {
			return errors.New("restore not allowed:\n  - " + strings.Join(b, "\n  - "))
		}
		sr, err := engine.PrepareSelective(ctx, in, f, *team)
		if err != nil {
			return err
		}
		decisions := map[string]dbx.Decision{}
		for _, c := range sr.Conflicts {
			d := dbx.NewCopy
			if *onConflict == "skip" {
				d = dbx.Skip
			}
			decisions[c.UUID] = d
			fmt.Printf("  ! %s %q already exists here - %s\n", c.Label(), c.Name, map[dbx.Decision]string{dbx.NewCopy: "restoring as a copy", dbx.Skip: "skipped"}[d])
		}
		if err := sr.Build(ctx, decisions); err != nil {
			return err
		}
		for _, n := range sr.Plan.Notes {
			shown[n] = true
			fmt.Println("  note:", n)
		}
		for _, v := range sr.ExistingVolumes {
			fmt.Println("  ! volume exists (gets the backup's data; its current data is kept aside):", v)
		}
		for _, h := range sr.HostPaths {
			switch {
			case h.Same:
				fmt.Println("  host file already identical here, kept:", h.Path)
			case h.Shared:
				fmt.Println("  ! host folder shared with the original resource, not overwritten:", h.Path)
			case h.Exists:
				fmt.Println("  ! host folder exists (current content moved aside, then restored):", h.Path)
			default:
				fmt.Println("  host folder restored:", h.Path)
			}
		}
		for _, d := range sr.DomainClashes {
			fmt.Println("  ! domain clash:", d)
		}
		for _, w := range sr.CoolifyWarnings {
			fmt.Println("  coolify:", w)
		}
		if !*yes && !confirm("Restore these resources into this Coolify?") {
			return errors.New("aborted")
		}
		pr := engine.NewProgress("Restore")
		stop := printProgress(pr)
		report, err = sr.Apply(ctx, pr)
		stop()
		if err != nil {
			return err
		}
	}
	for _, n := range report.Notes {
		if !shown[n] {
			shown[n] = true
			fmt.Println("  note:", n)
		}
	}
	if err := cliDomains(ctx, in, report, domainMap, !*yes); err != nil {
		// The resources are restored; a clashing one simply stays stopped.
		fmt.Println("  ! domains were not changed:", err, "- change them in Coolify")
	}
	for _, p := range report.Problems {
		fmt.Println("  ✗", p)
	}
	if *noStart {
		if len(report.Problems) > 0 {
			return fmt.Errorf("restored, but %d dependency problem(s) remain - the next deploy would fail", len(report.Problems))
		}
		fmt.Println("\nRestored. Start the resources from the Coolify dashboard.")
		return nil
	}
	fmt.Println("\nStarting resources through Coolify…")
	pr = engine.NewProgress("Start")
	stop = printProgress(pr)
	results, err := engine.StartResources(ctx, in, report.Resources, pr)
	stop()
	if err != nil {
		return err
	}
	failed, held, stopped := 0, 0, 0
	for _, r := range results {
		mark := "✓"
		switch {
		case !r.OK:
			mark, failed = "✗", failed+1
		case r.Resource.Hold != "":
			mark, held = "!", held+1
		case r.Stopped:
			mark, stopped = "-", stopped+1
		}
		fmt.Printf("  %s %-28s %s\n", mark, r.Resource.Name, r.Message)
	}
	if v := engine.Verdict(failed, held, report.Problems); v != "" {
		return errors.New(v)
	}
	redeployed := false
	if *verifyRedeploy {
		fmt.Println("\nRebuilding the applications on this server to prove the next deploy works…")
		pr = engine.NewProgress("Redeploy check")
		stop = printProgress(pr)
		checks, err := engine.VerifyRedeploy(ctx, in, report.Resources, pr)
		stop()
		if err != nil {
			return err
		}
		bad := 0
		for _, r := range checks {
			mark := "✓"
			if !r.OK {
				mark, bad = "✗", bad+1
			}
			fmt.Printf("  %s %-28s %s\n", mark, r.Resource.Name, r.Message)
		}
		if bad > 0 {
			return fmt.Errorf("Restored and running, but NOT redeployable: %d application(s) failed to rebuild here - a dependency of their build is missing on this server (see the deployment log in Coolify)", bad)
		}
		redeployed = len(checks) > 0
	}
	if f.Downloaded && !*keepDownload {
		if err := os.Remove(f.Path); err == nil {
			fmt.Println("  note: the downloaded backup copy was deleted (the source server still has it)")
		}
	}
	if stopped > 0 {
		fmt.Printf("\nDone. %d resource(s) are stopped, as they were on the source; everything else is running and verified.\n", stopped)
	} else {
		fmt.Println("\nSUCCESS. Every resource is running, stable and reachable through the proxy.")
	}
	if redeployed {
		fmt.Println("Every application was also rebuilt here from its source and runs: later deploys work on this server.")
	}
	fmt.Println("Scheduled tasks/backups run here now too - disable them on the old server once you switch.")
	return nil
}

// cmdStartAll starts, through Coolify, every local resource without a running
// container (for example after an interrupted restore).
func cmdStartAll(ctx context.Context) error {
	in, err := detect(ctx)
	if err != nil {
		return err
	}
	rs, err := in.ListResources(ctx)
	if err != nil {
		return err
	}
	var todo []dbx.PlannedResource
	for _, r := range rs {
		if !r.Local() {
			continue
		}
		cs, err := docker.Containers(ctx, "label=com.docker.compose.project="+r.UUID, "status=running")
		if err == nil && len(cs) > 0 {
			continue
		}
		pr := dbx.PlannedResource{Resource: r, SourceUUID: r.UUID, WasRunning: true}
		if r.Table == "applications" {
			// The commit of its last successful deployment: its images are
			// tagged with it (a restored application can start from them).
			pr.Commit, _ = in.Scalar(ctx, "SELECT q.commit FROM application_deployment_queues q JOIN applications a ON a.id::text = q.application_id"+
				" WHERE a.uuid = "+coolify.SQLString(r.UUID)+" AND q.status = 'finished' AND q.pull_request_id = 0 ORDER BY q.id DESC LIMIT 1")
		}
		todo = append(todo, pr)
		fmt.Printf("  - %s (%s)\n", r.Name, r.Label())
	}
	if len(todo) == 0 {
		fmt.Println("Every resource on this server already has running containers.")
		return nil
	}
	pr := engine.NewProgress("Start")
	stop := printProgress(pr)
	results, err := engine.StartResources(ctx, in, todo, pr)
	stop()
	if err != nil {
		return err
	}
	failed := 0
	for _, r := range results {
		if !r.OK {
			failed++
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d resource(s) did not start - see the Coolify dashboard", failed)
	}
	return nil
}

func hosts(ds []string) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = coolify.Host(d)
	}
	return out
}

func confirm(q string) bool {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return false
	}
	fmt.Printf("%s [yes/No]: ", q)
	var s string
	fmt.Scanln(&s)
	s = strings.ToLower(strings.TrimSpace(s))
	return s == "y" || s == "yes"
}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

// parseSetDomains checks --set-domain OLD=NEW values before anything is
// restored. NEW "-" removes the domain.
func parseSetDomains(set []string) (map[string]string, error) {
	repl := map[string]string{}
	for _, kv := range set {
		old, nw, ok := strings.Cut(kv, "=")
		old, nw = strings.TrimSpace(old), strings.TrimSpace(nw)
		if !ok || old == "" || nw == "" {
			return nil, fmt.Errorf("--set-domain %q: use OLD=NEW (NEW - removes the domain)", kv)
		}
		if nw != "-" {
			if strings.Contains(nw, ",") {
				return nil, fmt.Errorf("--set-domain %q: one domain per flag", kv)
			}
			if _, err := engine.NormalizeDomains(nw); err != nil {
				return nil, fmt.Errorf("--set-domain %q: %w", kv, err)
			}
		}
		repl[coolify.Host(old)] = nw
	}
	return repl, nil
}

// cliDomains applies --set-domain OLD=NEW and, when interactive, asks for
// every restored domain as the last step before starting.
func cliDomains(ctx context.Context, in *coolify.Instance, rep *engine.RestoreReport, repl map[string]string, ask bool) error {
	fields, err := engine.DomainFields(ctx, in, rep.Resources)
	if err != nil || len(fields) == 0 {
		return err
	}
	ask = ask && term.IsTerminal(int(os.Stdin.Fd()))
	if ask {
		fmt.Println("\nDomains (last step before starting). Enter = keep, '-' = no domain, several: comma separated.")
	}
	rd := bufio.NewReader(os.Stdin)
	for i := range fields {
		f := &fields[i]
		var parts []string
		for _, d := range coolify.SplitDomains(f.Value) {
			scheme := "https://"
			if strings.HasPrefix(d, "http://") {
				scheme = "http://"
			}
			if nw, ok := repl[coolify.Host(d)]; ok {
				if nw == "-" {
					continue
				}
				if !strings.Contains(nw, "://") {
					nw = scheme + nw
				}
				d = nw
			}
			parts = append(parts, d)
		}
		f.Value = strings.Join(parts, ",")
		for ask {
			fmt.Printf("  %s [%s]: ", f.Label, f.Value)
			line, _ := rd.ReadString('\n')
			line = strings.TrimSpace(line)
			if line == "" {
				break
			}
			if line == "-" {
				f.Value = ""
				break
			}
			v, err := engine.NormalizeDomains(line)
			if err != nil {
				fmt.Println("   ", err)
				continue
			}
			f.Value = v
			break
		}
		if v, err := engine.NormalizeDomains(f.Value); err != nil {
			return fmt.Errorf("%s: %w", f.Label, err)
		} else {
			f.Value = v
		}
	}
	return engine.ApplyDomains(ctx, in, rep.Resources, fields)
}
