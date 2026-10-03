# Remote bot via Discord/Slack and n8n

This wires a Discord or Slack chat message to the [remote-bot API](remote-bot-api.md) through an
n8n workflow you build yourself — the trigger, parsing the command, the HTTP call, the reply. WMS
never talks to Discord or Slack directly; n8n receives the message and decides what it means, then
POSTs that decision to WMS as plain JSON. Nothing here is a workflow file to import — this guide
tells you what to build and why, and flags the one networking gotcha that will otherwise make it
silently fail.

If it's only ever going to be Discord (no Slack, and no other reason to run n8n), a second guide,
[Remote bot via a Discord slash command](remote-bot-discord.md), does the same four actions without
n8n at all — no workflow to maintain, no networking gotcha, just a `/wms` command Discord calls
directly.

**Do [the setup in the API guide](remote-bot-api.md#part-1--set-up) first** — a real Access Control
entry, a linked Discord/Slack ID, a bot PIN, and `WMS_BOT_API_PORT` turned on. Everything below
assumes that part is already done and working from `curl`.

## The one gotcha: container networking

`scripts/n8n/` runs n8n in Docker, on the default Compose network — **not** host networking. The
WMS bot API deliberately binds only to `127.0.0.1` on the host (see the API guide), and a service
bound to loopback is only reachable from processes sharing that exact network namespace. From
inside an ordinary container, `127.0.0.1` means the *container's own* loopback, not the host's —
there's no hostname trick (`host.docker.internal` included) that gets a container to a host service
bound to `127.0.0.1` specifically, because the socket itself never listens anywhere a bridged
container can reach.

The fix is to run n8n with host networking, so its `127.0.0.1` really is the host's. In
`scripts/n8n/compose.yml`, on the `n8n` service:

```yaml
  n8n:
    image: docker.io/n8nio/n8n:${N8N_VERSION}
    network_mode: host   # added — see docs/guides/remote-bot-n8n.md
    # ports: - '5678:5678'   # remove: host mode doesn't use a port mapping
    ...
```

Then `docker compose up -d` to pick it up. This only changes n8n's own networking — the WMS port
stays exactly as loopback-only as before; you're giving n8n a way in, not opening WMS up further.
If you'd rather not touch host networking at all, running n8n directly on the host (outside Docker)
reaches `127.0.0.1` the normal way too — your call, this is your n8n install to shape.

## Building the workflow

1. **A trigger node** — n8n's own Discord or Slack trigger, for whichever channel or DM you'll use.
2. **Something that decides the action.** A Code node is the simplest: parse the message text (say,
   `!reassign 42 pat`) into `{"action": "reassign_ticket", "params": {"ticket_id": 42, "to": "pat"}}`.
   The command syntax is entirely up to you — WMS never sees the raw message, only the finished
   JSON your workflow produces. This is the actual design work; there's no template for it here on
   purpose.
3. **An HTTP Request node**, `POST` to `http://127.0.0.1:$WMS_BOT_API_PORT/bot/action`:
   ```json
   {"platform": "discord", "id": "<your linked id>", "pin": "<your bot PIN>", "action": "{{ $json.action }}", "params": {{ $json.params }}}
   ```
   Store your id/PIN in an n8n credential or environment variable, not hardcoded in the node — the
   same way you'd handle any other secret in a workflow.
4. **Something that posts the response back** to Discord/Slack — a Respond node, or a message sent
   back through the trigger's platform, so you can actually see `{"ok": true, ...}` or the error.

Test with `list_open_tickets` first (read-only, nothing to undo) before wiring up anything that
changes state, and confirm the HTTP Request node itself succeeds (check its own execution log in
n8n) before worrying about whether the Discord/Slack side is wired right — that isolates the
networking gotcha above from everything else that could go wrong.

## Troubleshooting (n8n-specific)

| Symptom | Likely cause |
|---|---|
| The HTTP Request node times out or refuses, but `curl` from the host works fine | The container-networking gotcha above — n8n isn't on host networking yet |
| The trigger fires but nothing reaches WMS | Check the Code/parsing node's output matches the exact JSON shape the HTTP Request node expects — log it with a temporary node if unsure |
| WMS replies `401` every time from n8n but never from `curl` | The id/PIN stored in n8n's credential doesn't match what's linked — a stray space or the wrong field is a common cause |
| Everything else | See the [API guide's own troubleshooting table](remote-bot-api.md#troubleshooting) — once the request actually reaches WMS, every failure mode from here on is the same as calling it directly |
