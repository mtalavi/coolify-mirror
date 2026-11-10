# Coolify 4.7.3 validation

Date: 2026-11-10. Coolify Mirror: 1.10.2. Upstream: [v4.7.3](https://github.com/coollabsio/coolify/releases/tag/v4.7.3).

## Changes reviewed

Compared the upstream v4.6.0 and v4.7.3 database migrations, application models, deployment job and Server Transfer exporter/bundle. Coolify now stores SFTP destinations in `s3_storages`, with a nullable `private_key_id`. Mirror must collect that key, remap its ID and insert it before the destination. The reference is optional so older schemas continue to work.

Resource-owned `image_scan_policies` and `image_scan_ignores` are collected and remapped with their resources. Copy restores get new UUIDs; skipped resources do not bring their scan rows. Selective restores clear the ignore author's source user reference. Instance/team/server policies and team-wide ignores stay with the target; the backup warns about this inheritance. Full restores carry the database. GitHub deployment IDs are external IDs and are no longer misreported as unknown local references.

## Reproduction

Use the existing trusted Linux runner and `lab/e2e.sh`; do not run simultaneous lab copies because it owns the `cm-e2e-*` containers and volumes.

```sh
go vet ./...
go test ./...
scripts/build.sh
bash lab/e2e.sh 4.7.3 dist/coolify-mirror-linux-amd64
E2E_MODE=offsite bash lab/e2e.sh 4.7.3 dist/coolify-mirror-linux-amd64
```

Each mode installs two real Coolify instances on fresh Ubuntu/systemd containers with their own Docker daemons. Off-site mode adds a third SFTP server. These are actual encrypted backups and restores on isolated test installations, not production servers or mocks.

## Assertions

- Existing suite: official transfer bundle, selective HTTPS sharing, full recovery with a recovery key via rclone/SFTP, scheduled backup, retention, PostgreSQL's 1,234 rows and ordered digest, volume markers, runtime secrets, Compose start without rebuild, HTTP through Traefik, and the target panel's address/IP.
- Existing 4.6 checks: encrypted HTTPS Git credentials, hidden environment variables and scoped/paused API token records.
- New 4.7 checks: a key used only by an SFTP destination, key fingerprint and encrypted credential readability, saved host pins, the storage/database link and disabled schedule, Git SSH known hosts, multiline bytes including the final newline, and resource-specific vulnerability policy/ignore records.
- Unit regressions: destination-to-key remapping and insertion order, reuse of an existing target key, and move/copy/skip handling of scan settings without retaining the source author ID.

## Evidence

- [Coolify 4.7.3 real restore run](https://github.com/mtalavi/coolify-mirror/actions/runs/42801124062): passed both selective and off-site full recovery on 2026-11-10. Both modes verified the database content digest, SFTP/key/schedule links, multiline bytes and resource scan settings. Full recovery also verified the scheduled upload, retention and target panel identity.

- [Coolify 4.6.0 regression run](https://github.com/mtalavi/coolify-mirror/actions/runs/42801171452): passed selective and off-site full recovery on 2026-11-10, including database content checks, volumes, runtime secrets, Git credentials, scoped/paused token handling, scheduled uploads and panel identity. The 4.7-only fixtures are gated by schema availability.
- Local formatting, vet, the full Go suite, PHP/Bash syntax, Linux amd64/arm64 builds and website checks passed. PR CI and CodeQL validate the release changes as well.

Both real runs built commit `feeeaef3ac099ad6cdda428a0b94806f4ddc44f3`. Later changes update documentation, the built-in tested-version list and stricter unit-test assertions; they do not change the backup/restore implementation or lab fixture. Final release CI also runs the full Go suite.

## Boundaries

The SFTP destination fixture proves preservation of Coolify's destination configuration, key and schedule. The actual off-site data transfer is the existing rclone/SFTP path; this does not test every native Coolify storage provider. The SSH host pins are stored fixtures, not a new external Git-server connection. Multiline values are compared through Laravel, not a deployed private-key consumer. Scan policies/ignores are checked as configuration; this suite does not run Syft/Grype or prove a vulnerability gate blocks deployment. Inherited scan policy must be reviewed on the target before a later deployment.

Both Linux architectures are built; end-to-end execution is amd64. Source and target must run exactly the same Coolify version. The format, menus, preflight and rollback mechanism are unchanged. Interactive clipboard behavior, every database engine, email/Telegram delivery and every cloud-storage provider are outside this run.
