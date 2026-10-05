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

- Backup files (`.cmb`) contain your Coolify secrets; they are encrypted with
  [age](https://age-encryption.org). Anyone with the file **and** its key can read them.
- Share links are temporary and token-protected, but plain HTTP by default: share
  over a private network or stop sharing once the target has downloaded the file.
