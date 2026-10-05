package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mtalavi/coolify-mirror/internal/coolify"
	"github.com/mtalavi/coolify-mirror/internal/dbx"
	"github.com/mtalavi/coolify-mirror/internal/engine"
	"github.com/mtalavi/coolify-mirror/internal/transfer"

	"github.com/charmbracelet/huh"
)

func restoreFlow(ctx context.Context, in *coolify.Instance) error {
	restoreStarted = time.Now()
	link := ""
	err := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title("Paste the link shown on the source server").
			Description("It looks like https://1.2.3.4/cm/…/backup.cmb#key=…&pin=… (a local .cmb file path also works)").
			Value(&link).
			Validate(func(s string) error {
				loc, key, isURL, err := transfer.ParseSource(s)
				if err != nil {
					return err
				}
				if isURL && key == "" {
					return errors.New("the link must include the #key=… part at the end")
				}
				if !isURL && !strings.HasSuffix(loc, ".cmb") {
					return errors.New("paste the https://… link (or the path of a .cmb file)")
				}
				return nil
			}),
	)).WithTheme(theme()).RunWithContext(ctx)
	if err != nil {
		return err
	}

	key := ""
	if loc, k, isURL, _ := transfer.ParseSource(link); !isURL && k == "" {
		if _, err := os.Stat(loc + ".key"); err != nil {
			if err := huh.NewForm(huh.NewGroup(huh.NewInput().Title("Key of this backup file").
				Description("It was shown on the source server when the backup was created").Value(&key))).
				WithTheme(theme()).RunWithContext(ctx); err != nil {
				return err
			}
		}
	}
	pr := engine.NewProgress("Download")
	var f *engine.Fetched
	err = runWithProgress(ctx, "Getting the backup", pr, func(ctx context.Context) error {
		var err error
		f, err = engine.Fetch(ctx, link, strings.TrimSpace(key), pr)
		return err
	})
	if err != nil {
		return err
	}
	m := f.Manifest
	var b strings.Builder
	b.WriteString(sOK.Render("Backup verified") + "\n")
	b.WriteString(fmt.Sprintf("%s %s (%s) · Coolify %s · %s\n", sMuted.Render("From  "), m.Source.Hostname, m.Source.IPv4,
		m.Source.CoolifyVersion, m.CreatedAt.Local().Format("2006-01-02 15:04")))
	kind := "selected resources"
	if m.Mode == engine.ModeFull {
		kind = sWarn.Render("FULL server backup")
	}
	b.WriteString(fmt.Sprintf("%s %s · %d resource(s) · %s of data", sMuted.Render("Type  "), kind, len(m.Resources), engine.HumanBytes(m.TotalBytes)))
	for _, r := range m.Resources {
		b.WriteString("\n  • " + resourceLabel(r))
	}
	fmt.Println(boxed(sBox, b.String()))

	if m.Mode == engine.ModeFull {
		return restoreFull(ctx, in, f)
	}
	return restoreSelective(ctx, in, f)
}

var restoreStarted time.Time

func restoreSelective(ctx context.Context, in *coolify.Instance, f *engine.Fetched) error {
	if showBlockers(engine.PreflightSelective(ctx, in, f)) {
		return errBack
	}
	var teams []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	}
	team := int64(0)
	if err := in.Query(ctx, "SELECT id, name FROM teams ORDER BY id", &teams); err == nil && len(teams) > 1 {
		var opts []huh.Option[int64]
		for _, t := range teams {
			opts = append(opts, huh.NewOption(t.Name, t.ID))
		}
		team = teams[0].ID
		if err := huh.NewForm(huh.NewGroup(huh.NewSelect[int64]().Title("Restore into which team?").
			Options(opts...).Value(&team))).WithTheme(theme()).RunWithContext(ctx); err != nil {
			return err
		}
	}
	sr, err := withSpinner(ctx, "Checking this Coolify (encryption test, existing resources)", func(ctx context.Context) (*engine.SelectiveRestore, error) {
		return engine.PrepareSelective(ctx, in, f, team)
	})
	if err != nil {
		return err
	}
	decisions := map[string]dbx.Decision{}
	for _, c := range sr.Conflicts {
		d := dbx.NewCopy
		err := huh.NewForm(huh.NewGroup(huh.NewSelect[dbx.Decision]().
			Title(fmt.Sprintf("%s %q already exists on this server", c.Label(), c.Name)).
			Description("Probably restored here before.").
			Options(
				huh.NewOption("Restore it again as a separate copy (new IDs, both keep running)", dbx.NewCopy),
				huh.NewOption("Skip it (keep the existing one)", dbx.Skip),
			).Value(&d))).WithTheme(theme()).RunWithContext(ctx)
		if err != nil {
			return err
		}
		decisions[c.UUID] = d
	}
	if _, err := withSpinner(ctx, "Preparing the import (trial run in a rolled-back transaction)", func(ctx context.Context) (bool, error) {
		return true, sr.Build(ctx, decisions)
	}); err != nil {
		return err
	}
	if len(sr.Plan.Resources) == 0 {
		fmt.Println(sWarn.Render("  Nothing left to restore."))
		return errBack
	}
	var lines []string
	for _, r := range sr.Plan.Resources {
		lines = append(lines, "• "+r.Name+" ("+r.Label()+") → "+r.Project+"/"+r.Environment)
	}
	for _, n := range sr.Plan.Notes {
		lines = append(lines, sMuted.Render("note: "+n))
	}
	for _, v := range sr.ExistingVolumes {
		lines = append(lines, sWarn.Render("! volume "+v+" exists: it gets the backup's data, its current data is kept in an aside volume"))
	}
	for _, h := range sr.HostPaths {
		switch {
		case h.Shared:
			lines = append(lines, sWarn.Render("! host folder "+h.Path+" is shared with the original resource - the copy uses it as it is (not overwritten)"))
		case h.Exists:
			lines = append(lines, sWarn.Render("! host folder "+h.Path+" exists: its current content is moved aside, then the backup is restored there"))
		default:
			lines = append(lines, sMuted.Render("host folder "+h.Path+" will be created"))
		}
	}
	for _, d := range sr.DomainClashes {
		lines = append(lines, sWarn.Render("! domain "+d))
	}
	for _, w := range sr.CoolifyWarnings {
		lines = append(lines, sMuted.Render("coolify: "+w))
	}
	ok, err := confirm(ctx, "Restore these resources into this Coolify?", strings.Join(lines, "\n"), true)
	if err != nil {
		return err
	}
	if !ok {
		return errBack
	}
	pr := engine.NewProgress("Restore")
	var rep *engine.RestoreReport
	err = runWithProgress(ctx, "Restoring", pr, func(ctx context.Context) error {
		var err error
		rep, err = sr.Apply(ctx, pr)
		return err
	})
	if err != nil {
		return err
	}
	if f.Export != nil && len(f.Export.Tables["scheduled_tasks"])+len(f.Export.Tables["scheduled_database_backups"])+len(f.Export.Tables["scheduled_volume_backups"]) > 0 {
		rep.Notes = append(rep.Notes, "scheduled tasks/backups were restored and are active here too - disable them on the old server once you switch")
	}
	return startAndReport(ctx, in, f, rep)
}

func restoreFull(ctx context.Context, in *coolify.Instance, f *engine.Fetched) error {
	chk, err := engine.CheckFull(ctx, in, f)
	if err != nil {
		return err
	}
	if showBlockers(chk.Blockers) {
		return errBack
	}
	var warn []string
	warn = append(warn, "This empty Coolify becomes an exact copy of the source: its users, settings, keys and every resource.")
	if len(chk.ExistingVolumes) > 0 {
		warn = append(warn, fmt.Sprintf("%d volume(s) with the same name already exist here (%s): they get the backup's data and their current data is kept in *.cm-old-* volumes.",
			len(chk.ExistingVolumes), strings.Join(chk.ExistingVolumes, ", ")))
	}
	warn = append(warn, "Afterwards you log in with the users and passwords of the source server.")
	if chk.ArchProblem != "" {
		warn = append(warn, chk.ArchProblem)
	}
	for _, w := range chk.CoolifyWarnings {
		warn = append(warn, "Coolify: "+w)
	}
	if len(chk.RemoteServers) > 0 {
		warn = append(warn, "Remote servers "+strings.Join(chk.RemoteServers, ", ")+" will be managed by this Coolify too - shut down the old Coolify to avoid two controllers.")
	}
	warn = append(warn, "A safety copy of this server's current Coolify database and .env is made first.")
	fmt.Println(boxed(sErrBox, sWarn.Render("Full restore")+"\n"+wrap(strings.Join(warn, "\n"), termWidth()-8)))
	answer := ""
	err = huh.NewForm(huh.NewGroup(huh.NewInput().
		Title("Type yes to replace this Coolify with the backup").
		Value(&answer).
		Validate(func(s string) error {
			if strings.ToLower(strings.TrimSpace(s)) != "yes" {
				return errors.New("type yes to continue, or press esc to go back")
			}
			return nil
		}))).WithTheme(theme()).RunWithContext(ctx)
	if err != nil {
		return err
	}
	pr := engine.NewProgress("Full restore")
	var rep *engine.RestoreReport
	err = runWithProgress(ctx, "Restoring the whole Coolify", pr, func(ctx context.Context) error {
		var err error
		rep, err = engine.ApplyFull(ctx, in, f, pr)
		return err
	})
	if err != nil {
		return err
	}
	nin, err := withSpinner(ctx, "Reconnecting to the restored Coolify", func(ctx context.Context) (*coolify.Instance, error) {
		return coolify.Detect(ctx)
	})
	if err != nil {
		return err
	}
	rep.Notes = append(rep.Notes, "scheduled tasks and backups are active here too - disable them on the old server (or shut it down) once you switch")
	return startAndReport(ctx, nin, f, rep)
}

// showBlockers prints the preflight problems that forbid a restore.
func showBlockers(b []string) bool {
	if len(b) == 0 {
		return false
	}
	var lines []string
	for _, x := range b {
		lines = append(lines, "✗ "+x)
	}
	fmt.Println(boxed(sErrBox, sErr.Render("Restore not allowed")+"\n"+wrap(strings.Join(lines, "\n"), termWidth()-8)))
	return true
}

func startAndReport(ctx context.Context, in *coolify.Instance, f *engine.Fetched, rep *engine.RestoreReport) error {
	if err := askDomains(ctx, in, rep); err != nil {
		if errors.Is(err, context.Canceled) {
			return err
		}
		// The resources are restored; a clashing one simply stays stopped.
		fmt.Println(sWarn.Render("  ! domains were not changed: " + err.Error() + " - change them in Coolify"))
	}
	pr := engine.NewProgress("Start")
	var results []engine.StartResult
	err := runWithProgress(ctx, "Starting everything through Coolify", pr, func(ctx context.Context) error {
		var err error
		results, err = engine.StartResources(ctx, in, rep.Resources, pr)
		return err
	})
	if err != nil {
		return err
	}
	dash := dashboardBase(ctx, in)
	var b strings.Builder
	failed := 0
	for _, r := range results {
		if r.OK && r.Resource.Hold != "" {
			b.WriteString(sWarn.Render("! ") + r.Resource.Name + sWarn.Render(" · "+r.Message))
		} else if r.OK {
			b.WriteString(sOK.Render("✓ ") + r.Resource.Name + sMuted.Render(" · "+r.Message))
		} else {
			failed++
			b.WriteString(sErr.Render("✗ ") + r.Resource.Name + " · " + sErr.Render(r.Message))
		}
		for _, d := range r.Resource.Domains {
			b.WriteString(sMuted.Render("  " + coolify.Host(d)))
		}
		b.WriteString("\n")
	}
	for _, n := range rep.Notes {
		b.WriteString(sMuted.Render("note: "+n) + "\n")
	}
	head := sOK.Render("Restore complete") + sMuted.Render(" · "+engine.HumanDuration(time.Since(restoreStarted)))
	if failed > 0 {
		head = sWarn.Render(fmt.Sprintf("Restored, but %d resource(s) did not start", failed))
	}
	if failed == 0 && f.Downloaded {
		if err := os.Remove(f.Path); err == nil {
			b.WriteString(sMuted.Render("the downloaded backup copy was deleted (the source server still has it)") + "\n")
		}
	}
	b.WriteString("\n" + sBold.Render("Open Coolify: ") + dash)
	b.WriteString("\n" + sMuted.Render("Point your domains' DNS to this server when you are ready to switch."))
	fmt.Println(boxed(sBox, head+"\n"+b.String()))
	return nil
}

func dashboardBase(ctx context.Context, in *coolify.Instance) string {
	if fqdn, err := in.Scalar(ctx, "SELECT coalesce(fqdn, '') FROM instance_settings ORDER BY id LIMIT 1"); err == nil && fqdn != "" {
		return fqdn
	}
	v4, _ := in.PublicIPs(ctx)
	port := in.Env.Value("APP_PORT", "8000")
	return "http://" + v4 + ":" + port
}

// askDomains is the last step before anything starts: every domain of the
// restored resources is shown (pre-filled with the source value) to keep or
// change. An empty field removes the domain.
func askDomains(ctx context.Context, in *coolify.Instance, rep *engine.RestoreReport) error {
	fields, err := withSpinner(ctx, "Reading the restored domains", func(ctx context.Context) ([]engine.DomainField, error) {
		return engine.DomainFields(ctx, in, rep.Resources)
	})
	if err != nil {
		return err
	}
	if len(fields) == 0 {
		return nil
	}
	inputs := make([]huh.Field, 0, len(fields)+1)
	inputs = append(inputs, huh.NewNote().Title("Domains").
		Description("Last step before starting: keep or change each domain.\nSeveral domains: separate with commas. Empty = no domain. https:// is added when missing."))
	for i := range fields {
		f := &fields[i]
		inputs = append(inputs, huh.NewInput().
			Title(f.Label).
			Value(&f.Value).
			Validate(func(s string) error {
				_, err := engine.NormalizeDomains(s)
				return err
			}))
	}
	if err := huh.NewForm(huh.NewGroup(inputs...)).WithTheme(theme()).RunWithContext(ctx); err != nil {
		return err
	}
	for i := range fields {
		fields[i].Value, _ = engine.NormalizeDomains(fields[i].Value)
		if fields[i].Original == fields[i].Value {
			continue
		}
		fmt.Println(sMuted.Render("  "+fields[i].Label+": ") + strings.Join(hostList(fields[i].Original), ", ") + " → " + sOK.Render(strings.Join(hostList(fields[i].Value), ", ")))
	}
	_, err = withSpinner(ctx, "Saving the domains", func(ctx context.Context) (struct{}, error) {
		return struct{}{}, engine.ApplyDomains(ctx, in, rep.Resources, fields)
	})
	return err
}

func hostList(v string) []string {
	var out []string
	for _, d := range coolify.SplitDomains(v) {
		out = append(out, coolify.Host(d))
	}
	if len(out) == 0 {
		return []string{"(none)"}
	}
	return out
}
