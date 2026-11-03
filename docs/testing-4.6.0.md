# Coolify 4.6.0 validation

Date: 2026-11-03. Tool: Coolify Mirror 1.10.1. Upstream: [Coolify v4.6.0](https://github.com/coollabsio/coolify/releases/tag/v4.6.0), released 2026-11-02.

## Environment and reproduction

The existing `coolify-compat` workflow runs `lab/e2e.sh` on the trusted Linux amd64 runner. Each mode installs the official Coolify image on two fresh Ubuntu 24.04 containers, each with systemd and its own Docker daemon and data volumes. Off-site mode adds a third SFTP storage container. These are real Coolify installations and real encrypted backups, not mocks; they are isolated lab servers, not production VPSs.

```sh
go vet ./...
go test ./...
scripts/build.sh
bash lab/e2e.sh 4.6.0 dist/coolify-mirror-linux-amd64
E2E_MODE=offsite bash lab/e2e.sh 4.6.0 dist/coolify-mirror-linux-amd64
```

Use a dedicated Docker test host: the lab owns the `cm-e2e-*` containers and volumes and removes them when finished. Do not run two copies concurrently. `E2E_BRANCH` selects the repository branch used by the Compose fixture; it defaults to `main`.

## Assertions

The source review compared upstream `v4.5.3` with `v4.6.0`: database migrations, `Application`, `ApplicationDeploymentJob`, and the Server Transfer exporter. The new Git password uses Laravel's encrypted cast; Mirror's existing column-independent decrypt/re-encrypt path handles it. New token scopes and paused state belong to the full database backup, not the selective resource graph. The exporter now includes the Git password in its official bundle; the existing bundle-presence and validation checks remain in force. No new backup format or replacement importer is needed.

| Test | What must be true |
| --- | --- |
| Selective backup/share/restore | The target reports `SUCCESS`; the official Server Transfer bundle is present; the Compose app starts from shipped images without a build. |
| Data integrity | PostgreSQL has all 1,234 rows and the same ordered content digest; nginx and Compose volume markers answer through Traefik; the runtime secret is unchanged. |
| HTTPS Git credentials, new in 4.6 | The target Laravel model decrypts the original password, the username matches, and the stored password remains encrypted. The unit test additionally proves that a selective move or copy re-encrypts with the target key and rejects the source key. |
| Hidden environment values | The original value and `is_shown_once` flag survive even though Coolify hides these values in resource-list API responses. |
| API access, new in 4.6 | Selective restore does not import the source token. Full restore preserves the fixture token's resource scope, read ability and paused state. |
| Off-site backup | Two full backups upload to SFTP via rclone; only one remains remotely and none locally after verified upload. An unknown remote is refused. |
| Scheduled backup | The systemd job runs, uploads and verifies a full backup. |
| Disaster recovery | The fresh target restores the newest remote backup using only the recovery key. Data, secrets and running resources pass the same checks. |
| Target panel | The panel answers on port 8000 and its own HTTPS domain; its target domain and IP are preserved. |
| TLS regression | A pin beginning with `+` survives raw or escaped fragment parsing. An incorrect certificate still fails pin verification; that integration test is repeated 50 times. |

## Evidence

| Run | Result |
| --- | --- |
| [Coolify 4.6.0, Mirror 1.10.1](https://github.com/mtalavi/coolify-mirror/actions/runs/41698355848) | Passed selective and off-site full restore. All 4.6 metadata checks passed. Selective completed 2026-11-03 19:03 UTC; off-site completed 19:13 UTC. |
| [Coolify 4.5.3 regression, Mirror 1.10.1](https://github.com/mtalavi/coolify-mirror/actions/runs/41699268675) | Passed selective and off-site full restore, including database digests and hidden values. Version-specific 4.6 fields were skipped as intended. Completed 19:34 UTC. |
| [Pull-request CI](https://github.com/mtalavi/coolify-mirror/actions/runs/41698626326) | Formatting, vet, all Go tests and both Linux builds passed. |
| [CodeQL](https://github.com/mtalavi/coolify-mirror/actions/runs/41698620545) | Go, Actions, JavaScript/TypeScript and Python analyses passed. |

Both end-to-end runs ended with `SUCCESS. Every resource is running, stable and reachable through the proxy.`, `OFF-SITE BACKUP AND RESTORE OK` and `COMPATIBLE`. Their results were published by the existing workflow to the `compat` branch. A first 4.6 attempt was cancelled while correcting the new fixture's explicit team selection; only the completed runs above are counted.

Local checks also passed on Go 1.26.5/Linux: `go vet ./...`, uncached `go test -count=1 ./...`, and 50 consecutive runs of `TestServerTokenAndRange`. The leading-plus regression covers both raw and URL-escaped pins. PHP and Bash syntax checks passed; the website's JSON-LD and 38 local links/assets were checked.

The earlier [1.10.0 daily run](https://github.com/mtalavi/coolify-mirror/actions/runs/41623785294) already passed the original selective and off-site suites on Coolify 4.6.0. It does not stand in for the expanded 1.10.1 tests above.

## Compatibility boundaries

- Source and target must have **exactly the same Coolify version**. This is not an in-place Coolify upgrade or a cross-version migration.
- Selective restore imports resources into the target team. Users, memberships and API tokens remain the target's; recreate resource access grants there. Full restore carries the source database, including these records.
- The HTTPS Git check proves credential storage and decryption, not a clone from a private third-party Git server. The lab repository is public. A rebuild against your private host requires its network access and valid credentials.
- The scoped-token check verifies preservation of stored scopes, abilities and paused state; it does not replace Coolify's authorization tests.
- The automated restore transfer uses direct pinned HTTPS; app and panel checks go through Traefik. This run does not re-test the older interactive walkthrough, clipboard, proxy-mode sharing, real email/Telegram delivery, every database engine or every cloud-storage provider.
- Both Linux amd64 and arm64 binaries are built; the end-to-end runtime is amd64. Coolify 4.5.3 was rerun for this patch; other older version results remain historical evidence.
- Backup format, exact-version checks, preflight, rollback and the CLI/menu workflow are unchanged. The runtime fix is confined to parsing a certificate pin whose first character was turned from `+` into a space by URL query parsing.
