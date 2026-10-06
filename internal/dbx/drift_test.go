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
	if !driftIgnored("scheduled_task_executions") || driftIgnored("application_previews") {
		t.Fatal("ignored tables")
	}
}
