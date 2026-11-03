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

Validation run links and final outcomes are recorded here after the runs complete.

The earlier [1.10.0 daily run](https://github.com/mtalavi/coolify-mirror/actions/runs/41623785294) already passed the original selective and off-site suites on Coolify 4.6.0. It does not stand in for the expanded 1.10.1 tests above.

## Compatibility boundaries

- Source and target must have **exactly the same Coolify version**. This is not an in-place Coolify upgrade or a cross-version migration.
- Selective restore imports resources into the target team. Users, memberships and API tokens remain the target's; recreate resource access grants there. Full restore carries the source database, including these records.
- The HTTPS Git check proves credential storage and decryption, not a clone from a private third-party Git server. The lab repository is public. A rebuild against your private host requires its network access and valid credentials.
- The scoped-token check verifies preservation of stored scopes, abilities and paused state; it does not replace Coolify's authorization tests.
- The automated restore transfer uses direct pinned HTTPS; app and panel checks go through Traefik. This run does not re-test the older interactive walkthrough, clipboard, proxy-mode sharing, real email/Telegram delivery, every database engine or every cloud-storage provider.
- Both Linux amd64 and arm64 binaries are built; the end-to-end runtime is amd64. Older version results remain historical evidence, not a claim that every version was rerun for this patch.
- Backup format, exact-version checks, preflight, rollback and the CLI/menu workflow are unchanged. The runtime fix is confined to parsing a certificate pin whose first character was turned from `+` into a space by URL query parsing.
