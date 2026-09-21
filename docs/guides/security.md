# Security

What is protected, how, and the honest limits. To report a problem: see `SECURITY.md` in the repository.

| Area | Protection |
|------|------------|
| Sign-in | Passwords checked against ModernWMS / Part-DB; single-use 2FA codes; lockout with escalating delay; idle lock |
| Audit log | Every line hash-chained; `wms audit verify` names the first edited, removed or reordered line; field values sanitised so nothing can forge an entry |
| Secrets | API keys and tokens in a mode-0600 settings file, entered masked, never logged; `doctor` flags loose permissions |
| Backups | age passphrase encryption; the plaintext is removed only after the sealed copy is proven to decrypt |
| Plugins | Locked folder, pinned SHA-256, scrubbed environment, unprivileged user, time limit, audited |
| Downloads | https only, allow-listed hosts, size and pixel caps; catalog refresh is all-or-nothing and refuses a suspiciously shrunken file |
| Updates | Checksum-verified, never restarts anything, keeps the old binary |
| Files you import | Size-capped, parsed strictly, written in one transaction after a dry-run plan |

## The limits

!!! warning "The audit chain is tamper-*evident*, not tamper-*proof*"
    The gateway runs as root, so someone with root can rewrite the log **and** recompute the chain, or drop the newest lines. Keep a copy of
    `wms audit head` where this machine can't write. [Alerts](alerts.md) send it daily.

- Telnet is not encrypted ([details](telnet-and-web.md#telnet-is-not-encrypted)).
- A plugin you enable with `--as-user root` has full control of the machine.
- `wms update` trusts GitHub's release hosts and the release's own checksums; verify provenance with
  `gh attestation verify <file> --repo KC-OU/KC-LEGO-CLI-NEW` if you need more.

## `wms doctor`

Run it after any change. It checks ModernWMS, the Part-DB file and token, the Rebrickable key, catalog age, clock synchronisation (2FA
depends on it), the gateway, file permissions, the audit chain, plugins and backups. It exits 1 on any FAIL, so a monitor can run it.
