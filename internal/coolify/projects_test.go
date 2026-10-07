package coolify

import (
	"strings"
	"testing"
)

func res(project, env, name, kind, status string, domains ...string) Resource {
	if kind == "app" {
		kind = "application"
	}
	return Resource{Kind: kind, Name: name, UUID: name + "-uuid", Project: project, ProjectUUID: project + "-p",
		Environment: env, EnvironmentUUID: project + "-" + env, Status: status, Domains: domains, ServerID: LocalServerID}
}

func TestGroupProjects(t *testing.T) {
	rs := []Resource{
		res("event manager", "production", "em", "app", "exited", "https://em.example.com"),
		res("Lift", "production", "lift-site", "app", "running:healthy", "https://lift.example.com"),
		res("event manager", "production", "phpmyadmin", "service", "running"),
		res("event manager", "production", "mysql-db", "mysql", "exited"),
		res("akt", "production", "akt", "app", "exited", "https://akt.example.com"),
		res("Shop", "staging", "shop-web", "app", "running", "https://staging.shop.example.com"),
		res("Shop", "production", "shop-web", "app", "running", "https://shop.example.com"),
	}
	ps := GroupProjects(rs)
	var got []string
	for _, p := range ps {
		got = append(got, p.Title())
	}
	want := "Lift|Shop · production|Shop · staging|event manager|akt"
	if strings.Join(got, "|") != want {
		t.Fatalf("order = %s, want %s", strings.Join(got, "|"), want)
	}
	em := ps[3]
	if len(em.Resources) != 3 || em.Running() != 1 {
		t.Fatalf("event manager: %d resources, %d running", len(em.Resources), em.Running())
	}
	if k := em.Kinds(); k != "app, service, mysql" {
		t.Errorf("kinds = %q", k)
	}
	if d := em.Domains(); len(d) != 1 || d[0] != "em.example.com" {
		t.Errorf("domains = %v", d)
	}
}

func TestProjectMainDomainFirst(t *testing.T) {
	p := Project{Resources: []Resource{
		res("menu", "production", "menu", "app", "running", "https://app-x.194.34.232.74.sslip.io", "https://www.menu.example.com", "https://menu.example.com"),
		res("menu", "production", "api", "app", "running", "https://api.menu.example.com:4000"),
	}}
	if d := p.Domains(); d[0] != "menu.example.com" || d[len(d)-1] != "app-x.194.34.232.74.sslip.io" {
		t.Errorf("domains = %v", d)
	}
}

func TestProjectRunState(t *testing.T) {
	p := func(statuses ...string) Project {
		var q Project
		for i, s := range statuses {
			q.Resources = append(q.Resources, res("x", "production", string(rune('a'+i)), "service", s))
		}
		return q
	}
	for want, q := range map[string]Project{
		"running": p("running", "running:healthy"),
		"partly":  p("degraded"),
		"stopped": p("exited", "exited:unhealthy"),
	} {
		if got := q.RunState(); got != want {
			t.Errorf("%v: %s, want %s", q.Resources, got, want)
		}
	}
	if q := p("running", "exited"); q.RunState() != "partly" || q.Running() != 1 {
		t.Errorf("one of two running: %s", q.RunState())
	}
}

func TestProjectKindsCounts(t *testing.T) {
	p := Project{Resources: []Resource{
		res("x", "production", "db", "postgresql", ""),
		res("x", "production", "a", "app", ""),
		res("x", "production", "b", "app", ""),
	}}
	if k := p.Kinds(); k != "2 apps, postgresql" {
		t.Errorf("kinds = %q", k)
	}
}

func TestMatchProject(t *testing.T) {
	ps := GroupProjects([]Resource{
		res("vemela.app", "production", "vemela", "app", "running", "https://api.vemela.app:4000", "https://vemela.app:3000"),
		res("Shop", "staging", "web", "app", "running", "https://staging.shop.example.com"),
		res("Shop", "production", "web2", "app", "running", "https://shop.example.com/store"),
	})
	find := func(name string) string {
		var out []string
		for _, p := range ps {
			if MatchProject(p, name) {
				out = append(out, p.Title())
			}
		}
		return strings.Join(out, ",") + "."
	}
	for name, want := range map[string]string{
		"vemela.app":               "vemela.app.",
		"api.vemela.app":           "vemela.app.",
		"https://vemela.app:3000":  "vemela.app.",
		"shop":                     "Shop · production,Shop · staging.",
		"Shop/staging":             "Shop · staging.",
		"shop.example.com":         "Shop · production.",
		"staging.shop.example.com": "Shop · staging.",
		"Shop-p":                   "Shop · production,Shop · staging.",
		"nothing.example.com":      ".",
		"":                         ".",
	} {
		if got := find(name); got != want {
			t.Errorf("%q matches %q, want %q", name, got, want)
		}
	}
}
