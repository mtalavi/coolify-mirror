package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mtalavi/coolify-mirror/internal/run"
)

// Scheduled backups are systemd timers: they survive reboots and run a
// missed time right after the server starts again (Persistent=true). Each
// one runs `coolify-mirror backup … --to DEST`, which uploads, checks the
// copy and only then frees this server's disk.

// UnitDir is where the timers are written.
var UnitDir = "/etc/systemd/system"

const unitPrefix = "coolify-mirror-"

// Schedule is one scheduled backup.
type Schedule struct {
	Name     string   // a-z, 0-9, -
	At       string   // systemd OnCalendar ("*-*-* 03:30:00")
	Args     []string // the backup command's arguments
	NextRun  time.Time
	LastRun  *OffsiteResult
	Disabled bool
}

var scheduleName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,40}$`)

// OnCalendar turns "03:30", "3:30", "daily", "hourly" or a systemd calendar
// expression into the OnCalendar value of a timer.
func OnCalendar(at string) (string, error) {
	at = strings.TrimSpace(at)
	if at == "" {
		return "", errors.New("say when, e.g. 03:30")
	}
	if m := regexp.MustCompile(`^([0-9]{1,2}):([0-9]{2})$`).FindStringSubmatch(at); m != nil {
		var h, mi int
		fmt.Sscanf(m[1]+" "+m[2], "%d %d", &h, &mi)
		if h > 23 || mi > 59 {
			return "", fmt.Errorf("%q is not a time of day", at)
		}
		return fmt.Sprintf("*-*-* %02d:%02d:00", h, mi), nil
	}
	if strings.ContainsAny(at, "\n\r") {
		return "", fmt.Errorf("%q is not a time", at)
	}
	if run.Exists("systemd-analyze") {
		if _, err := run.Text(context.Background(), "systemd-analyze", "calendar", at); err != nil {
			return "", fmt.Errorf("%q is not a time systemd understands (examples: 03:30, daily, Mon *-*-* 04:00)", at)
		}
	}
	return at, nil
}

// unitArg quotes one argument for an ExecStart line.
func unitArg(a string) string {
	if a != "" && !strings.ContainsAny(a, " \t\"'\\$%;") {
		return a
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`, `$`, `$$`).Replace(a) + `"`
}

// ScheduleUnits returns the service and timer files of a schedule.
func ScheduleUnits(name, onCalendar, binary string, args []string) (service, timer string) {
	parts := []string{binary, "backup"}
	for _, a := range args {
		parts = append(parts, unitArg(a))
	}
	parts = append(parts, "--schedule-name", name)
	service = "# Written by coolify-mirror (schedule add). Remove with: coolify-mirror schedule remove " + name + "\n" +
		"[Unit]\nDescription=coolify-mirror scheduled backup " + name + "\n" +
		"After=docker.service network-online.target\nWants=network-online.target\n\n" +
		"[Service]\nType=oneshot\nNice=10\nIOSchedulingClass=best-effort\nIOSchedulingPriority=7\n" +
		"Environment=HOME=/root\n" +
		"ExecStart=" + strings.Join(parts, " ") + "\n"
	timer = "# Written by coolify-mirror (schedule add)\n" +
		"[Unit]\nDescription=coolify-mirror scheduled backup " + name + "\n\n" +
		"[Timer]\nOnCalendar=" + onCalendar + "\nPersistent=true\nRandomizedDelaySec=60\n\n" +
		"[Install]\nWantedBy=timers.target\n"
	return service, timer
}

// AddSchedule writes and starts a timer.
func AddSchedule(ctx context.Context, name, at string, args []string) (string, error) {
	if !scheduleName.MatchString(name) {
		return "", fmt.Errorf("schedule name %q: use a-z, 0-9 and -", name)
	}
	if !run.Exists("systemctl") {
		return "", errors.New("this server has no systemd (systemctl): run `coolify-mirror backup … --to …` from cron instead")
	}
	cal, err := OnCalendar(at)
	if err != nil {
		return "", err
	}
	bin, err := os.Executable()
	if err != nil || strings.HasPrefix(bin, "/tmp") {
		bin = "/usr/local/bin/coolify-mirror"
	}
	svc, tmr := ScheduleUnits(name, cal, bin, args)
	base := filepath.Join(UnitDir, unitPrefix+name)
	if err := os.WriteFile(base+".service", []byte(svc), 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(base+".timer", []byte(tmr), 0o644); err != nil {
		return "", err
	}
	if _, err := run.Text(ctx, "systemctl", "daemon-reload"); err != nil {
		return "", err
	}
	if _, err := run.Text(ctx, "systemctl", "enable", "--now", unitPrefix+name+".timer"); err != nil {
		return "", err
	}
	return cal, nil
}

// RemoveSchedule stops and deletes a timer (the backups stay where they are).
func RemoveSchedule(ctx context.Context, name string) error {
	base := filepath.Join(UnitDir, unitPrefix+name)
	if _, err := os.Stat(base + ".timer"); err != nil {
		return fmt.Errorf("no schedule %q", name)
	}
	_, _ = run.Text(ctx, "systemctl", "disable", "--now", unitPrefix+name+".timer")
	_ = os.Remove(base + ".timer")
	_ = os.Remove(base + ".service")
	_, err := run.Text(ctx, "systemctl", "daemon-reload")
	return err
}

// RunScheduleNow starts a scheduled backup now (in the background).
func RunScheduleNow(ctx context.Context, name string) error {
	if _, err := os.Stat(filepath.Join(UnitDir, unitPrefix+name+".service")); err != nil {
		return fmt.Errorf("no schedule %q", name)
	}
	_, err := run.Text(ctx, "systemctl", "start", "--no-block", unitPrefix+name+".service")
	return err
}

// ListSchedules reads the timers written by coolify-mirror.
func ListSchedules(ctx context.Context) ([]Schedule, error) {
	files, _ := filepath.Glob(filepath.Join(UnitDir, unitPrefix+"*.timer"))
	var out []Schedule
	for _, f := range files {
		name := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(f), unitPrefix), ".timer")
		s := Schedule{Name: name}
		if b, err := os.ReadFile(f); err == nil {
			for _, l := range strings.Split(string(b), "\n") {
				if v, ok := strings.CutPrefix(l, "OnCalendar="); ok {
					s.At = strings.TrimSpace(v)
				}
			}
		}
		if b, err := os.ReadFile(strings.TrimSuffix(f, ".timer") + ".service"); err == nil {
			for _, l := range strings.Split(string(b), "\n") {
				if v, ok := strings.CutPrefix(l, "ExecStart="); ok {
					fs := strings.Fields(v)
					if len(fs) > 2 {
						s.Args = fs[2:]
					}
					// --schedule-name NAME is added by the timer itself
					for i := 0; i+1 < len(s.Args); i++ {
						if s.Args[i] == "--schedule-name" {
							s.Args = append(s.Args[:i:i], s.Args[i+2:]...)
							break
						}
					}
				}
			}
		}
		if out, err := run.Text(ctx, "systemctl", "show", unitPrefix+name+".timer", "-p", "NextElapseUSecRealtime", "-p", "ActiveState", "--value"); err == nil {
			ls := strings.Split(out, "\n")
			if len(ls) >= 1 {
				if t, err := parseSystemdTime(ls[0]); err == nil {
					s.NextRun = t
				}
			}
			if len(ls) >= 2 && strings.TrimSpace(ls[1]) != "active" {
				s.Disabled = true
			}
		}
		s.LastRun, _ = LastResult(name)
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func parseSystemdTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{"Mon 2006-01-02 15:04:05 MST", "Mon 2006-01-02 15:04:05 -0700"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("time %q", s)
}

// ScheduleRunning reports whether a schedule's backup runs right now.
func ScheduleRunning(ctx context.Context, name string) bool {
	out, err := run.Text(ctx, "systemctl", "is-active", unitPrefix+name+".service")
	return err == nil && strings.TrimSpace(out) == "activating"
}
