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
	fmt.Println()
	fmt.Println(sTitle.Render("◆ Coolify Mirror "+engine.Version) + sMuted.Render("  ·  full backup & restore for Coolify"))
	in, err := withSpinner(ctx, "Checking this server", func(ctx context.Context) (*coolify.Instance, error) {
		return coolify.Detect(ctx)
	})
	if err != nil {
		return err
	}
	if msg := engine.RecoverPaused(ctx); msg != "" {
		fmt.Println("  " + sWarn.Render("! "+msg))
	}
	v4, _ := in.PublicIPs(ctx)
	fmt.Println(sMuted.Render(fmt.Sprintf("  Coolify %s on %s (%s)", in.Version, in.Hostname, v4)))
	fmt.Println()

	for {
		action := ""
		err := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title("What do you want to do?").
				Options(
					huh.NewOption("Make a backup  —  pick domains / resources", actBackupSelected),
					huh.NewOption("Make a FULL backup  —  the whole Coolify server", actBackupFull),
					huh.NewOption("Restore a backup  —  paste the link from the other server", actRestore),
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

var errBack = errors.New("back")

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
