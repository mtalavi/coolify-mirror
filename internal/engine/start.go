package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mtalavi/coolify-mirror/internal/coolify"
	"github.com/mtalavi/coolify-mirror/internal/dbx"
	"github.com/mtalavi/coolify-mirror/internal/docker"
	"github.com/mtalavi/coolify-mirror/internal/run"
)

// StartResult is the outcome of starting one resource.
type StartResult struct {
	Resource dbx.PlannedResource
	OK       bool
	Message  string
}

type startItem struct {
	res        dbx.PlannedResource
	step       *Step
	deployUUID string
	done       bool
	ok         bool
	msg        string
	deadline   time.Time
}

// StartResources asks Coolify to start/deploy the restored resources (databases
// first, then services, then applications) and waits until they run.
// Resources that were stopped on the source stay stopped.
func StartResources(ctx context.Context, in *coolify.Instance, resources []dbx.PlannedResource, pr *Progress) (out []StartResult, err error) {
	defer func() { pr.End(err) }()
	unlock, err := Lock()
	if err != nil {
		return nil, err
	}
	defer unlock()
	stServer := pr.Add("Check that Coolify can use this server", 0)
	stProxy := pr.Add("Check the proxy", 0)
	var dbs, svcs, apps []*startItem
	for _, r := range resources {
		it := &startItem{res: r}
		if r.Hold != "" {
			s := pr.Add("Start  "+r.Name, 0)
			s.SkipStep("domain in use")
			out = append(out, StartResult{Resource: r, OK: true, Message: r.Hold})
			continue
		}
		if !r.WasRunning {
			s := pr.Add("Start  "+r.Name, 0)
			s.SkipStep("was stopped on the source")
			out = append(out, StartResult{Resource: r, OK: true, Message: "left stopped (it was stopped on the source)"})
			continue
		}
		it.step = pr.Add(fmt.Sprintf("Start  %s (%s)", r.Name, r.Label()), 0)
		switch {
		case r.IsDatabase():
			dbs = append(dbs, it)
		case r.Table == "services":
			svcs = append(svcs, it)
		default:
			apps = append(apps, it)
		}
	}
	stServer.Begin("")
	if err := in.EnsureLocalServer(ctx); err != nil {
		stServer.Fail(err)
		return nil, err
	}
	stServer.Finish("ready")
	stProxy.Begin("")
	var px struct {
		Type    string `json:"proxy_type"`
		Started bool   `json:"started"`
	}
	if err := in.PHP(ctx, "ensure_proxy", nil, &px); err != nil {
		stProxy.Fail(err)
		pr.Warn("proxy check failed: %v", err)
	} else if px.Started {
		stProxy.Finish(px.Type + " started")
	} else {
		stProxy.Finish(px.Type + " running")
	}

	if err := dispatch(ctx, in, append(dbs, svcs...)); err != nil {
		return nil, err
	}
	waitAll(ctx, in, dbs, 5*time.Minute)
	if err := dispatch(ctx, in, apps); err != nil {
		return nil, err
	}
	waitAll(ctx, in, append(svcs, apps...), 25*time.Minute)

	for _, group := range [][]*startItem{dbs, svcs, apps} {
		for _, it := range group {
			out = append(out, StartResult{Resource: it.res, OK: it.ok, Message: it.msg})
		}
	}
	return out, nil
}

func dispatch(ctx context.Context, in *coolify.Instance, items []*startItem) error {
	if len(items) == 0 {
		return nil
	}
	var req []map[string]string
	for _, it := range items {
		m := map[string]string{"uuid": it.res.UUID, "table": it.res.Table}
		switch {
		case it.res.IsDatabase():
			m["type"] = "database"
		case it.res.Table == "services":
			m["type"] = "service"
		default:
			m["type"] = "application"
			if c := it.res.Commit; c != "" && c != "HEAD" {
				m["commit"] = c
			}
		}
		req = append(req, m)
		it.step.Begin("asking Coolify")
		it.deadline = time.Now().Add(25 * time.Minute)
	}
	var res struct {
		Items []struct {
			UUID       string `json:"uuid"`
			OK         bool   `json:"ok"`
			Error      string `json:"error"`
			Deployment string `json:"deployment_uuid"`
			Message    string `json:"message"`
		} `json:"items"`
	}
	if err := in.PHP(ctx, "start", req, &res); err != nil {
		for _, it := range items {
			it.step.Fail(err)
			it.done, it.msg = true, err.Error()
		}
		return nil
	}
	byUUID := map[string]*startItem{}
	for _, it := range items {
		byUUID[it.res.UUID] = it
	}
	for _, r := range res.Items {
		it := byUUID[r.UUID]
		if it == nil {
			continue
		}
		if !r.OK {
			msg := r.Error
			if msg == "" {
				msg = r.Message
			}
			it.step.Fail(fmt.Errorf("%s", msg))
			it.done, it.msg = true, msg
			continue
		}
		it.deployUUID = r.Deployment
		it.step.SetDetail("starting")
	}
	return nil
}

func waitAll(ctx context.Context, in *coolify.Instance, items []*startItem, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for {
		pending := 0
		for _, it := range items {
			if it.done {
				continue
			}
			pending++
			check(ctx, in, it)
			if !it.done && time.Now().After(deadline) {
				it.done, it.ok = true, false
				it.msg = "still not running after " + HumanDuration(timeout) + " - check it in Coolify"
				it.step.Fail(fmt.Errorf("%s", it.msg))
			}
		}
		if pending == 0 || ctx.Err() != nil {
			return
		}
		time.Sleep(3 * time.Second)
	}
}

func check(ctx context.Context, in *coolify.Instance, it *startItem) {
	if it.res.Table == "applications" && it.deployUUID != "" {
		st, _ := in.Scalar(ctx, "SELECT status FROM application_deployment_queues WHERE deployment_uuid = "+coolify.SQLString(it.deployUUID))
		switch st {
		case "queued":
			it.step.SetDetail("deployment queued")
			return
		case "in_progress":
			it.step.SetDetail("deploying")
			return
		case "failed", "cancelled-by-user":
			it.done, it.ok = true, false
			it.msg = "deployment " + st + " - open the deployment log in Coolify"
			it.step.Fail(fmt.Errorf("%s", it.msg))
			return
		case "finished":
			// fall through to container check
		default:
			it.step.SetDetail("deployment " + st)
		}
	}
	cs, err := docker.Containers(ctx, "label=com.docker.compose.project="+it.res.UUID)
	if err != nil || len(cs) == 0 {
		it.step.SetDetail("waiting for containers")
		return
	}
	running, healthy, total := 0, 0, 0
	for _, c := range cs {
		if pr := c.Label("coolify.pullRequestId"); pr != "" && pr != "0" {
			continue
		}
		total++
		if c.State == "running" {
			running++
			h := docker.Health(ctx, c.ID)
			if h == "" || h == "healthy" {
				healthy++
			} else if h == "unhealthy" {
				it.step.SetDetail(c.Names + " unhealthy")
			}
		}
	}
	if total > 0 && healthy == total {
		// StartService ends with "docker network connect <uuid> coolify-proxy
		// || true", which can fail silently right after the network is created;
		// the domain then answers 504. Repeat it (a no-op when attached).
		if it.res.Table == "services" && docker.NetworkExists(ctx, it.res.UUID) {
			if err := docker.Connect(ctx, it.res.UUID, "coolify-proxy"); err != nil {
				run.Logf("connect proxy to %s: %v", it.res.UUID, err)
			}
		}
		it.done, it.ok = true, true
		it.msg = "running"
		it.step.Finish(fmt.Sprintf("running (%d container%s)", total, plural(total)))
		return
	}
	it.step.SetDetail(fmt.Sprintf("%d/%d containers up", running, total))
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// DashboardLink builds the Coolify UI link of a resource.
func DashboardLink(base string, r dbx.PlannedResource) string {
	base = strings.TrimRight(base, "/")
	kind := "application"
	switch {
	case r.Table == "services":
		kind = "service"
	case r.IsDatabase():
		kind = "database"
	}
	return fmt.Sprintf("%s/project/%s/environment/%s/%s/%s", base, r.ProjectUUID, r.EnvironmentUUID, kind, r.UUID)
}
