# Report archive

Every report you generate — CLI or TUI, any of the [six report types](reports.md), sent to Discord or not — also
gets a permanent-ish copy, kept for 90 days: the frozen snapshot of what was generated, not a live-regenerated view.
This is separate from the normal export folder's own short retention (7 days by default), so "the QR/link expired,
can I still get that report" always has an answer.

## Browse it

```bash
wms-go lego report archive list          # your own archived reports
wms-go lego report archive list --all    # everyone's (same idea as admin-only browsing elsewhere)
wms-go lego report archive get <id>      # mint a fresh, viewable link for one
```

`archive get` doesn't reuse the archived copy's original link (that one's long gone) — it makes a **new** share link
(same multi-use, expiring kind as [share links](share-links.md)) pointing at it, default 24 hours
(`--expires` to change it).

In the interface: **Set Workshop → 7 Reports → 7 Archive**. Enter on a row mints a fresh link the same way, shown on
the same result screen every export already uses.

## Who can see what

Your own archived reports are yours; an admin can browse and retrieve everyone's (the same "your own, or an admin"
model already used for exports/downloads). A non-admin trying to fetch someone else's archived report by id is
refused, the same way a wrong download link is.

## Where it's stored

`WMS_ARCHIVE_DIR` (default `/root/docker-server/wms/archive`), tracked in a small database table alongside the
files. Nothing in it is deleted before 90 days; after that, both the database record and the file go together.
