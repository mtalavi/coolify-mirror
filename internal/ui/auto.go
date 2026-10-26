package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/huh"

	"github.com/mtalavi/coolify-mirror/internal/coolify"
	"github.com/mtalavi/coolify-mirror/internal/engine"
)

const autoMenuName = "Automatic backups"

// automaticBackups is the menu for off-site backups: the recovery key,
// notifications and scheduled backups that upload, check and free the disk.
func automaticBackups(ctx context.Context, in *coolify.Instance) error {
	for {
		cfg, err := engine.LoadConfig()
		if err != nil {
			return err
		}
		schedules, _ := engine.ListSchedules(ctx)
		printAutoStatus(ctx, cfg, schedules)

		const (
			aKey, aAdd, aRun, aRemove, aNotify, aTest, aBack = "key", "add", "run", "remove", "notify", "test", "back"
		)
		var opts []huh.Option[string]
		if len(cfg.RecoveryKeys) == 0 {
			opts = append(opts, huh.NewOption("Create the recovery key (needed first)", aKey))
		}
		opts = append(opts, huh.NewOption("Add a scheduled backup", aAdd))
		if len(schedules) > 0 {
			opts = append(opts, huh.NewOption("Run a scheduled backup now", aRun), huh.NewOption("Remove a scheduled backup", aRemove))
		}
		opts = append(opts, huh.NewOption("Notifications (email, Telegram)", aNotify))
		if len(cfg.Notify.Channels()) > 0 {
			opts = append(opts, huh.NewOption("Send a test notification", aTest))
		}
		if len(cfg.RecoveryKeys) > 0 {
			opts = append(opts, huh.NewOption("Create another recovery key", aKey))
		}
		opts = append(opts, huh.NewOption("Back", aBack))
		choice := opts[0].Value
		if err := huh.NewForm(huh.NewGroup(huh.NewSelect[string]().Title(autoMenuName).
			Description("Backups that leave this server on their own: if it is lost, restore everything on a new one in minutes.").
			Options(opts...).Value(&choice))).WithTheme(theme()).WithKeyMap(keys()).RunWithContext(ctx); err != nil {
			return err
		}
		switch choice {
		case aBack:
			return nil
		case aKey:
			err = createRecoveryKey(ctx, in)
		case aAdd:
			err = addSchedule(ctx, in)
		case aRun:
			err = pickSchedule(ctx, schedules, "Run which scheduled backup now?", func(name string) error {
				if err := engine.RunScheduleNow(ctx, name); err != nil {
					return err
				}
				fmt.Println(sOK.Render("  ✓ ") + "Started in the background. Follow it with: journalctl -fu coolify-mirror-" + name)
				return nil
			})
		case aRemove:
			err = pickSchedule(ctx, schedules, "Remove which scheduled backup?", func(name string) error {
				if err := engine.RemoveSchedule(ctx, name); err != nil {
					return err
				}
				fmt.Println(sOK.Render("  ✓ ") + "Removed (the backups already made stay where they are).")
				return nil
			})
		case aNotify:
			err = setNotifications(ctx)
		case aTest:
			host, _ := os.Hostname()
			sent, errs := engine.Notify(ctx, in, cfg.Notify, "coolify-mirror test - "+host, "Notifications from coolify-mirror on "+host+" work.")
			printSent(sent, errs)
		}
		if errors.Is(err, huh.ErrUserAborted) {
			err = nil
		}
		if err != nil {
			showError(err)
		}
		fmt.Println()
	}
}

func printAutoStatus(ctx context.Context, cfg *engine.Config, schedules []engine.Schedule) {
	var lines []string
	if len(cfg.RecoveryKeys) == 0 {
		lines = append(lines, sWarn.Render("✗ No recovery key yet")+sMuted.Render(" - without it a backup kept elsewhere cannot be opened after losing this server"))
	} else {
		lines = append(lines, sOK.Render("✓ Recovery key set")+sMuted.Render(" - new backups open with it on any server"))
	}
	if v := engine.RcloneVersion(ctx); v != "" {
		remotes, _ := engine.RcloneRemotes(ctx)
		r := strings.Join(remotes, " ")
		if r == "" {
			r = "no destination yet - add one with: rclone config"
		}
		lines = append(lines, sOK.Render("✓ ")+v+sMuted.Render(" · "+r))
	} else {
		lines = append(lines, sMuted.Render("• rclone not installed: backups can go to a folder; for Google Drive, S3, SFTP… install it: curl https://rclone.org/install.sh | sudo bash"))
	}
	if ch := cfg.Notify.Channels(); len(ch) > 0 {
		lines = append(lines, sOK.Render("✓ ")+"Notifications: "+strings.Join(ch, ", "))
	} else {
		lines = append(lines, sMuted.Render("• No notifications (email / Telegram)"))
	}
	if len(schedules) == 0 {
		lines = append(lines, sMuted.Render("• No scheduled backup"))
	}
	for _, s := range schedules {
		l := sBold.Render(s.Name) + "  " + s.At + "  " + sMuted.Render(strings.Join(s.Args, " "))
		if r := s.LastRun; r != nil {
			if r.OK {
				l += "\n    " + sOK.Render("last: OK ") + r.Started.Local().Format("2006-01-02 15:04") + sMuted.Render(" · "+r.File+" · "+engine.HumanBytes(r.Size))
			} else {
				l += "\n    " + sErr.Render("last: FAILED ") + r.Started.Local().Format("2006-01-02 15:04") + sMuted.Render(" · "+truncate(r.Error, 90))
			}
		}
		if !s.NextRun.IsZero() {
			l += "\n    " + sMuted.Render("next: "+s.NextRun.Local().Format("2006-01-02 15:04"))
		}
		lines = append(lines, l)
	}
	fmt.Println(boxed(sBox, sBold.Render(autoMenuName)+"\n"+strings.Join(lines, "\n")))
}

func pickSchedule(ctx context.Context, list []engine.Schedule, title string, do func(string) error) error {
	name := list[0].Name
	var opts []huh.Option[string]
	for _, s := range list {
		opts = append(opts, huh.NewOption(s.Name+"  "+s.At, s.Name))
	}
	if err := huh.NewForm(huh.NewGroup(huh.NewSelect[string]().Title(title).Options(opts...).Value(&name))).
		WithTheme(theme()).WithKeyMap(keys()).RunWithContext(ctx); err != nil {
		return err
	}
	return do(name)
}

// createRecoveryKey makes the key, shows it once, sends it if wanted and
// makes sure it was saved.
func createRecoveryKey(ctx context.Context, in *coolify.Instance) error {
	secret, public, err := engine.NewRecoveryKey()
	if err != nil {
		return err
	}
	fmt.Println(boxed(sErrBox, sWarn.Render("Recovery key - shown once, not kept on this server")+"\n\n  "+sBold.Render(secret)+"\n\n"+
		wrap("Keep it safe now (a password manager is best). With it, every backup made from now on opens on any server, even after losing this one. Without it those backups cannot be opened. Anyone who has it can open them.", termWidth()-8)+
		"\n"+sMuted.Render("Public half (stays here): "+public)))
	copyToClipboard(secret)
	fmt.Println(sMuted.Render("  It was also sent to your clipboard (if your terminal allows it)."))
	cfg, _ := engine.LoadConfig()
	n := cfg.Notify
	email, chat := n.Email, n.TelegramChat
	if err := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Also send it by email (optional)").Description("Empty = no email. Uses Coolify's mail settings unless coolify-mirror has its own").Value(&email),
		huh.NewInput().Title("And/or to a Telegram chat id (optional)").Description("Your numeric id (ask @userinfobot) - send /start to Coolify's bot first. Empty = no Telegram").Value(&chat),
	)).WithTheme(theme()).WithKeyMap(keys()).RunWithContext(ctx); err != nil && !errors.Is(err, huh.ErrUserAborted) {
		return err
	}
	n.Email, n.TelegramChat = strings.TrimSpace(email), strings.TrimSpace(chat)
	if len(n.Channels()) > 0 {
		host, _ := os.Hostname()
		subject, body := engine.RecoveryKeyMessage(host, secret)
		sent, errs := engine.Notify(ctx, in, n, subject, body)
		printSent(sent, errs)
		if len(sent) > 0 {
			cfg.Notify.Email, cfg.Notify.TelegramChat = n.Email, n.TelegramChat
			_ = cfg.Save()
		}
	}
	saved := ""
	return huh.NewForm(huh.NewGroup(huh.NewInput().Title("Type saved once the key is somewhere safe").Value(&saved).
		Validate(func(s string) error {
			if strings.ToLower(strings.TrimSpace(s)) != "saved" {
				return errors.New("type saved - the key is not shown again")
			}
			return nil
		}))).WithTheme(theme()).WithKeyMap(keys()).RunWithContext(ctx)
}

func printSent(sent []string, errs []error) {
	for _, s := range sent {
		fmt.Println("  " + sOK.Render("✓ sent by ") + s)
	}
	for _, e := range errs {
		fmt.Println("  " + sErr.Render("✗ ") + e.Error())
	}
}

// addSchedule asks what, where, when and how many to keep, then writes the timer.
func addSchedule(ctx context.Context, in *coolify.Instance) error {
	cfg, err := engine.LoadConfig()
	if err != nil {
		return err
	}
	if len(cfg.RecoveryKeys) == 0 {
		fmt.Println(sWarn.Render("  First the recovery key: without it these backups could not be opened after losing this server."))
		if err := createRecoveryKey(ctx, in); err != nil {
			return err
		}
	}
	what := "--full"
	dest := ""
	var destOpts []huh.Option[string]
	remotes, _ := engine.RcloneRemotes(ctx)
	for _, r := range remotes {
		destOpts = append(destOpts, huh.NewOption("rclone "+r+"  (a folder in it)", r))
	}
	destOpts = append(destOpts, huh.NewOption("A folder on this server (another disk or a mount)", "/"))
	if len(remotes) == 0 {
		fmt.Println(sMuted.Render("  rclone has no destination yet: for Google Drive, S3, SFTP… run `rclone config` (install: curl https://rclone.org/install.sh | sudo bash), then come back."))
	}
	dest = destOpts[0].Value
	if err := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title("What to back up").Options(
			huh.NewOption("The whole server (recommended: everything comes back on a new server)", "--full"),
			huh.NewOption("Every app (merges into an existing Coolify)", "--all"),
		).Value(&what),
		huh.NewSelect[string]().Title("Where the backups go").Options(destOpts...).Value(&dest),
	)).WithTheme(theme()).WithKeyMap(keys()).RunWithContext(ctx); err != nil {
		return err
	}
	folder := "coolify-backups"
	if dest == "/" {
		folder = "/mnt/backups/coolify"
	}
	at, keepRemote, keepLocal, name := "03:30", "7", "0", "nightly"
	if err := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title(map[bool]string{true: "Folder on this server", false: "Folder in " + dest}[dest == "/"]).Value(&folder),
		huh.NewInput().Title("Every day at (server time)").Description("03:30, or a systemd time like Mon *-*-* 04:00").Value(&at).
			Validate(func(s string) error { _, err := engine.OnCalendar(s); return err }),
		huh.NewInput().Title("Backups to keep there").Description("Older ones are deleted there after each new one (0 = keep all)").Value(&keepRemote).Validate(isCount),
		huh.NewInput().Title("Backups to keep on this server").Description("0 = none: each one is deleted here once its upload is checked").Value(&keepLocal).Validate(isCount),
		huh.NewInput().Title("Name of this schedule").Value(&name),
	)).WithTheme(theme()).WithKeyMap(keys()).RunWithContext(ctx); err != nil {
		return err
	}
	target := strings.TrimSpace(folder)
	if dest != "/" {
		target = dest + strings.TrimLeft(target, "/")
	}
	args := []string{what, "--to", target, "--keep-remote", strings.TrimSpace(keepRemote), "--keep-local", strings.TrimSpace(keepLocal)}
	cal, err := withSpinner(ctx, "Checking the destination and writing the timer", func(ctx context.Context) (string, error) {
		if engine.IsRemote(target) {
			if _, err := engine.ListBackupsAt(ctx, target); err != nil && !strings.Contains(err.Error(), "directory not found") {
				return "", fmt.Errorf("the destination %s does not work: %w", target, err)
			}
		} else if err := os.MkdirAll(target, 0o700); err != nil {
			return "", err
		}
		return engine.AddSchedule(ctx, strings.TrimSpace(name), at, args)
	})
	if err != nil {
		return err
	}
	fmt.Println(sOK.Render("  ✓ ") + fmt.Sprintf("Scheduled %q at %s → %s", strings.TrimSpace(name), cal, target))
	now := true
	if err := huh.NewForm(huh.NewGroup(huh.NewConfirm().Title("Run it once now, to see it work?").Affirmative("Yes").Negative("No").Value(&now))).
		WithTheme(theme()).WithKeyMap(keys()).RunWithContext(ctx); err == nil && now {
		if err := engine.RunScheduleNow(ctx, strings.TrimSpace(name)); err != nil {
			return err
		}
		fmt.Println(sOK.Render("  ✓ ") + "Started in the background. Its result shows here (" + autoMenuName + ") and in: journalctl -fu coolify-mirror-" + strings.TrimSpace(name))
	}
	return nil
}

func isCount(s string) error {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err != nil || n < 0 {
		return errors.New("a number, 0 or more")
	}
	return nil
}

func setNotifications(ctx context.Context) error {
	cfg, err := engine.LoadConfig()
	if err != nil {
		return err
	}
	n := cfg.Notify
	on := n.On
	if on == "" {
		on = "failure"
	}
	if err := huh.NewForm(huh.NewGroup(
		huh.NewInput().Title("Email address").Description("Empty = no email. Sent through Coolify's mail settings (Settings → Email)").Value(&n.Email),
		huh.NewInput().Title("Telegram chat id").Description("Empty = no Telegram. Sent by Coolify's Telegram bot (Notifications → Telegram); send /start to it first").Value(&n.TelegramChat),
		huh.NewSelect[string]().Title("Scheduled backups report").Options(
			huh.NewOption("Only failures", "failure"), huh.NewOption("Every run", "always"), huh.NewOption("Never", "never"),
		).Value(&on),
	)).WithTheme(theme()).WithKeyMap(keys()).RunWithContext(ctx); err != nil {
		return err
	}
	n.Email, n.TelegramChat, n.On = strings.TrimSpace(n.Email), strings.TrimSpace(n.TelegramChat), on
	cfg.Notify = n
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Println(sOK.Render("  ✓ ") + "Saved. Own SMTP server or bot: sudo coolify-mirror notify set --help")
	return nil
}

// pickStoredBackup lists the backups of an rclone remote or a folder and
// returns the chosen one (newest first, with its Coolify version in the name).
func pickStoredBackup(ctx context.Context) (string, error) {
	where := "/"
	var opts []huh.Option[string]
	remotes, _ := engine.RcloneRemotes(ctx)
	for _, r := range remotes {
		opts = append(opts, huh.NewOption("rclone "+r, r))
	}
	opts = append(opts, huh.NewOption("A folder on this server", "/"))
	if len(remotes) > 0 {
		where = remotes[0]
	} else {
		fmt.Println(sMuted.Render("  rclone has no remote here: to restore from Google Drive, S3, SFTP… install rclone and run `rclone config` with the same account."))
	}
	folder := "coolify-backups"
	if err := huh.NewForm(huh.NewGroup(huh.NewSelect[string]().Title("Which storage?").Options(opts...).Value(&where))).
		WithTheme(theme()).WithKeyMap(keys()).RunWithContext(ctx); err != nil {
		return "", err
	}
	if where == "/" {
		folder = "/mnt/backups/coolify"
	}
	if err := huh.NewForm(huh.NewGroup(huh.NewInput().Title("Folder").Value(&folder))).
		WithTheme(theme()).WithKeyMap(keys()).RunWithContext(ctx); err != nil {
		return "", err
	}
	src := strings.TrimSpace(folder)
	if where != "/" {
		src = where + strings.TrimLeft(src, "/")
	}
	list, err := withSpinner(ctx, "Reading "+src, func(ctx context.Context) ([]engine.StoredBackupAt, error) {
		return engine.ListBackupsAt(ctx, src)
	})
	if err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "", fmt.Errorf("no backup (.cmb) in %s", src)
	}
	pick := list[0].Path
	var bo []huh.Option[string]
	for i, b := range list {
		label := fmt.Sprintf("%s  %9s  %s", b.ModTime.Local().Format("2006-01-02 15:04"), engine.HumanBytes(b.Size), b.Name)
		if i == 0 {
			label += "  (newest)"
		}
		bo = append(bo, huh.NewOption(truncate(label, termWidth()-10), b.Path))
	}
	if err := huh.NewForm(huh.NewGroup(huh.NewSelect[string]().Title("Which backup?").
		Description("The name says the server, full or apps, and the Coolify version it needs").Options(bo...).Value(&pick))).
		WithTheme(theme()).WithKeyMap(keys()).RunWithContext(ctx); err != nil {
		return "", err
	}
	return pick, nil
}
