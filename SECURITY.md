# Security policy

## Reporting a vulnerability

Please report security problems privately through GitHub: **Security > Report a vulnerability** on this repository
(a private advisory). Do not open a public issue for something exploitable. Expect an acknowledgement within a week.

## What is protected, and how

- **Sign-in and 2FA:** TOTP codes are single-use; five wrong codes lock the account (5 minutes, doubling to an hour).
- **Audit log:** every line is hash-chained (`wms audit verify`). The chain catches edits, deletions and reordering. It does
  **not** stop someone with root from recomputing it, so keep an off-machine copy of `wms audit head` (alerts can send it).
- **Telnet gateway:** at most four sessions per address, and repeated failed sign-ins block an address for a while.
  Telnet itself is not encrypted; use it only on a network you trust and use the web terminal over HTTPS elsewhere.
- **Secrets:** API keys and tokens live in a mode-0600 settings file, are entered masked, and are never written to the audit
  log. Backups can be encrypted (`wms backup --encrypt`, age).
- **Plugins:** programs run only from a root-owned, non-writable folder, after you enable them (their SHA-256 is pinned), with a
  scrubbed environment, a time limit, as an unprivileged user, and every run is audited.
- **Images and downloads:** only https from an allow-list of hosts, size- and pixel-capped.

`wms doctor` checks the parts of this that can drift (file permissions, the audit chain, plugin checksums, backup age, clock).

## Supported versions

Only the latest release receives fixes.
