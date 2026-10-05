<p align="center">
  <img src="docs/banner.png" alt="Coolify Mirror — move Coolify projects between servers with one link" width="100%">
</p>

<p align="center">
  <a href="https://github.com/mtalavi/coolify-mirror/releases/latest"><img alt="release" src="https://img.shields.io/github/v/release/mtalavi/coolify-mirror?style=flat-square&color=8b5cf6"></a>
  <a href="https://github.com/mtalavi/coolify-mirror/actions/workflows/ci.yml"><img alt="ci" src="https://img.shields.io/github/actions/workflow/status/mtalavi/coolify-mirror/ci.yml?style=flat-square&label=tests"></a>
  <img alt="go" src="https://img.shields.io/badge/Go-1.26-00ADD8?style=flat-square&logo=go&logoColor=white">
  <img alt="coolify" src="https://img.shields.io/badge/Coolify-v4.3.x-6366f1?style=flat-square">
  <img alt="platform" src="https://img.shields.io/badge/linux-amd64%20%7C%20arm64-22c55e?style=flat-square">
  <a href="LICENSE"><img alt="license" src="https://img.shields.io/badge/license-MIT-a1a1aa?style=flat-square"></a>
</p>

<p align="center">
  <b>Back up one domain, a few, or a whole Coolify server — then restore it on another Coolify with a single pasted link.</b><br>
  Projects, environments, env vars, secrets, volumes, files, images and proxy settings arrive intact and start through Coolify itself.
</p>

<p align="center">
  <a href="#-install">Install</a> ·
  <a href="#-walkthrough">Walkthrough</a> ·
  <a href="#-how-it-works">How it works</a> ·
  <a href="#-command-line">CLI</a> ·
  <a href="#-safety">Safety</a> ·
  <a href="#-limitations">Limitations</a> ·
  <a href="README.fa.md">فارسی</a>
</p>

---

## ✨ Why

Coolify's own backup covers its database. Moving **one app** to a new server — with its database data, volumes, env vars, domains and the exact built image — is a manual afternoon. Coolify Mirror makes it one menu:

| | |
|---|---|
| 🎯 **Pick by domain** | Arrow keys + space. Databases and services an app depends on (via `DATABASE_URL`, etc.) are found and added for you. |
| 🔒 **Encrypted at rest and in transit** | The whole backup — `APP_KEY`, env vars, SSH keys, tokens, S3 keys, database passwords — is one [age](https://age-encryption.org)-encrypted file (passphrase, scrypt). It travels over **HTTPS with a pinned certificate**; the key and the pin live only in the link's `#fragment`. |
| 🔗 **One link transfer** | A temporary, random-token HTTPS link — through Coolify's own proxy on port 443 (TLS passthrough), or a direct port. Plain HTTP is never served. |
| 🗄️ **Native database dumps** | PostgreSQL, MySQL and MariaDB are saved with `pg_dumpall` / `mysqldump` while they keep running, and loaded with the same image on the target. |
| 🧾 **Coolify's own format inside** | Every backup also carries Coolify's official *Server Transfer* bundle (`schema_version 1`, made by Coolify's exporter) and the target validates it with Coolify's own validator. |
| ⚡ **No rebuild** | The exact image your app runs is shipped, so Coolify on the target logs *“Build step skipped”* and starts the same commit. |
| 🌐 **Domains last** | Right before anything starts you can keep or change every domain. |
| ♻️ **Preflight + rollback** | Exact-version check, conflict and domain detection, disk space, bundle validation and a trial import in a rolled-back transaction run **before** anything changes; a failure in the middle puts the target back the way it was. |

---

## 🚀 Install

On **each** Coolify server (as root):

```bash
curl -fsSL https://raw.githubusercontent.com/mtalavi/coolify-mirror/main/install.sh | sudo sh
```

The installer picks `amd64`/`arm64`, downloads the [latest release](https://github.com/mtalavi/coolify-mirror/releases/latest), verifies its SHA-256 and installs `/usr/local/bin/coolify-mirror`. Pin a version with `… | sudo VERSION=v1.1.0 sh`.

Then just run:

```bash
sudo coolify-mirror
```

> [!TIP]
> The target server doesn't even need the installer: after a backup, the source prints a one-line `curl … | sha256sum -c` command that downloads the tool **from the source server itself**.

<details>
<summary><b>Build from source</b></summary>

```bash
git clone https://github.com/mtalavi/coolify-mirror && cd coolify-mirror
scripts/build.sh            # → dist/coolify-mirror-linux-{amd64,arm64} + SHA256SUMS
scp dist/coolify-mirror-linux-amd64 root@SERVER:/usr/local/bin/coolify-mirror
```
Needs Go 1.26+. The binaries are static (no CGO) and have no runtime dependencies besides Docker and Coolify on the server.
</details>

---

## 🎬 Walkthrough

Real screenshots from two Coolify 4.3.23 servers (IP addresses replaced with documentation ranges).

### On the source server

**1 · Start** — the tool checks Docker, Coolify and its version.

<img src="docs/shots/01-menu.png" width="760" alt="main menu">

**2 · Pick what to move** — every resource with its domain, type, project and state. `space` selects, `/` filters, `ctrl+a` selects all.

<img src="docs/shots/02-select.png" width="760" alt="select domains">

**3 · Dependencies** — the Postgres that `shop-web` uses through `DATABASE_URL` is suggested automatically.

<img src="docs/shots/03-deps.png" width="760" alt="dependencies">

**4 · Options** — how to keep running databases consistent while their data is copied, and which Docker images to include.

<img src="docs/shots/04-options.png" width="760" alt="options">

| Consistency | What happens |
|---|---|
| **Pause** *(default)* | Containers using a volume are paused for the seconds it takes to copy it. Databases are dumped live and never paused. |
| **Stop** | Stopped and started again — safest, a short downtime. |
| **Live** | Nothing is touched; databases may be copied mid-write. |

| Images | What happens on the new server |
|---|---|
| **Application images** *(default)* | Apps built by Coolify start from the shipped image — no rebuild. Public images (postgres, wordpress…) are pulled. |
| **All images** | Everything is in the file — works without internet / Docker Hub. Bigger file. |
| **No images** | Smallest file; Coolify pulls everything and **rebuilds** apps from Git. |

**5 · Live progress** — every step with ✓ / ✗, bytes, speed and ETA.

<img src="docs/shots/05-progress.png" width="760" alt="progress">

**6 · Share** — an `https://` link appears (key + certificate pin after `#`). Paste it on the other server. The one-liner below it downloads the tool over the same pinned connection and checks its SHA-256.

<img src="docs/shots/07-share.png" width="760" alt="share link">

### On the target server

**7 · Paste the link** — choose *Restore a backup*.

<img src="docs/shots/08-paste.png" width="760" alt="paste link">

**8 · Verify** — the file is downloaded (resumable), decrypted and every checksum verified **before** anything changes.

<img src="docs/shots/09-summary.png" width="760" alt="summary">

**9 · Review** — conflicts, existing volumes, shared host folders and domain clashes are listed. Nothing has changed yet.

<img src="docs/shots/10-confirm.png" width="760" alt="confirm">

**10 · Domains, last** — keep or change every domain. Comma-separate several; empty removes it.

<img src="docs/shots/12-domains.png" width="760" alt="domains step">

**11 · Running** — Coolify itself starts databases, then services, then apps, and the tool waits until they are healthy.

<img src="docs/shots/13-complete.png" width="760" alt="restore complete">

Refresh Coolify — the project is there with all its environments, variables, storages, tags, scheduled tasks and backups.

---

## 🧠 How it works

```mermaid
flowchart LR
  subgraph S[Source server]
    A[Coolify DB<br/>rows of the picked resources] --> P
    B[Volumes + bind folders<br/>paused while copied] --> P
    C[Images<br/>docker save] --> P
    E[PostgreSQL / MySQL / MariaDB<br/>pg_dumpall · mysqldump] --> P
    O[Coolify's own transfer bundle<br/>schema_version 1] --> P
    P[[tar → zstd → age]] --> F[(backup.cmb)]
    F --> H{{temporary HTTPS link<br/>pinned self-signed cert}}
  end
  H -- "link + #key" --> D
  subgraph T[Target server]
    D[download · resume · verify] --> Q[preflight<br/>same version · conflicts · space<br/>Coolify validates the bundle]
    Q --> R[trial import<br/>in a rolled-back transaction]
    R --> X[restore files, volumes, images<br/>load database dumps]
    X --> I[import rows<br/>new IDs · re-encrypted secrets]
    I --> N[your domains]
    N --> Z[Coolify starts everything]
  end
```

**Selective backup** (one or more resources) exports their rows from Coolify's Postgres — application, service, database, environment variables, storages, tags, scheduled tasks, backup schedules, S3, keys, GitHub App, deployment snapshot — plus their volumes, file mounts and images. On restore, every row gets a fresh ID, every encrypted value (env vars, passwords, keys) is decrypted with the source key and re-encrypted with the target's `APP_KEY`, and the import runs in **one transaction**.

**Full backup** takes the whole Coolify: database dump, `.env` (including `APP_KEY`), SSH keys, proxy config and certificates, and every resource's data. A full restore is only allowed onto a **fresh, empty** Coolify; it still makes a safety copy and rolls back on any error. To move resources into a Coolify that already has projects, use a selective backup — it merges.

**Databases.** PostgreSQL, MySQL and MariaDB containers (standalone or inside a service) are saved with their native dump tool while they run. On restore their data volume is created empty, initialised by the same image with the same credentials in a temporary container (no network), the dump is loaded, and only then does Coolify start the real container. Other engines (Redis, MongoDB, ClickHouse…) are copied as files with the chosen consistency mode.

**Coolify's official format.** Coolify 4.3.23 ships an (internal) *Server Transfer* exporter/importer. It moves the Coolify records of a remote server between instances, but no data, and refuses the Coolify host itself. Every backup embeds that official bundle, produced by Coolify's own exporter code for the selected resources, and the target checks it with Coolify's `ServerTransferBundle::validate` and shows Coolify's own warnings (webhook URLs, credentials to refresh). The rows themselves are imported by this tool, because the official importer always creates a new server record instead of using the target's localhost.

| | Selective | Full |
|---|---|---|
| Use it for | moving / cloning projects | moving a whole server |
| Target Coolify | any — merges, existing resources are kept | **fresh and empty only** |
| Logins | target's users | source's users, API tokens, settings |
| Same resource already there | restore as copy, or skip | — |

---

## ⌨️ Command line

Everything in the menu also works headless, for scripts:

```bash
coolify-mirror list                                     # resources and domains
coolify-mirror backup --domain shop.com,blog.com        # selective (+ dependencies)
coolify-mirror backup --all                             # every resource
coolify-mirror backup --full                            # the whole Coolify
coolify-mirror backup --domain shop.com --serve --mode proxy
coolify-mirror serve FILE.cmb --detach                  # share for 24 h in the background
coolify-mirror restore 'http://…/backup.cmb#key=…'
coolify-mirror restore FILE.cmb --key KEY --yes \
  --set-domain shop.com=shop.new.com --set-domain www.shop.com=-
coolify-mirror start-all                                # ask Coolify to start anything stopped
```

<details>
<summary><b>All flags</b></summary>

| Command | Flag | Meaning |
|---|---|---|
| `backup` | `--domain a,b` / `--uuid u` / `--all` / `--full` | what to back up |
| | `--no-deps` | don't add databases/services the apps depend on |
| | `--consistency pause\|stop\|live` | see the table above |
| | `--images apps\|all\|none` | see the table above |
| | `--include-backups` | full: also `/data/coolify/backups` |
| | `--output DIR` | default `/data/coolify-mirror/backups` |
| | `--serve --mode proxy\|direct --port 8123 --host IP --open-firewall` | share right after (HTTPS; proxy = port 443) |
| `serve` | `--key`, `--mode`, `--port`, `--host`, `--open-firewall`, `--detach`, `--ttl 24h` | share an existing file |
| `restore` | `--key KEY` | when the link has no `#key=` |
| | `--yes` | no questions |
| | `--on-conflict copy\|skip` | resource already exists here |
| | `--team ID` | target team (selective) |
| | `--set-domain OLD=NEW` | change a domain; `NEW` = `-` removes it (repeatable) |
| | `--no-start` | restore but don't start |
| | `--keep-download` | keep the downloaded file |
</details>

---

## 🛡️ Safety

- **Everything sensitive is encrypted.** The backup is a single age-encrypted file (random 130-bit passphrase, scrypt): `APP_KEY`, environment variables and secrets, API tokens, private SSH keys, GitHub/GitLab app credentials, S3 access/secret keys, database users, passwords and connection strings, webhook secrets and registry logins never exist unencrypted outside the two servers. The key is written only to `<file>.key` (mode 600) and the link's `#fragment`.
- **No plain HTTP.** Shares are HTTPS only, with a self-signed certificate made for that share and pinned in the link; the downloader accepts exactly that public key (a mismatch aborts). Through Coolify's proxy the TLS connection is passed through untouched (SNI routing), so it ends in this tool, not in Traefik. Links use a random 128-bit token and stop when you quit (or after `--ttl`, 24 h max in the background).
- **Same Coolify version only.** Restores require exactly the same Coolify version on both servers.
- **Full restore only onto an empty Coolify.** A target with projects, resources, extra servers or S3 storages is refused — use a selective (merge) restore.
- **Mandatory preflight.** Version, disk space, Coolify's bundle validation, conflicts (same resources, volumes, host folders, domains) and a trial import in a rolled-back transaction run before anything is written. `--yes` only skips the confirmation, never these checks.
- **Rollback.** Selective: everything created is removed if the import fails. Full: the previous Coolify database and `.env` are restored and its containers started again.
- **Nothing is deleted.** Volumes that already exist on the target keep their old data in `<name>.cm-old-<time>`; replaced folders go to `/data/coolify-mirror/replaced-<time>`.
- **One run at a time.** A lock prevents two backups/restores on a server. Paused containers are always resumed — even after `Ctrl+C` or a crash.
- **Logs** of every run: `/data/coolify-mirror/logs/` (no secrets).

> [!IMPORTANT]
> Run it inside `tmux`/`screen` on big servers so an SSH disconnect doesn't interrupt a long copy.

---

## ⚠️ Limitations

- Coolify **v4.3.x**; both servers must run **exactly the same** version (upgrade the older one first).
- MySQL/MariaDB dumps contain the user databases; extra database users created by hand (beyond the image's `MYSQL_USER`) are not recreated. PostgreSQL dumps include all roles.
- Resources running on **remote servers**, **preview deployments** and **Swarm** are not part of a selective backup.
- **GitHub webhooks**, **DNS** and **scheduled tasks/backups** still point to / run on the old server — switch them when you move.
- Different CPU architecture (amd64 → arm64): shipped images can't be used, Coolify rebuilds.

---

## 🧪 Tested

On real Coolify 4.3.23 installs (lab in [`lab/`](lab)): native Postgres/MariaDB dumps (identical row counts and checksums after restore), HTTPS share through Traefik passthrough with pin check (a wrong pin is refused, plain HTTP gets nothing), full restore refused on a non-empty Coolify and accepted on a fresh one, Postgres/MariaDB/Redis data, WordPress, compose apps, Git apps without rebuild, copies next to originals, restore into a fresh never-used Coolify, full server restore with rollback (fault injection), interrupted backups, domain changes at the end, plus unit tests for Laravel encryption, the archive format, the import planner and resumable downloads.

---

## 🧩 Project layout

```
cmd/coolify-mirror     CLI + entry point
internal/ui            interactive menu (Bubble Tea, huh, lipgloss)
internal/engine        backup, share, fetch, selective/full restore, start, domains
internal/dbx           export + import planner for Coolify's database
internal/coolify       Coolify detection, psql, PHP runner, Laravel encryption
internal/archive       tar + zstd + age stream format
internal/transfer      share server + resumable download
lab/                   Docker lab with real Coolify servers
```

Contributions and issues are welcome. **Not affiliated with Coolify / coolLabs** — “Coolify” is their trademark.

## 📄 License

[MIT](LICENSE)
