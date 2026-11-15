# Coolify 4.7.5 validation

Mirror 1.10.3 · 2026-11-15

## Upstream review

The [4.7.3–4.7.5 source comparison](https://github.com/coollabsio/coolify/compare/v4.7.3...v4.7.5) has no database migrations or changes to the Server Transfer exporter. [4.7.5](https://github.com/coollabsio/coolify/releases/tag/v4.7.5) fixes IPv6 connections for native SFTP backup destinations and reads valid grants from older API token records that contain unreadable scope entries. The compatibility review also found that selective backups carried the legacy primary-storage column but missed the two junction tables that link every destination to its database or volume schedule. Mirror 1.10.3 collects those links and their storage and key dependencies, remaps both foreign keys, and inserts the links after their parent rows. Older schemas continue to use the primary-storage column.

## Reproduce

Run the existing `coolify-compat` workflow with `coolify=4.7.5` and `force=true`. For a manual run on a dedicated Linux Docker test host, use the same lab in sequence:

```sh
go vet ./...
go test ./...
scripts/build.sh
bash lab/e2e.sh 4.7.5 dist/coolify-mirror-linux-amd64
E2E_MODE=offsite bash lab/e2e.sh 4.7.5 dist/coolify-mirror-linux-amd64
```

Each mode starts two isolated Ubuntu/systemd containers with their own Docker daemons and official Coolify installations. Selective migration uses an encrypted HTTPS share. Full recovery uses a third container for SFTP, rclone and a recovery key. The lab uses the same container names in both modes, so run them in sequence.

## Checks

- Both modes compare 1,234 PostgreSQL rows and their ordered content checksum, volume files, runtime secrets, restored Compose images and HTTP responses through Traefik.
- Both modes verify the SFTP destination's bracketed IPv6 address, custom port and encoded directory through the Coolify storage model, together with the SSH key, host pin and disabled database backup schedule.
- Both modes verify that disabled database and volume schedules resolve the primary destination first and keep a secondary SFTP destination whose key is used only there. Unit tests verify move, copy, skip, reuse of an existing target destination and insert order.
- The full restore preserves the legacy token's stored scopes and paused state. Coolify's edit-form data includes the valid resource grant and read permission while ignoring the invalid entry. Selective restore excludes this source token. The existing well-formed token and encrypted settings checks also run.
- Off-site mode verifies retention, a scheduled upload, recovery with the recovery key and preservation of the target panel's address and IP.

## Evidence

- [Automatic baseline with Mirror 1.10.2](https://github.com/mtalavi/coolify-mirror/actions/runs/43009444626) passed both modes on Coolify 4.7.5 on November 11.
- [Fresh run with the added checks](https://github.com/mtalavi/coolify-mirror/actions/runs/43699198826) uses Mirror 1.10.3 from commit `eb1631c663f3f3e0fc7d39a430a664d0c6f6123c`.

## Test scope

The IPv6 fixture exercises stored destination settings. Actual off-site transfer uses the lab's rclone/SFTP server. The token check calls Coolify's form-data parser. The suite covers self-hosted production resources and fresh, same-version restores. Preview deployments, Coolify Cloud backup imports, the native IPv6 network connection and vulnerability scan execution are outside this run. End-to-end execution is Linux amd64. Linux arm64 is covered by the build.
