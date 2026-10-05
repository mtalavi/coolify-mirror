package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mtalavi/coolify-mirror/internal/coolify"
	"github.com/mtalavi/coolify-mirror/internal/engine"
	"github.com/mtalavi/coolify-mirror/internal/transfer"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

const shareNone = "none"

// offerShare asks how to hand the file to the other server and starts sharing.
func offerShare(ctx context.Context, in *coolify.Instance, file, key string) error {
	proxy := engine.ProxyAvailable(ctx)
	var opts []huh.Option[string]
	if proxy {
		opts = append(opts, huh.NewOption("Share a link through Coolify's proxy on port 80 (recommended - already open)", engine.ShareProxy))
	}
	opts = append(opts,
		huh.NewOption(fmt.Sprintf("Share a link on port %d (the port must be open in the firewall)", engine.DefaultPort), engine.ShareDirect),
		huh.NewOption("Don't share now (copy the file yourself, or share it later from the menu)", shareNone))
	mode := opts[0].Value
	err := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title("How should the other server get this backup?").Options(opts...).Value(&mode),
	)).WithTheme(theme()).RunWithContext(ctx)
	if err != nil {
		return err
	}
	if mode == shareNone {
		fmt.Println(sMuted.Render("  To restore it elsewhere, copy the file to the other server and run:"))
		fmt.Printf("    ./coolify-mirror restore %s --key %s\n", filepath.Base(file), key)
		return nil
	}
	opt := engine.ShareOptions{Mode: mode, Port: engine.DefaultPort}
	if mode == engine.ShareDirect && engine.UFWBlocks(ctx, opt.Port) {
		open, err := confirm(ctx, fmt.Sprintf("The ufw firewall blocks port %d. Open it while sharing?", opt.Port),
			"The rule is removed again when sharing stops.", true)
		if err != nil {
			return err
		}
		opt.OpenFirewall = open
	}
	return shareScreen(ctx, in, file, key, opt)
}

func shareScreen(ctx context.Context, in *coolify.Instance, file, key string, opt engine.ShareOptions) error {
	sh, err := withSpinner(ctx, "Starting to share", func(ctx context.Context) (*engine.Share, error) {
		return engine.StartShare(ctx, in, file, key, opt)
	})
	if err != nil {
		return err
	}
	fmt.Println()
	fmt.Println(sBold.Render("  On the other Coolify server, run this tool, choose \"Restore a backup\" and paste:"))
	fmt.Println()
	fmt.Println("  " + sLink.Render(sh.Link))
	fmt.Println()
	fmt.Println(sMuted.Render("  The other server needs this tool too. As root there, this downloads it (checksum verified) and opens the menu:"))
	fmt.Println("  " + sh.ToolCommand())
	fmt.Println()
	if sh.Mode == engine.ShareDirect {
		fmt.Println(sMuted.Render(fmt.Sprintf("  If the other server cannot connect, open TCP port %d in your cloud firewall or share through the proxy instead.", engine.DefaultPort)))
	}

	m := newShareModel(sh)
	_, err = tea.NewProgram(m, tea.WithContext(ctx)).Run()
	if m.background {
		sh.Stop()
		opt.Token = sh.Token
		pid, logf, derr := engine.Detach(file, key, opt, 24*time.Hour)
		if derr != nil {
			return fmt.Errorf("could not keep sharing in the background: %w", derr)
		}
		fmt.Println(sOK.Render("  ✓ ") + fmt.Sprintf("Still sharing in the background for 24 hours (pid %d, log %s).", pid, logf))
		fmt.Println(sMuted.Render(fmt.Sprintf("    The link stays the same. Stop it early with: kill %d", pid)))
		return nil
	}
	sh.Stop()
	fmt.Println(sMuted.Render("  Sharing stopped."))
	if err != nil && !errors.Is(err, tea.ErrProgramKilled) && !errors.Is(err, tea.ErrInterrupted) {
		return err
	}
	return nil
}

type shareEvent transfer.Event

type downloader struct {
	sent, total int64
	complete    bool
	last        time.Time
	samples     []sample
}

type shareModel struct {
	sh         *engine.Share
	spin       spinner.Model
	bar        progress.Model
	peers      map[string]*downloader
	completed  int
	background bool
	width      int
}

func newShareModel(sh *engine.Share) *shareModel {
	sp := spinner.New()
	sp.Spinner = spinner.MiniDot
	sp.Style = sAccent
	return &shareModel{sh: sh, spin: sp, bar: progress.New(progress.WithGradient("#7C3AED", "#22C55E"), progress.WithoutPercentage()),
		peers: map[string]*downloader{}, width: 80}
}

func (m *shareModel) wait() tea.Cmd {
	return func() tea.Msg { return shareEvent(<-m.sh.Events) }
}

func (m *shareModel) Init() tea.Cmd { return tea.Batch(m.spin.Tick, m.wait()) }

func (m *shareModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "b":
			m.background = true
			return m, tea.Quit
		}
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case shareEvent:
		host := msg.Remote
		if h, _, ok := strings.Cut(host, ":"); ok && !strings.Contains(host, "]") {
			host = h
		}
		d := m.peers[host]
		if d == nil {
			d = &downloader{}
			m.peers[host] = d
		}
		d.sent, d.total, d.last = msg.Sent, msg.Total, time.Now()
		d.samples = append(d.samples, sample{time.Now(), msg.Sent})
		if len(d.samples) > 20 {
			d.samples = d.samples[1:]
		}
		if msg.Complete && !d.complete {
			d.complete = true
			m.completed++
		}
		return m, m.wait()
	}
	return m, nil
}

func (m *shareModel) View() string {
	var b strings.Builder
	if len(m.peers) == 0 {
		b.WriteString("  " + m.spin.View() + " Waiting for the other server to download…\n")
	}
	hosts := make([]string, 0, len(m.peers))
	for h := range m.peers {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	w := m.width - 60
	if w < 20 {
		w = 20
	}
	if w > 50 {
		w = 50
	}
	m.bar.Width = w
	for _, h := range hosts {
		d := m.peers[h]
		frac := 0.0
		if d.total > 0 {
			frac = float64(d.sent) / float64(d.total)
		}
		if d.complete {
			b.WriteString(fmt.Sprintf("  %s %s downloaded the complete backup (%s)\n", sOK.Render("✓"), h, engine.HumanBytes(d.total)))
			continue
		}
		rate := ""
		if n := len(d.samples); n > 1 {
			dt := d.samples[n-1].at.Sub(d.samples[0].at).Seconds()
			if dt > 0.3 {
				rate = engine.HumanBytes(int64(float64(d.samples[n-1].done-d.samples[0].done)/dt)) + "/s"
			}
		}
		b.WriteString(fmt.Sprintf("  %s %-15s %s %3.0f%%  %s / %s  %s\n", m.spin.View(), h, m.bar.ViewAs(frac), frac*100,
			engine.HumanBytes(d.sent), engine.HumanBytes(d.total), sMuted.Render(rate)))
	}
	if m.completed > 0 {
		b.WriteString("\n  " + sOK.Render("The other server has the backup.") + " It verifies and restores it on its own - you can stop sharing now.\n")
	}
	b.WriteString(sMuted.Render("\n  q stop sharing · b keep sharing in the background (24 h) and exit") + "\n")
	return b.String()
}

// shareExisting lets the user pick a backup file created earlier.
func shareExisting(ctx context.Context, in *coolify.Instance) error {
	files, _ := filepath.Glob(filepath.Join(engine.BackupsDir, "*.cmb"))
	if len(files) == 0 {
		fmt.Println(sWarn.Render("  No backups found in " + engine.BackupsDir))
		return errBack
	}
	sort.Slice(files, func(i, j int) bool {
		a, _ := os.Stat(files[i])
		b, _ := os.Stat(files[j])
		return a.ModTime().After(b.ModTime())
	})
	var opts []huh.Option[string]
	for _, f := range files {
		st, _ := os.Stat(f)
		opts = append(opts, huh.NewOption(fmt.Sprintf("%s  %s  %s", filepath.Base(f), engine.HumanBytes(st.Size()), st.ModTime().Format("2006-01-02 15:04")), f))
	}
	file := files[0]
	action := "share"
	if err := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title("Which backup?").Options(opts...).Value(&file),
		huh.NewSelect[string]().Title("Do what with it?").Options(
			huh.NewOption("Share it (show a link for the other server)", "share"),
			huh.NewOption("Delete it (free the disk space)", "delete"),
		).Value(&action),
	)).WithTheme(theme()).RunWithContext(ctx); err != nil {
		return err
	}
	if action == "delete" {
		ok, err := confirm(ctx, "Delete "+filepath.Base(file)+"?", "This cannot be undone.", false)
		if err != nil || !ok {
			return errBack
		}
		_ = os.Remove(file + ".key")
		if err := os.Remove(file); err != nil {
			return err
		}
		fmt.Println(sOK.Render("  ✓ ") + "deleted " + filepath.Base(file))
		return nil
	}
	key := ""
	if b, err := os.ReadFile(file + ".key"); err == nil {
		key = strings.TrimSpace(string(b))
	}
	if key == "" {
		if err := huh.NewForm(huh.NewGroup(huh.NewInput().Title("Key of this backup").
			Description("It was shown when the backup was created").Value(&key))).WithTheme(theme()).RunWithContext(ctx); err != nil {
			return err
		}
	}
	return offerShare(ctx, in, file, strings.TrimSpace(key))
}
