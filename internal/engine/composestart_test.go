package engine

import "testing"

func TestComposeStartCommand(t *testing.T) {
	f := composeFiles{Workdir: "/data/coolify/applications/u1"}
	svc := []string{"db", "web"}
	cases := []struct{ name, start, want string }{
		{"default", "", "docker compose --env-file /data/coolify/applications/u1/.env --project-name u1 --project-directory /data/coolify/applications/u1 -f /data/coolify/applications/u1/docker-compose.yaml up -d --no-build --no-deps db web"},
		{"custom with files", "docker compose --project-directory . --project-name u1 -f docker-compose.yaml -f extra.json up -d --no-build",
			"docker compose --env-file /data/coolify/applications/u1/.env --project-directory . --project-name u1 -f docker-compose.yaml -f extra.json up -d --no-build --no-deps db web"},
		{"custom without files", "docker compose -p u1 up -d",
			"docker compose -f /data/coolify/applications/u1/docker-compose.yaml --env-file /data/coolify/applications/u1/.env -p u1 up -d --no-build --no-deps db web"},
		{"not ending in up: run as it is", "docker compose up -d && docker compose exec web true",
			"docker compose -f /data/coolify/applications/u1/docker-compose.yaml --env-file /data/coolify/applications/u1/.env up -d && docker compose exec web true"},
	}
	for _, c := range cases {
		f.StartCommand = c.start
		if got := composeStartCommand(f, "u1", "/docker-compose.yaml", svc); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
	}
}

func TestComposeServices(t *testing.T) {
	images := map[string]string{"db": "postgres", "web": "u_web:c", "backup": "u_backup:c"}
	got := composeServices(images, []string{"web", "db", "web", "gone"})
	if len(got) != 2 || got[0] != "db" || got[1] != "web" {
		t.Fatalf("got %v", got)
	}
}
