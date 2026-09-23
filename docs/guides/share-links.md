# Share links

A read-only page for someone without the CLI — your wishlist before a birthday, or your whole collection — viewable
as many times as it's opened, until it expires. This is different from the export download links used elsewhere
([export & import](export-import.md), [Discord notifications](discord-notifications.md)): those are single-use
attachments consumed on first download; a share link is a page you can open again and again, like any other web
link, right up until it expires.

```bash
wms-go lego share wishlist --expires 168h      # a week (the default)
wms-go lego share collection --expires 48h
```

Needs `WMS_PUBLIC_URL` set, the same requirement as every other link/QR feature. The wishlist page reads your
BrickLink watch list (`wms lego watch`) — a set watch shows its catalog title, not just a bare number, so it means
something to whoever opens it.

Served at `/share/<token>` (a browser renders it inline), separate from `/dl/<token>` (a single-use download) — the
two token kinds are not interchangeable, so a wishlist link can't be replayed as a download or vice versa.
