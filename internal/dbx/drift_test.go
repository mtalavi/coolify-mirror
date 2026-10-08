package dbx

import "testing"

// Every table the importer handles has its reference columns listed.
func TestSchemaNeeds(t *testing.T) {
	n := SchemaNeeds()
	if len(n["applications"]) == 0 || len(n["environment_variables"]) != 2 {
		t.Fatalf("%v", n)
	}
	for tbl, cols := range n {
		for _, c := range cols {
			if c == "uuid" || c == "id" {
				t.Fatalf("%s.%s is not a reference column", tbl, c)
			}
		}
	}
	if plural("dragonfly") != "dragonflies" || plural("application") != "applications" {
		t.Fatal("plural")
	}
	if !driftIgnored("scheduled_task_executions") || driftIgnored("application_previews") || !driftIgnored("audit_events") {
		t.Fatal("ignored tables")
	}
	// Tables and columns only Coolify 4.4+ has are not required (4.3 lacks them).
	for _, tbl := range []string{"standalone_sqlites", "secret_manager_links", "integration_tokens"} {
		if _, ok := n[tbl]; ok {
			t.Errorf("%s must be optional", tbl)
		}
	}
	for _, c := range n["local_persistent_volumes"] {
		if c == "standalone_sqlite_id" {
			t.Error("local_persistent_volumes.standalone_sqlite_id must be optional")
		}
	}
}
