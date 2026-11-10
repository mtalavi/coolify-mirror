package dbx

import (
	"strings"
	"testing"

	"github.com/mtalavi/coolify-mirror/internal/coolify"
)

func TestSFTPStorageKeyDependency(t *testing.T) {
	for _, reused := range []bool{false, true} {
		ex := sampleExport()
		ex.Tables["scheduled_database_backups"] = []coolify.Row{{"id": num(21), "uuid": "backup47", "database_type": `App\Models\StandalonePostgresql`, "database_id": num(3), "s3_storage_id": num(22), "team_id": num(5)}}
		ex.Tables["s3_storages"] = []coolify.Row{{"id": num(22), "uuid": "storage47", "type": "sftp", "private_key_id": num(23), "team_id": num(5), "secret": enc("storage-password"), "sftp_host_key": "ssh-ed25519 fixture"}}
		ex.Tables["private_keys"] = []coolify.Row{{"id": num(23), "uuid": "key47", "team_id": num(5), "private_key": enc("ssh-key")}}
		ts := target(nil)
		for table, rows := range ex.Tables {
			for _, row := range rows {
				for column := range row {
					ts.Columns[table][column] = "text"
				}
			}
		}
		if reused {
			ts.Existing["private_keys"] = map[string]int64{"key47": 999}
		}
		p, err := BuildPlan(ex, ts, func(coolify.Resource) Decision { return KeepUUID }, allocator(), fakeEnc)
		if err != nil {
			t.Fatal(err)
		}
		keyID := p.idMap["private_keys"][23]
		if keyID == 0 || keyID == 23 || (reused && keyID != 999) {
			t.Fatalf("missing/remapping key: %d", keyID)
		}
		for _, pr := range p.rows {
			if pr.table == "s3_storages" {
				r, err := p.finalRow(pr)
				if err != nil {
					t.Fatal(err)
				}
				id, _ := Int64(r["private_key_id"])
				if id != keyID || r["secret"] != "ENC(storage-password)" || r["sftp_host_key"] != "ssh-ed25519 fixture" {
					t.Fatal("storage credentials or key reference lost")
				}
			}
		}
		sql, err := p.SQL()
		if err != nil {
			t.Fatal(err)
		}
		if !reused && strings.Index(sql, `INSERT INTO "private_keys"`) > strings.Index(sql, `INSERT INTO "s3_storages"`) {
			t.Fatal("storage inserted before its key")
		}
	}
}

func TestResourceScanSettingsMoveCopySkip(t *testing.T) {
	for _, decision := range []Decision{KeepUUID, NewCopy, Skip} {
		ex := sampleExport()
		ex.Tables["image_scan_policies"] = []coolify.Row{{"id": num(30), "uuid": "policy47", "scope_type": coolify.MorphApplication, "scope_id": num(7), "gate_mode": "off"}}
		ex.Tables["image_scan_ignores"] = []coolify.Row{{"id": num(31), "uuid": "ignore47", "resource_type": coolify.MorphApplication, "resource_id": num(7), "team_id": num(5), "created_by": num(44), "reason": "fixture"}}
		ts := target(map[string]map[string]int64{"applications": {ex.Roots[0].UUID: 88}})
		for _, table := range []string{"image_scan_policies", "image_scan_ignores"} {
			for column := range ex.Tables[table][0] {
				ts.Columns[table][column] = "text"
			}
		}
		p, err := BuildPlan(ex, ts, func(coolify.Resource) Decision { return decision }, allocator(), fakeEnc)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, pr := range p.rows {
			if pr.table != "image_scan_policies" && pr.table != "image_scan_ignores" {
				continue
			}
			count++
			r, err := p.finalRow(pr)
			if err != nil {
				t.Fatal(err)
			}
			col := "scope_id"
			if pr.table == "image_scan_ignores" {
				col = "resource_id"
				if r["created_by"] != nil {
					t.Fatal("source user reference carried")
				}
			}
			id, _ := Int64(r[col])
			if id != p.idMap["applications"][7] || id == 7 {
				t.Fatal("scan settings point at wrong resource")
			}
			if decision == NewCopy && r["uuid"] == ex.Tables[pr.table][0]["uuid"] {
				t.Fatal("copy kept scan row UUID")
			}
		}
		if (decision == Skip && count != 0) || (decision != Skip && count != 2) {
			t.Fatalf("scan row count: %d", count)
		}
	}
}
