package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mtalavi/coolify-mirror/internal/coolify"
	"github.com/mtalavi/coolify-mirror/internal/engine"
)

// offsiteRun is `backup … --to DEST`: upload the new backup, check the copy,
// keep keepRemote backups there and keepLocal here, record and report.
type offsiteRun struct {
	dest       string
	keepRemote int
	keepLocal  int
	notifyOn   string
	name       string
	started    time.Time
}

// precheck fails before a long backup when the result could not leave the
// server or could not be opened after losing it.
func (o *offsiteRun) precheck(ctx context.Context) error {
	cfg, err := engine.LoadConfig()
	if err != nil {
		return err
	}
	if len(cfg.RecoveryKeys) == 0 {
		return errors.New("no recovery key yet: without it a backup kept off this server cannot be opened after losing the server - create one first: sudo coolify-mirror recovery-key create")
	}
	if engine.IsRemote(o.dest) {
		if !engine.RcloneInstalled() {
			return errors.New("rclone is not installed - install it (curl https://rclone.org/install.sh | sudo bash) and add the destination with: rclone config")
		}
		if _, err := engine.ListBackupsAt(ctx, o.dest); err != nil && !strings.Contains(err.Error(), "directory not found") {
			return fmt.Errorf("the destination %s does not work: %w", o.dest, err)
		}
		return nil
	}
	if !filepath.IsAbs(o.dest) {
		return fmt.Errorf("--to %q: give a folder (/mnt/backups) or an rclone remote (name:folder)", o.dest)
	}
	return os.MkdirAll(o.dest, 0o700)
}

// fail records and reports a failed run (o may be nil: a plain backup).
func (o *offsiteRun) fail(ctx context.Context, in *coolify.Instance, err error) error {
	if o == nil || err == nil {
		return err
	}
	r := engine.OffsiteResult{Name: o.name, OK: false, Error: err.Error(), Dest: o.dest, Started: o.started,
		Duration: engine.HumanDuration(time.Since(o.started))}
	engine.SaveResult(r)
	o.notify(ctx, in, r)
	return err
}

func (o *offsiteRun) finish(ctx context.Context, in *coolify.Instance, res *engine.BackupResult) error {
	fmt.Printf("\nBackup ready: %s (%s), Coolify %s\n", filepath.Base(res.Path), engine.HumanBytes(res.Size), res.Manifest.Source.CoolifyVersion)
	pr := engine.NewProgress("Upload")
	st := pr.Add("Upload to "+o.dest+" and check the copy", res.Size)
	stop := printProgress(pr)
	st.Begin("")
	err := engine.Upload(ctx, res.Path, o.dest, func(done, total int64) { st.SetDone(done) })
	if err != nil {
		st.Fail(err)
		pr.End(err)
		stop()
		return o.fail(ctx, in, fmt.Errorf("%w (the backup stays here: %s)", err, res.Path))
	}
	st.Finish("checked")
	pr.End(nil)
	stop()
	r := engine.OffsiteResult{Name: o.name, OK: true, File: filepath.Base(res.Path), Dest: o.dest, Size: res.Size, Started: o.started}
	prefix := engine.BackupPrefix(res.Manifest.Source.Hostname, res.Manifest.Mode)
	if del, err := engine.PruneBackupsAt(ctx, o.dest, prefix, o.keepRemote); err != nil {
		fmt.Println("  ! cleaning older backups at the destination:", err)
	} else {
		r.Deleted = del
	}
	removed, err := engine.AfterUpload(res.Path, prefix, o.keepLocal)
	if err != nil {
		fmt.Println("  ! freeing the space here:", err)
	}
	if o.keepLocal > 0 {
		r.Kept = fmt.Sprintf("%d newest here", o.keepLocal)
	} else {
		r.Kept = "nothing here (uploaded and checked)"
	}
	r.Duration = engine.HumanDuration(time.Since(o.started))
	engine.SaveResult(r)
	fmt.Printf("Uploaded and checked: %s\n", engine.HumanBytes(res.Size))
	if len(r.Deleted) > 0 {
		fmt.Printf("Older backups removed at the destination (keeping %d): %s\n", o.keepRemote, strings.Join(r.Deleted, ", "))
	}
	if len(removed) > 0 {
		fmt.Printf("Removed here after the checked upload: %s\n", strings.Join(removed, ", "))
	}
	fmt.Println("It opens with the recovery key on any server: sudo coolify-mirror restore " + o.dest + " --recovery-key …")
	o.notify(ctx, in, r)
	return nil
}

func (o *offsiteRun) notify(ctx context.Context, in *coolify.Instance, r engine.OffsiteResult) {
	cfg, err := engine.LoadConfig()
	if err != nil {
		return
	}
	n := cfg.Notify
	if o.notifyOn != "" {
		n.On = o.notifyOn
	}
	if len(n.Channels()) == 0 || !n.Wants(r.OK) {
		return
	}
	host, _ := os.Hostname()
	subject := "coolify-mirror backup OK - " + host
	body := fmt.Sprintf("Backup of %s uploaded and checked.\nFile: %s (%s)\nTo: %s\nTook: %s\n", host, r.File, engine.HumanBytes(r.Size), r.Dest, r.Duration)
	if !r.OK {
		subject = "coolify-mirror backup FAILED - " + host
		body = fmt.Sprintf("The backup of %s to %s failed after %s:\n\n%s\n\nLog: %s\n", host, r.Dest, r.Duration, r.Error, logPath)
	}
	_, errs := engine.Notify(ctx, in, n, subject, body)
	for _, e := range errs {
		fmt.Println("  ! notification:", e)
	}
}

// ---------------------------------------------------------------------------

func cmdRecoveryKey(ctx context.Context, args []string) error {
	sub := "show"
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("recovery-key", flag.ContinueOnError)
	email := fs.String("email", "", "")
	telegram := fs.String("telegram", "", "")
	telegramToken := fs.String("telegram-token", "", "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	switch sub {
	case "show", "status":
		cfg, err := engine.LoadConfig()
		if err != nil {
			return err
		}
		if len(cfg.RecoveryKeys) == 0 {
			fmt.Println("No recovery key: backups get a random key that stays on this server. Create one with: sudo coolify-mirror recovery-key create")
			return nil
		}
		fmt.Println("New backups are also encrypted to these recovery keys (public halves; the secret halves are not on this server):")
		for _, k := range cfg.RecoveryKeys {
			fmt.Println("  " + k)
		}
		return nil
	case "create", "new":
		secret, public, err := engine.NewRecoveryKey()
		if err != nil {
			return err
		}
		fmt.Println(`
RECOVERY KEY - shown once, not kept on this server
====================================================

  ` + secret + `

Keep it safe now (a password manager is best). With it, every backup made
from now on opens on any server, even after losing this one; without it,
those backups cannot be opened. Anyone who has it can open them.

Public half (stays here): ` + public)
		sendRecoveryKey(ctx, secret, *email, *telegram, *telegramToken)
		return nil
	case "add":
		if fs.NArg() == 0 {
			return errors.New("usage: coolify-mirror recovery-key add age1…   (the public key of a recovery key you already have)")
		}
		pub, err := engine.AddRecoveryKey(fs.Arg(0))
		if err != nil {
			return err
		}
		fmt.Println("New backups are also encrypted to " + pub)
		return nil
	case "send":
		if fs.NArg() == 0 {
			return errors.New("usage: coolify-mirror recovery-key send AGE-SECRET-KEY-1… --email you@example.com --telegram CHAT_ID")
		}
		sendRecoveryKey(ctx, fs.Arg(0), *email, *telegram, *telegramToken)
		return nil
	case "remove":
		if err := engine.RemoveRecoveryKeys(); err != nil {
			return err
		}
		fmt.Println("Recovery keys removed: new backups get a random key again (older ones still open with their recovery key).")
		return nil
	}
	return fmt.Errorf("unknown recovery-key command %q (create, show, add, send, remove)", sub)
}

// sendRecoveryKey delivers the key by email/Telegram (given here or set with
// `notify`), and says what arrived.
func sendRecoveryKey(ctx context.Context, secret, email, telegram, telegramToken string) {
	cfg, err := engine.LoadConfig()
	if err != nil {
		return
	}
	n := cfg.Notify
	if email != "" {
		n.Email = email
	}
	if telegram != "" {
		n.TelegramChat = telegram
	}
	if telegramToken != "" {
		n.TelegramToken = telegramToken
	}
	if len(n.Channels()) == 0 {
		fmt.Println("\nNot sent anywhere (no --email / --telegram, and none set with `coolify-mirror notify set`).")
		return
	}
	in, _ := coolify.Detect(ctx)
	host, _ := os.Hostname()
	subject, body := engine.RecoveryKeyMessage(host, secret)
	sent, errs := engine.Notify(ctx, in, n, subject, body)
	for _, s := range sent {
		fmt.Println("  ✓ sent by " + s)
	}
	for _, e := range errs {
		fmt.Println("  ✗ not sent: " + e.Error())
	}
	if len(sent) == 0 {
		fmt.Println("Nothing arrived: copy the key above now.")
	}
}

func cmdNotify(ctx context.Context, args []string) error {
	sub := "show"
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("notify", flag.ContinueOnError)
	email := fs.String("email", "", "")
	telegram := fs.String("telegram", "", "")
	telegramToken := fs.String("telegram-token", "", "")
	on := fs.String("on", "", "")
	smtpHost := fs.String("smtp-host", "", "")
	smtpPort := fs.Int("smtp-port", 587, "")
	smtpUser := fs.String("smtp-user", "", "")
	smtpPass := fs.String("smtp-password", "", "")
	smtpFrom := fs.String("smtp-from", "", "")
	smtpSec := fs.String("smtp-security", "starttls", "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := engine.LoadConfig()
	if err != nil {
		return err
	}
	switch sub {
	case "show", "status":
		n := cfg.Notify
		if len(n.Channels()) == 0 {
			fmt.Println("No notifications. Set them with: sudo coolify-mirror notify set --email you@example.com --telegram CHAT_ID")
			return nil
		}
		fmt.Println("Notifications go to: " + strings.Join(n.Channels(), ", "))
		mail := "Coolify's mail settings"
		if n.SMTP != nil {
			mail = "SMTP " + n.SMTP.Host + ":" + strconv.Itoa(n.SMTP.Port)
		}
		bot := "Coolify's Telegram bot"
		if n.TelegramToken != "" {
			bot = "coolify-mirror's own bot token"
		}
		on := n.On
		if on == "" {
			on = "failure"
		}
		fmt.Printf("Email through %s · Telegram through %s · scheduled results: %s\n", mail, bot, on)
		return nil
	case "set":
		if *email != "" {
			cfg.Notify.Email = *email
		}
		if *telegram != "" {
			cfg.Notify.TelegramChat = *telegram
		}
		if *telegramToken != "" {
			cfg.Notify.TelegramToken = *telegramToken
		}
		if *on != "" {
			if *on != "failure" && *on != "always" && *on != "never" {
				return errors.New("--on failure|always|never")
			}
			cfg.Notify.On = *on
		}
		if *smtpHost != "" {
			cfg.Notify.SMTP = &engine.SMTPConfig{Host: *smtpHost, Port: *smtpPort, User: *smtpUser, Password: *smtpPass, From: *smtpFrom, Security: *smtpSec}
		}
		if err := cfg.Save(); err != nil {
			return err
		}
		fmt.Println("Saved. Try it: sudo coolify-mirror notify test")
		return nil
	case "test":
		if len(cfg.Notify.Channels()) == 0 {
			return errors.New("no notifications set: sudo coolify-mirror notify set --email … --telegram …")
		}
		in, _ := coolify.Detect(ctx)
		host, _ := os.Hostname()
		sent, errs := engine.Notify(ctx, in, cfg.Notify, "coolify-mirror test - "+host, "Notifications from coolify-mirror on "+host+" work.")
		for _, s := range sent {
			fmt.Println("  ✓ sent by " + s)
		}
		for _, e := range errs {
			fmt.Println("  ✗ " + e.Error())
		}
		if len(errs) > 0 {
			return errors.New("some notifications did not go out")
		}
		return nil
	case "off":
		cfg.Notify = engine.NotifyConfig{}
		return cfg.Save()
	}
	return fmt.Errorf("unknown notify command %q (show, set, test, off)", sub)
}

func cmdSchedule(ctx context.Context, args []string) error {
	sub := "list"
	if len(args) > 0 {
		sub, args = args[0], args[1:]
	}
	switch sub {
	case "list", "status":
		list, err := engine.ListSchedules(ctx)
		if err != nil {
			return err
		}
		if len(list) == 0 {
			fmt.Println("No scheduled backups. Add one, e.g.:\n  sudo coolify-mirror schedule add --at 03:30 --full --to gdrive:coolify --keep-remote 7")
			return nil
		}
		for _, s := range list {
			printSchedule(s)
		}
		return nil
	case "add":
		fs := flag.NewFlagSet("schedule add", flag.ContinueOnError)
		name := fs.String("name", "nightly", "")
		at := fs.String("at", "", "")
		// the rest are backup flags; validate the essentials here
		var rest []string
		for i := 0; i < len(args); i++ {
			a := args[i]
			switch {
			case a == "--name" || a == "--at":
				if i+1 >= len(args) {
					return fmt.Errorf("%s needs a value", a)
				}
				if err := fs.Set(strings.TrimPrefix(a, "--"), args[i+1]); err != nil {
					return err
				}
				i++
			case strings.HasPrefix(a, "--name=") || strings.HasPrefix(a, "--at="):
				kv := strings.SplitN(strings.TrimPrefix(a, "--"), "=", 2)
				if err := fs.Set(kv[0], kv[1]); err != nil {
					return err
				}
			default:
				rest = append(rest, a)
			}
		}
		if !hasFlag(rest, "--to") {
			return errors.New("say where the backups go: --to name:folder (an rclone remote) or --to /mnt/backups")
		}
		if !hasFlag(rest, "--full") && !hasFlag(rest, "--all") && !hasFlag(rest, "--project") && !hasFlag(rest, "--domain") && !hasFlag(rest, "--uuid") {
			rest = append([]string{"--full"}, rest...)
		}
		to := flagValue(rest, "--to")
		o := &offsiteRun{dest: to}
		if err := o.precheck(ctx); err != nil {
			return err
		}
		cal, err := engine.AddSchedule(ctx, *name, *at, rest)
		if err != nil {
			return err
		}
		fmt.Printf("Scheduled %q: %s (%s), then upload to %s, check the copy, free this server's disk.\n", *name, cal, strings.Join(rest, " "), to)
		fmt.Println("Status: sudo coolify-mirror schedule status · run once now: sudo coolify-mirror schedule run " + *name)
		return nil
	case "run", "run-now":
		name := "nightly"
		if len(args) > 0 {
			name = args[0]
		}
		if err := engine.RunScheduleNow(ctx, name); err != nil {
			return err
		}
		fmt.Printf("Started %q in the background. Follow it: journalctl -fu coolify-mirror-%s · result: sudo coolify-mirror schedule status\n", name, name)
		return nil
	case "remove", "rm":
		if len(args) == 0 {
			return errors.New("usage: coolify-mirror schedule remove NAME")
		}
		if err := engine.RemoveSchedule(ctx, args[0]); err != nil {
			return err
		}
		fmt.Printf("Removed %q (the backups already made stay where they are).\n", args[0])
		return nil
	}
	return fmt.Errorf("unknown schedule command %q (list, add, run, remove)", sub)
}

func printSchedule(s engine.Schedule) {
	state := ""
	if s.Disabled {
		state = "  (timer not active)"
	}
	fmt.Printf("%s  at %s  ->  %s%s\n", s.Name, s.At, strings.Join(s.Args, " "), state)
	if !s.NextRun.IsZero() {
		fmt.Printf("  next run: %s\n", s.NextRun.Local().Format("2006-01-02 15:04"))
	}
	if r := s.LastRun; r != nil {
		if r.OK {
			fmt.Printf("  last run: OK %s - %s (%s) in %s\n", r.Started.Local().Format("2006-01-02 15:04"), r.File, engine.HumanBytes(r.Size), r.Duration)
		} else {
			fmt.Printf("  last run: FAILED %s - %s\n", r.Started.Local().Format("2006-01-02 15:04"), r.Error)
		}
	} else {
		fmt.Println("  last run: none yet")
	}
}

func hasFlag(args []string, name string) bool {
	for _, a := range args {
		if a == name || strings.HasPrefix(a, name+"=") {
			return true
		}
	}
	return false
}

func flagValue(args []string, name string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
		if v, ok := strings.CutPrefix(a, name+"="); ok {
			return v
		}
	}
	return ""
}
