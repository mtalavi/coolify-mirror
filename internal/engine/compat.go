package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mtalavi/coolify-mirror/internal/coolify"
	"github.com/mtalavi/coolify-mirror/internal/dbx"
)

// Coolify updates often. The tool relies on parts of Coolify (database
// tables, PHP classes and methods), so before a backup or restore it checks
// that this Coolify still has every one of them, and looks up the published
// result of the automatic compatibility test (the "compat" branch, written by
// .github/workflows/compat.yml for every new Coolify release). The outcome:
//
//   - something the tool cannot work without is missing, or the test failed
//     for this Coolify version: the restore is refused before anything changes,
//     with what is missing and how to update;
//   - an optional part is missing: that feature falls back (and says so);
//   - the version is just not tested yet: a warning - every safety check
//     (trial import, rollback, verification of the result) still runs.

// TestedCoolify lists the Coolify versions this release was tested with.
var TestedCoolify = []string{"4.3.23"}

// RepoURL is the project's home page.
const RepoURL = "https://github.com/mtalavi/coolify-mirror"

// SiteURL is the website: the guide with a screenshot of every screen.
const SiteURL = "https://coolify-mirror.pages.dev"

// CompatURL holds the results of the automatic compatibility test.
var CompatURL = "https://raw.githubusercontent.com/mtalavi/coolify-mirror/compat/compat.json"

// Compat is what CheckCompat found for one Coolify.
type Compat struct {
	CoolifyVersion string
	Status         string // "tested", "untested", "failed"
	Blockers       []string
	Warnings       []string
}

// CompatEntry is one Coolify version in compat.json.
type CompatEntry struct {
	Status string `json:"status"` // "ok" or "fail"
	Tool   string `json:"tool"`   // coolify-mirror version that was tested
	Date   string `json:"date"`
	Detail string `json:"detail,omitempty"`
}

var featureEffect = map[string]string{
	"no_rebuild":      "Coolify may rebuild Dockerfile and git applications on their first start instead of reusing the restored image",
	"compose_start":   "Docker Compose applications are deployed by Coolify (clone and build) instead of starting from the restored images",
	"domains":         "domain changes in the last step may not update everything - check the domains in Coolify afterwards",
	"transfer_bundle": "Coolify's own transfer bundle is not made (it is only used as an extra check)",
}

var (
	compatMu    sync.Mutex
	compatCache = map[string]*Compat{}
)

// CheckCompat checks this Coolify (once per version and run).
func CheckCompat(ctx context.Context, in *coolify.Instance) *Compat {
	compatMu.Lock()
	defer compatMu.Unlock()
	if c, ok := compatCache[in.Version]; ok {
		return c
	}
	c := &Compat{CoolifyVersion: in.Version, Status: "untested"}
	for _, v := range TestedCoolify {
		if v == strings.TrimPrefix(in.Version, "v") {
			c.Status = "tested"
		}
	}
	update := "update coolify-mirror (sudo coolify-mirror update) - if it is already the latest, please open an issue at " + RepoURL + "/issues"

	var caps struct {
		Missing map[string][]string `json:"missing"`
	}
	if err := in.PHP(ctx, "capabilities", nil, &caps); err != nil {
		c.Blockers = append(c.Blockers, "the coolify-mirror helper cannot run inside this Coolify ("+firstLine(err.Error())+") - "+update)
	} else {
		if m := caps.Missing["core"]; len(m) > 0 {
			c.Blockers = append(c.Blockers, fmt.Sprintf("Coolify %s no longer has %s, which coolify-mirror needs - %s", in.Version, strings.Join(m, ", "), update))
		}
		for _, f := range sortedFeatures(caps.Missing) {
			if f != "core" && len(caps.Missing[f]) > 0 {
				c.Warnings = append(c.Warnings, fmt.Sprintf("Coolify %s changed %s: %s", in.Version, strings.Join(caps.Missing[f], ", "), featureEffect[f]))
			}
		}
	}

	if schema, err := dbx.SchemaColumns(ctx, in); err == nil {
		var missing []string
		for t, cols := range dbx.SchemaNeeds() {
			if schema[t] == nil {
				missing = append(missing, "table "+t)
				continue
			}
			for _, col := range cols {
				if !schema[t][col] {
					missing = append(missing, t+"."+col)
				}
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			c.Blockers = append(c.Blockers, fmt.Sprintf("Coolify %s changed its database (%s missing) - %s", in.Version, strings.Join(missing, ", "), update))
		}
	}

	if e, ok := lookupCompat(ctx, in.Version); ok {
		switch {
		case e.Status == "ok":
			c.Status = "tested"
		case e.Status == "fail" && CompareVersions(Version, e.Tool) <= 0:
			c.Status = "failed"
			c.Blockers = append(c.Blockers, fmt.Sprintf("the automatic test of coolify-mirror %s with Coolify %s failed (%s) - %s", e.Tool, in.Version, e.Detail, update))
		case e.Status == "fail":
			c.Warnings = append(c.Warnings, fmt.Sprintf("coolify-mirror %s did not work with Coolify %s; this newer version is not tested with it yet", e.Tool, in.Version))
		}
	}
	if c.Status == "untested" && len(c.Blockers) == 0 {
		c.Warnings = append(c.Warnings, fmt.Sprintf("Coolify %s is not tested with coolify-mirror %s yet - every safety check still runs (trial import, rollback, verification of the result)", in.Version, Version))
	}
	compatCache[in.Version] = c
	return c
}

func sortedFeatures(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// lookupCompat reads this Coolify version's test result (best effort: no
// network means no entry).
func lookupCompat(ctx context.Context, version string) (CompatEntry, bool) {
	var doc struct {
		Coolify map[string]CompatEntry `json:"coolify"`
	}
	if err := getJSON(ctx, CompatURL, &doc); err != nil {
		return CompatEntry{}, false
	}
	e, ok := doc.Coolify[strings.TrimPrefix(version, "v")]
	return e, ok
}

func getJSON(ctx context.Context, url string, dst any) error {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "coolify-mirror/"+Version)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(dst)
}

// CompareVersions compares dotted versions numerically ("4.3.9" < "4.3.23"):
// -1, 0 or 1.
func CompareVersions(a, b string) int {
	return compareVersions(a, b)
}

func compareVersions(a, b string) int {
	pa, pb := versionParts(a), versionParts(b)
	for i := 0; i < len(pa) || i < len(pb); i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

func versionParts(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	var out []int
	for _, p := range strings.FieldsFunc(v, func(r rune) bool { return r == '.' || r == '-' }) {
		n, err := strconv.Atoi(strings.TrimLeft(p, "abcdefghijklmnopqrstuvwxyz"))
		if err != nil {
			continue
		}
		out = append(out, n)
	}
	return out
}
