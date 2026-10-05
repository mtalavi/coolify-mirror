package ui

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/mtalavi/coolify-mirror/internal/coolify"
	"github.com/mtalavi/coolify-mirror/internal/engine"

	"github.com/charmbracelet/huh"
	"golang.org/x/term"
)

// termWidth is the terminal width (100 when unknown).
func termWidth() int {
	if w, _, err := term.GetSize(int(os.Stdout.Fd())); err == nil && w > 20 {
		return w
	}
	return 100
}

// resourceLabel renders one line: domain(s), status, name, type, project/env.
// It is shortened to fit the terminal so options never wrap.
func resourceLabel(r coolify.Resource) string {
	var dom string
	switch len(r.Domains) {
	case 0:
		dom = "(no domain)"
	case 1:
		dom = coolify.Host(r.Domains[0])
	default:
		dom = fmt.Sprintf("%s +%d", coolify.Host(r.Domains[0]), len(r.Domains)-1)
	}
	status := "●"
	if !r.Running() {
		status = "○"
	}
	w := termWidth()
	domW := w / 3
	if domW > 34 {
		domW = 34
	}
	if domW < 16 {
		domW = 16
	}
	label := fmt.Sprintf("%-*s  %s %s · %s · %s/%s", domW, truncate(dom, domW), status, r.Name, r.Label(), r.Project, r.Environment)
	if !r.Local() {
		label += "  [remote server " + r.ServerName + " - not supported]"
	}
	return truncate(label, w-10)
}

func backupSelected(ctx context.Context, in *coolify.Instance) error {
	all, err := withSpinner(ctx, "Reading projects and domains", func(ctx context.Context) ([]coolify.Resource, error) {
		return in.ListResources(ctx)
	})
	if err != nil {
		return err
	}
	if len(all) == 0 {
		fmt.Println(sWarn.Render("  This Coolify has no resources yet."))
		return errBack
	}
	byUUID := map[string]coolify.Resource{}
	var opts []huh.Option[string]
	for _, r := range all {
		byUUID[r.UUID] = r
		opts = append(opts, huh.NewOption(resourceLabel(r), r.UUID))
	}
	var chosen []string
	height := len(opts) + 2
	if height > 18 {
		height = 18
	}
	for len(chosen) == 0 {
		err = huh.NewForm(huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Which domains / resources should be backed up?").
				Description("↑/↓ move · space select · / filter · ctrl+a all · enter continue · esc back").
				Options(opts...).
				Filterable(true).
				Height(height).
				Value(&chosen).
				Validate(func(v []string) error {
					for _, u := range v {
						if r := byUUID[u]; !r.Local() {
							return fmt.Errorf("%s runs on remote server %s - only resources on this server can be backed up", r.Name, r.ServerName)
						}
					}
					return nil
				}),
		)).WithTheme(theme()).WithShowHelp(true).RunWithContext(ctx)
		if err != nil {
			return err
		}
		if len(chosen) == 0 {
			fmt.Println(sWarn.Render("  Nothing selected - press space on a line to select it, then enter."))
		}
	}
	var sel []coolify.Resource
	for _, u := range chosen {
		sel = append(sel, byUUID[u])
	}
	for _, r := range sel {
		fmt.Println("  " + sOK.Render("›") + " " + resourceLabel(r))
	}

	deps, err := withSpinner(ctx, "Looking for databases and services they depend on", func(ctx context.Context) ([]engine.Dependency, error) {
		return engine.ResolveDependencies(ctx, in, sel, all)
	})
	if err != nil {
		return err
	}
	if len(deps) > 0 {
		var lines []string
		for _, d := range deps {
			lines = append(lines, fmt.Sprintf("• %s (%s) — %s", d.Resource.Name, d.Resource.Label(), d.Reason))
		}
		include, err := confirm(ctx, "Also back up what they depend on? (recommended)", strings.Join(lines, "\n"), true)
		if err != nil {
			return err
		}
		if include {
			for _, d := range deps {
				sel = append(sel, d.Resource)
				fmt.Println("  " + sOK.Render("+") + " " + resourceLabel(d.Resource))
			}
		}
	}

	req := engine.BackupRequest{Mode: engine.ModeSelective, Resources: sel}
	if err := backupOptions(ctx, &req, false); err != nil {
		return err
	}
	return runBackup(ctx, in, req)
}

func backupFull(ctx context.Context, in *coolify.Instance) error {
	all, err := withSpinner(ctx, "Reading the Coolify configuration", func(ctx context.Context) ([]coolify.Resource, error) {
		return in.ListResources(ctx)
	})
	if err != nil {
		return err
	}
	local, remote := 0, 0
	for _, r := range all {
		if r.Local() {
			local++
		} else {
			remote++
		}
	}
	fmt.Println(sMuted.Render(fmt.Sprintf("  A full backup contains Coolify's database (projects, settings, users, keys, env vars),\n  the APP_KEY, /data/coolify (proxy, certificates, compose files) and the data of %d resource(s) on this server.", local)))
	if remote > 0 {
		fmt.Println(sWarn.Render(fmt.Sprintf("  %d resource(s) run on remote servers: their settings are included, their data stays on those servers.", remote)))
	}
	req := engine.BackupRequest{Mode: engine.ModeFull}
	if err := backupOptions(ctx, &req, true); err != nil {
		return err
	}
	return runBackup(ctx, in, req)
}

func backupOptions(ctx context.Context, req *engine.BackupRequest, full bool) error {
	req.Consistency = engine.ConsistencyPause
	req.Images = engine.ImagesApps
	fields := []huh.Field{
		huh.NewSelect[string]().
			Title("Running containers while their data is copied").
			Options(
				huh.NewOption("Pause them for a moment (recommended, safe for databases)", engine.ConsistencyPause),
				huh.NewOption("Stop and start them again (safest, short downtime)", engine.ConsistencyStop),
				huh.NewOption("Don't touch them (no interruption, databases may be inconsistent)", engine.ConsistencyLive),
			).Value(&req.Consistency),
		huh.NewSelect[string]().
			Title("Docker images").
			Options(
				huh.NewOption("Include application images (recommended: no rebuild on the new server)", engine.ImagesApps),
				huh.NewOption("Include all images (also for services/databases; works offline)", engine.ImagesAll),
				huh.NewOption("No images (smaller; the new server pulls/builds them)", engine.ImagesNone),
			).Value(&req.Images),
	}
	if full {
		fields = append(fields, huh.NewConfirm().
			Title("Include Coolify's own database backup files (/data/coolify/backups)?").
			Affirmative("Yes").Negative("No").Value(&req.IncludeBackups))
	}
	return huh.NewForm(huh.NewGroup(fields...)).WithTheme(theme()).WithShowHelp(true).RunWithContext(ctx)
}

func runBackup(ctx context.Context, in *coolify.Instance, req engine.BackupRequest) error {
	pr := engine.NewProgress("Backup")
	var res *engine.BackupResult
	title := "Creating backup"
	if req.Mode == engine.ModeFull {
		title = "Creating FULL backup"
	}
	err := runWithProgress(ctx, title, pr, func(ctx context.Context) error {
		var err error
		res, err = engine.Backup(ctx, in, req, pr)
		return err
	})
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString(sOK.Render("Backup complete") + "\n")
	b.WriteString(fmt.Sprintf("%s  %s\n", sMuted.Render("File "), res.Path))
	b.WriteString(fmt.Sprintf("%s  %s  %s\n", sMuted.Render("Size "), sBold.Render(engine.HumanBytes(res.Size)),
		sMuted.Render(fmt.Sprintf("(%s of data, %d resource(s), %s)", engine.HumanBytes(res.RawBytes), len(res.Manifest.Resources), engine.HumanDuration(res.Duration)))))
	b.WriteString(fmt.Sprintf("%s  %s", sMuted.Render("Key  "), res.Key))
	fmt.Println(boxed(sBox, b.String()))
	return offerShare(ctx, in, res.Path, res.Key)
}
