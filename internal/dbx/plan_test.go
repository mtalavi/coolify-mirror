package dbx

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mtalavi/coolify-mirror/internal/coolify"
)

func num(n int) json.Number { return json.Number(itoa(n)) }

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func enc(plain string) map[string]any {
	return map[string]any{encKey: base64.StdEncoding.EncodeToString([]byte(plain))}
}

// A small export: project/env, a postgres and an app that references it.
func sampleExport() *Export {
	const appUUID, dbUUID = "appuuid00000000000000001", "dbuuid000000000000000001"
	ex := &Export{Version: FormatVersion, Tables: map[string][]coolify.Row{}}
	ex.Roots = []coolify.Resource{
		{Kind: "application", Table: "applications", ID: 7, UUID: appUUID, Name: "web", Status: "running:healthy"},
		{Kind: "postgresql", Table: "standalone_postgresqls", ID: 3, UUID: dbUUID, Name: "db", Status: "running:healthy"},
	}
	ex.Tables["projects"] = []coolify.Row{{"id": num(2), "uuid": "projuuid0000000000000001", "name": "Shop", "team_id": num(5)}}
	ex.Tables["environments"] = []coolify.Row{{"id": num(4), "uuid": "envuuid00000000000000001", "name": "production", "project_id": num(2)}}
	ex.Tables["standalone_dockers"] = []coolify.Row{{"id": num(0), "uuid": "dockeruuid00000000000001", "name": "localhost", "network": "coolify", "server_id": num(0)}}
	ex.Tables["applications"] = []coolify.Row{{"id": num(7), "uuid": appUUID, "name": "web", "environment_id": num(4),
		"destination_type": coolify.MorphStandaloneDocker, "destination_id": num(0),
		"source_type": coolify.MorphGithubApp, "source_id": num(0), "status": "running:healthy",
		"custom_labels": base64.StdEncoding.EncodeToString([]byte("coolify.applicationId=7\ntraefik.x=" + appUUID))}}
	ex.Tables["application_settings"] = []coolify.Row{{"id": num(9), "application_id": num(7)}}
	ex.Tables["standalone_postgresqls"] = []coolify.Row{{"id": num(3), "uuid": dbUUID, "name": "db", "environment_id": num(4),
		"destination_type": coolify.MorphStandaloneDocker, "destination_id": num(0), "postgres_password": enc("pw"), "status": "running:healthy"}}
	ex.Tables["environment_variables"] = []coolify.Row{{"id": num(11), "uuid": "envvaruuid00000000000001", "key": "DATABASE_URL",
		"value":             enc(`s:48:"postgres://u:p@` + dbUUID + `:5432/app";`),
		"resourceable_type": coolify.MorphApplication, "resourceable_id": num(7)}}
	ex.Tables["local_persistent_volumes"] = []coolify.Row{{"id": num(12), "uuid": "lpvuuid00000000000000001", "name": "postgres-data-" + dbUUID,
		"resource_type": "App\\Models\\StandalonePostgresql", "resource_id": num(3)}}
	ex.Tables["shared_environment_variables"] = []coolify.Row{{"id": num(13), "key": "CURRENCY", "type": "project", "team_id": num(5), "project_id": num(2), "value": enc("EUR")}}
	return ex
}

func target(existing map[string]map[string]int64) *TargetState {
	cols := map[string]map[string]string{}
	for _, t := range insertOrder {
		cols[t] = map[string]string{}
	}
	for t, rows := range sampleExport().Tables {
		for _, r := range rows {
			for k := range r {
				cols[t][k] = "text"
			}
		}
	}
	if existing == nil {
		existing = map[string]map[string]int64{}
	}
	return &TargetState{Columns: cols, TeamID: 0, Existing: existing, EnvsByProjectName: map[int64]map[string]int64{},
		EnvUUID: map[int64]string{}, Destinations: map[string]int64{"coolify": 0}, TagsByName: map[string]int64{}, SharedKeys: map[string]bool{}}
}

func allocator() Allocator {
	next := int64(100)
	return func(table string, n int) ([]int64, error) {
		out := make([]int64, n)
		for i := range out {
			next++
			out[i] = next
		}
		return out, nil
	}
}

func fakeEnc(b []byte) (string, error) { return "ENC(" + string(b) + ")", nil }

func TestPlanKeepUUIDRemapsIDs(t *testing.T) {
	ex := sampleExport()
	p, err := BuildPlan(ex, target(nil), func(coolify.Resource) Decision { return KeepUUID }, allocator(), fakeEnc)
	if err != nil {
		t.Fatal(err)
	}
	sql, err := p.SQL()
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Renames) != 0 {
		t.Fatalf("unexpected renames %v", p.Renames)
	}
	for _, want := range []string{`"postgres_password":"ENC(pw)"`, `"status":"exited"`, `"team_id":0`, `"destination_id":0`, `"source_id":0`} {
		if !strings.Contains(sql, want) {
			t.Errorf("SQL lacks %s", want)
		}
	}
	// application_settings.application_id and the env var must point to the new app id.
	var appID int64
	for _, pr := range p.rows {
		if pr.table == "applications" {
			appID = pr.newID
		}
	}
	for _, pr := range p.rows {
		switch pr.table {
		case "application_settings":
			if id, _ := Int64(pr.row["application_id"]); id != appID {
				t.Errorf("settings application_id=%d want %d", id, appID)
			}
		case "environment_variables":
			if id, _ := Int64(pr.row["resourceable_id"]); id != appID {
				t.Errorf("env resourceable_id=%d want %d", id, appID)
			}
		}
	}
	if len(p.Resources) != 2 || p.Resources[0].ID == 7 {
		t.Fatalf("resources %+v", p.Resources)
	}
}

func TestPlanCopyRenamesEverywhere(t *testing.T) {
	ex := sampleExport()
	existing := map[string]map[string]int64{
		"applications":           {"appuuid00000000000000001": 1},
		"standalone_postgresqls": {"dbuuid000000000000000001": 1},
		"projects":               {"projuuid0000000000000001": 50},
	}
	ts := target(existing)
	ts.EnvsByProjectName[50] = map[string]int64{"production": 60}
	ts.EnvUUID[60] = "targetenvuuid00000000001"
	ts.SharedKeys["project|50|CURRENCY"] = true
	p, err := BuildPlan(ex, ts, func(coolify.Resource) Decision { return NewCopy }, allocator(), fakeEnc)
	if err != nil {
		t.Fatal(err)
	}
	newDB := p.Renames["dbuuid000000000000000001"]
	newApp := p.Renames["appuuid00000000000000001"]
	if len(newDB) != 24 || len(newApp) != 24 || newDB == "dbuuid000000000000000001" {
		t.Fatalf("renames %v", p.Renames)
	}
	sql, err := p.SQL()
	if err != nil {
		t.Fatal(err)
	}
	// The env var (a PHP serialized string) must reference the copied database, with a correct length prefix.
	want := `ENC(s:48:\"postgres://u:p@` + newDB + `:5432/app\";)`
	if !strings.Contains(sql, want) {
		t.Errorf("env var not rewritten; want %s in\n%s", want, sql)
	}
	if !strings.Contains(sql, "postgres-data-"+newDB) {
		t.Error("volume name not renamed")
	}
	if strings.Contains(sql, `"name":"Shop"`) {
		t.Error("existing project should be reused, not inserted")
	}
	if strings.Contains(sql, "CURRENCY") {
		t.Error("existing shared variable must not be overwritten")
	}
	if !strings.Contains(sql, `"environment_id":60`) {
		t.Error("environment should map to the existing one by name")
	}
	if !strings.Contains(sql, `"name":"web (copy)"`) {
		t.Error("copy should be renamed")
	}
	var appID int64
	for _, pr := range p.rows {
		if pr.table == "applications" {
			appID = pr.newID
			labels, _ := base64.StdEncoding.DecodeString(pr.row["custom_labels"].(string))
			if !strings.Contains(string(labels), "coolify.applicationId="+itoa(int(appID))) || !strings.Contains(string(labels), newApp) {
				t.Errorf("custom labels not rewritten: %s", labels)
			}
		}
	}
}

func TestPlanSkip(t *testing.T) {
	ex := sampleExport()
	existing := map[string]map[string]int64{"applications": {"appuuid00000000000000001": 1}}
	p, err := BuildPlan(ex, target(existing), func(coolify.Resource) Decision { return Skip }, allocator(), fakeEnc)
	if err != nil {
		t.Fatal(err)
	}
	for _, pr := range p.rows {
		if pr.table == "applications" || pr.table == "application_settings" || pr.table == "environment_variables" {
			t.Fatalf("skipped app row %s still planned", pr.table)
		}
	}
	if len(p.Resources) != 1 || p.Resources[0].Table != "standalone_postgresqls" {
		t.Fatalf("resources %+v", p.Resources)
	}
}

func TestPHPUnserialize(t *testing.T) {
	if s, ok := phpUnserializeString([]byte(`s:5:"héllo";`)); ok {
		t.Fatalf("multibyte length must count bytes: %q", s)
	}
	if s, ok := phpUnserializeString([]byte(`s:6:"héllo";`)); !ok || s != "héllo" {
		t.Fatalf("%q %v", s, ok)
	}
}
