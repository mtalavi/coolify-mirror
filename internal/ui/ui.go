// Package ui is the interactive terminal interface (arrow keys, space to
// select, enter to confirm). Everything it does is also available as plain
// commands (see main.go), which share the same engine.
package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mtalavi/coolify-mirror/internal/coolify"
	"github.com/mtalavi/coolify-mirror/internal/engine"

	"github.com/charmbracelet/huh"
)

// LogPath is shown to the user when something fails.
var LogPath string

const (
	actBackupSelected = "backup"
	actBackupFull     = "full"
	actRestore        = "restore"
	actShare          = "share"
	actQuit           = "quit"
)

// Run starts the interactive menu.
func Run(ctx context.Context) error {
	Banner()
	fmt.Println()
	in, err := checkServer(ctx)
	if err != nil {
		return err
	}

	for {
		action := ""
		err := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title("What do you want to do?").
				Options(
					huh.NewOption("Make a backup  —  pick domains / resources", actBackupSelected),
					huh.NewOption("Make a FULL backup  —  the whole Coolify server", actBackupFull),
					huh.NewOption("Restore a backup  —  type the share code from the other server", actRestore),
					huh.NewOption("Share or delete an existing backup file", actShare),
					huh.NewOption("Quit", actQuit),
				).
				Value(&action),
		)).WithTheme(theme()).WithShowHelp(true).RunWithContext(ctx)
		if errors.Is(err, huh.ErrUserAborted) || action == actQuit {
			return nil
		}
		if err != nil {
			return err
		}
		switch action {
		case actBackupSelected:
			err = backupSelected(ctx, in)
		case actBackupFull:
			err = backupFull(ctx, in)
		case actRestore:
			err = restoreFlow(ctx, in)
			// A full restore swaps the APP_KEY: detect again for further actions.
			if nin, derr := coolify.Detect(ctx); derr == nil {
				in = nin
			}
		case actShare:
			err = shareExisting(ctx, in)
		}
		if errors.Is(err, errNotRestored) {
			printNotRestored()
			fmt.Println()
			continue
		}
		if errors.Is(err, errShown) {
			fmt.Println()
			continue
		}
		if err != nil {
			if errors.Is(err, huh.ErrUserAborted) || errors.Is(err, errBack) {
				fmt.Println(sMuted.Render("  (back to the menu)"))
				fmt.Println()
				continue
			}
			if errors.Is(err, context.Canceled) && ctx.Err() != nil {
				return err
			}
			showError(err)
		}
		fmt.Println()
	}
}

// RunRestore restores a share code, link or file interactively (the menu's
// restore, without the menu).
func RunRestore(ctx context.Context, source string) error {
	Banner()
	fmt.Println()
	in, err := checkServer(ctx)
	if err != nil {
		return err
	}
	err = restoreSource(ctx, in, source)
	switch {
	case errors.Is(err, errNotRestored):
		printNotRestored()
		return errors.New("not restored")
	case errors.Is(err, huh.ErrUserAborted), errors.Is(err, errBack):
		return errors.New("not restored")
	case errors.Is(err, errShown):
		return errors.New("restored, but not everything is running - see above")
	case err != nil:
		showError(err)
		return err
	}
	return nil
}

// checkServer detects Coolify, checks that this version of coolify-mirror
// works with it and whether a newer release exists, and prints the result.
func checkServer(ctx context.Context) (*coolify.Instance, error) {
	newer := make(chan string, 1)
	go func() { newer <- engine.NewerRelease(ctx) }()
	var compat *engine.Compat
	in, err := withSpinner(ctx, "Checking this server", func(ctx context.Context) (*coolify.Instance, error) {
		in, err := coolify.Detect(ctx)
		if err == nil {
			compat = engine.CheckCompat(ctx, in)
		}
		return in, err
	})
	if err != nil {
		return nil, err
	}
	if msg := engine.RecoverPaused(ctx); msg != "" {
		fmt.Println("  " + sWarn.Render("! "+msg))
	}
	v4, _ := in.PublicIPs(ctx)
	state := sOK.Render("✓ tested with this version")
	switch {
	case len(compat.Blockers) > 0:
		state = sErr.Render("✗ not supported")
	case compat.Status != "tested":
		state = sWarn.Render("not tested with this version yet")
	}
	fmt.Println(sMuted.Render(fmt.Sprintf("  Coolify %s on %s (%s) · ", in.Version, in.Hostname, v4)) + state)
	for _, b := range compat.Blockers {
		fmt.Println("  " + sErr.Render("✗ "+b))
	}
	for _, w := range compat.Warnings {
		fmt.Println("  " + sWarn.Render("! "+w))
	}
	select {
	case v := <-newer:
		if v != "" {
			fmt.Println("  " + sAccent.Render("★ coolify-mirror "+v+" is available") + sMuted.Render(" - update with: sudo coolify-mirror update"))
		}
	case <-time.After(1500 * time.Millisecond):
	}
	fmt.Println()
	return in, nil
}

func printNotRestored() {
	fmt.Println(boxed(sErrBox, sWarn.Render("Not restored")+"\n"+
		"Nothing was changed on this server: the restore was cancelled before it started.\n"+
		sMuted.Render("To restore, start the restore again and answer Yes.")))
}

var (
	errBack        = errors.New("back")
	errNotRestored = errors.New("restore cancelled")
	errShown       = errors.New("problem already shown")
)

func showError(err error) {
	msg := err.Error()
	if LogPath != "" {
		msg += "\n\n" + sMuted.Render("Details: "+LogPath)
	}
	fmt.Println(boxed(sErrBox, sErr.Render("Failed")+"\n"+wrap(msg, termWidth()-8)))
}

func wrap(s string, w int) string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		for len([]rune(line)) > w {
			r := []rune(line)
			cut := w
			for i := w; i > w/2; i-- {
				if r[i] == ' ' {
					cut = i
					break
				}
			}
			out = append(out, string(r[:cut]))
			line = strings.TrimLeft(string(r[cut:]), " ")
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

func confirm(ctx context.Context, title, desc string, def bool) (bool, error) {
	ok := def
	err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title(title).Description(desc).Affirmative("Yes").Negative("No").Value(&ok),
	)).WithTheme(theme()).RunWithContext(ctx)
	return ok, err
}

func isRoot() bool { return os.Geteuid() == 0 }
