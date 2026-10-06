package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/mtalavi/coolify-mirror/internal/engine"

	"github.com/charmbracelet/huh"
)

// filesMenuName is how the other screens refer to the saved files screen.
const filesMenuName = "Saved files & disk space"

// savedFiles shows what coolify-mirror keeps on this server (backups,
// downloads, safety copies of restores, logs) and deletes what the user picks.
func savedFiles(ctx context.Context) error {
	list, err := withSpinner(ctx, "Reading what coolify-mirror keeps on this server", func(ctx context.Context) ([]engine.StoredFile, error) {
		return engine.ListStored(ctx)
	})
	if err != nil {
		return err
	}
	fmt.Println(boxed(sBox, storedSummary(list)))
	if len(list) == 0 {
		return nil
	}

	byName := map[string]engine.StoredFile{}
	var opts []huh.Option[string]
	for _, f := range list {
		byName[f.Name] = f
		opts = append(opts, huh.NewOption(storedLabel(f), f.Name))
	}
	height := len(opts) + 2
	if height > 16 {
		height = 16
	}
	var chosen []string
	ms := huh.NewMultiSelect[string]()
	ms.Title("What should be deleted?").
		Description("enter = the highlighted line · space = tick several, then enter · ctrl+a all · esc back").
		Options(opts...).
		Height(height).
		Value(&chosen).
		Validate(func(v []string) error {
			if len(v) == 0 {
				if h, ok := ms.Hovered(); ok {
					v = []string{h}
				}
			}
			for _, n := range v {
				if f := byName[n]; f.Busy != "" {
					return fmt.Errorf("%s is in use: %s", f.Name, f.Busy)
				}
			}
			return nil
		})
	if err := huh.NewForm(huh.NewGroup(ms)).WithTheme(theme()).WithKeyMap(listKeys()).WithShowHelp(true).RunWithContext(ctx); err != nil {
		return err
	}
	if len(chosen) == 0 {
		if h, ok := ms.Hovered(); ok {
			chosen = []string{h}
		}
	}
	var todo []engine.StoredFile
	for _, n := range chosen {
		todo = append(todo, byName[n])
	}
	if len(todo) == 0 {
		return errBack
	}

	var lines []string
	for _, f := range todo {
		lines = append(lines, fmt.Sprintf("• %s  %s  %s", engine.KindLabel(f.Kind), sBold.Render(engine.HumanBytes(f.Size)), f.About))
		for _, w := range engine.DeleteWarnings(f) {
			lines = append(lines, sWarn.Render("  ! "+w))
		}
	}
	lines = append(lines, sMuted.Render("This cannot be undone."))
	ok, err := confirm(ctx, fmt.Sprintf("Delete %d item(s) and free %s?", len(todo), engine.HumanBytes(engine.StoredTotal(todo))),
		strings.Join(lines, "\n"), false)
	if err != nil {
		return err
	}
	if !ok {
		fmt.Println(sMuted.Render("  Nothing was deleted."))
		return nil
	}

	var freed int64
	var failed []string
	for _, f := range todo {
		_, err := withSpinner(ctx, "Deleting "+f.Name, func(ctx context.Context) (struct{}, error) {
			return struct{}{}, engine.DeleteStored(ctx, f)
		})
		if err != nil {
			failed = append(failed, f.Name)
			continue
		}
		freed += f.Size
	}
	free, _ := engine.DiskSpace(engine.HomeDir)
	head := sOK.Render("Freed "+engine.HumanBytes(freed)) + sMuted.Render(" · "+engine.HumanBytes(free)+" free on the disk now")
	if len(failed) > 0 {
		head += "\n" + sErr.Render(fmt.Sprintf("%d item(s) were not deleted (see above)", len(failed)))
	}
	fmt.Println(boxed(sBox, head))
	if len(failed) > 0 {
		return errShown
	}
	return nil
}

// storedSummary: what is kept here, per kind, and the free disk space.
func storedSummary(list []engine.StoredFile) string {
	free, total := engine.DiskSpace(engine.HomeDir)
	disk := sMuted.Render(fmt.Sprintf("Disk: %s free of %s", engine.HumanBytes(free), engine.HumanBytes(total)))
	if len(list) == 0 {
		return sOK.Render("Nothing is kept here") + "\n" +
			sMuted.Render("No backups, downloads or safety copies - coolify-mirror uses no disk space on this server.") + "\n" + disk
	}
	var b strings.Builder
	b.WriteString(sTitle.Render("Saved by coolify-mirror on this server") + "  " + sBold.Render(engine.HumanBytes(engine.StoredTotal(list))) + "\n")
	kinds := []struct{ kind, label, hint string }{
		{engine.StoredBackup, "Backups made here", "delete one after the new server has restored it"},
		{engine.StoredDownload, "Downloaded backups", "kept because a restore did not finish"},
		{engine.StoredPartial, "Unfinished", "interrupted backups or downloads"},
		{engine.StoredSafety, "Safety copies", "what a restore replaced - delete once everything works"},
		{engine.StoredVolume, "Old volume data", "set aside by a restore - delete once everything works"},
		{engine.StoredLogs, "Logs", ""},
		{engine.StoredTemp, "Leftovers", "from interrupted runs"},
	}
	for _, k := range kinds {
		fs := engine.StoredOf(list, k.kind)
		if len(fs) == 0 {
			continue
		}
		n := fmt.Sprint(len(fs))
		if k.kind == engine.StoredLogs {
			n = "-"
		}
		b.WriteString(fmt.Sprintf("  %-19s %3s  %9s  %s\n", k.label, n, engine.HumanBytes(engine.StoredTotal(fs)), sMuted.Render(k.hint)))
	}
	b.WriteString(disk)
	return b.String()
}

// storedLabel is one line of the list: kind, size, date, what it is.
func storedLabel(f engine.StoredFile) string {
	date := "                "
	if !f.ModTime.IsZero() {
		date = f.ModTime.Format("2006-01-02 15:04")
	}
	l := fmt.Sprintf("%-11s %9s  %s  %s", engine.KindLabel(f.Kind), engine.HumanBytes(f.Size), date, f.About)
	switch f.Kind {
	case engine.StoredBackup, engine.StoredDownload, engine.StoredPartial:
		l += "  · " + f.Name
	}
	if len(f.Shares) > 0 {
		l += "  ⇄ shared now"
	}
	if f.Busy != "" {
		l += "  [in use]"
	}
	return truncate(l, termWidth()-10)
}

// usageLine is the one-line note at start: what coolify-mirror keeps here
// ("" when nothing).
func usageLine() string {
	n, size := engine.UsageSummary()
	if size < 1<<20 {
		return ""
	}
	what := engine.HumanBytes(size) + " kept by coolify-mirror"
	if n > 0 {
		what += fmt.Sprintf(" (%d backup(s))", n)
	}
	free, _ := engine.DiskSpace(engine.HomeDir)
	return sMuted.Render("  "+what+" · "+engine.HumanBytes(free)+" free · free space: menu → ") + sAccent.Render(filesMenuName)
}
