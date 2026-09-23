# Metrics (Prometheus)

The gateway can expose a `/metrics` endpoint for Prometheus — off by default, and separate from the telnet/web gateway
so a scrape never shares a port with anything public-facing.

## Enable it

Set `WMS_METRICS_PORT` (Admin → Settings, or directly in `settings.json`) and restart `wms-gateway.service`. It listens
on every interface (not just loopback), because a dockerised Prometheus reaches the host through
`host.docker.internal`, which cannot cross into host loopback — it is still never reverse-proxied or tunnelled out the
way telnet/web are.

```bash
wms doctor   # "Metrics" reports whether it's listening
curl 127.0.0.1:$WMS_METRICS_PORT/metrics
```

## What's counted

| Metric | Labels | Covers |
|---|---|---|
| `wms_audit_events_total` | `action`, `status` | every audited action — sign-ins (`LOGIN_*`), 2FA outcomes (`LOGIN_2FA_*`), exports (`EXPORT`), downloads (`EXPORT_DOWNLOAD`), and everything else `wms audit` already tracks |
| `wms_telnet_connections_total` | — | telnet sessions accepted |
| `wms_telnet_throttled_total` | — | telnet connections refused by the rate limiter |
| `wms_notify_deliveries_total` | `outcome` | notification send attempts (`success`/`failure`) |

Sign-in success vs. denied vs. locked, for example, are all `wms_audit_events_total` with different `action`/`status`
label values (`LOGIN_MODERNWMS`/`SUCCESS`, `LOGIN_DENIED_2FA_REQUIRED`/`DENIED`, and so on) rather than separate
metrics — the audit log already names every outcome, so the metric reuses it instead of duplicating the list.

## Wiring it into an existing Prometheus

```yaml
# docker-server/monitoring/docker-compose.yml — the prometheus service needs this to reach the host:
    extra_hosts:
      - "host.docker.internal:host-gateway"
```

```yaml
# docker-server/monitoring/prometheus/prometheus.yml
  - job_name: 'wms-gateway'
    metrics_path: '/metrics'
    static_configs:
      - targets: ['host.docker.internal:9107']   # match WMS_METRICS_PORT
```
