# Security policy

## Reporting a vulnerability

Please **do not open a public issue** for security problems.
Report them privately through GitHub: **Security → Report a vulnerability**
(<https://github.com/mtalavi/coolify-mirror/security/advisories/new>).

Include the version (`coolify-mirror version`), the Coolify version and steps to
reproduce. You will get an answer within a few days; fixes are released as a new
version and credited in the advisory unless you prefer otherwise.

## Supported versions

Only the latest release receives security fixes.

## Scope notes

- Backup files (`.cmb`) contain Coolify configuration and secrets. They are
  encrypted with [age](https://age-encryption.org) using a random passphrase.
  Anyone who obtains **both** the backup file and its key can decrypt it.
- Share links are **HTTPS-only**. Each share gets a fresh self-signed TLS
  certificate and the destination verifies its pinned public key from the link.
  Plain `http://` share links are rejected.
- The decryption key and certificate pin live in the URL fragment
  (`#key=...&pin=...`). URL fragments are not sent to the share server.
- Share URLs also contain a random token and are temporary. Background shares
  expire after at most 24 hours; interactive shares stop when the operator quits.
- The local `<backup>.key` file is written with root-only permissions. Treat it
  like a secret and remove it when the backup is no longer needed.
