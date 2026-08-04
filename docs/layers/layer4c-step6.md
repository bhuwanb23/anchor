# Layer 4C — Step 6: Alert Delivery

## Delivery paths

| Path       | MVP | Responsibility                                     |
| ---------- | --- | -------------------------------------------------- |
| Dashboard  | ✅  | always — live WS push + persisted history + bell   |
| Email      | ✅  | control plane only, via SMTP (stdlib `net/smtp`)   |
| WhatsApp   | 🚧  | post-MVP — architecture designed below             |

The agent never sends email or WhatsApp. It pushes one `anomaly_alert` message
over its WebSocket; the control plane decides how (and whether) to deliver it.

## Severity rules (Step 6A)

| Severity  | Dashboard | Email                                            | WhatsApp (post-MVP) |
| --------- | --------- | ------------------------------------------------ | ------------------- |
| CRITICAL  | immediate | immediate                                        | immediate           |
| WARNING   | immediate | hourly digest (batched)                          | not sent            |
| RESOLVED  | immediate | next hourly digest, only within working hours    | only for critical   |

Email queue lives in the `alert_emails` table:

- One job per `(alert_id, severity, status)` — escalations and resolutions
  insert a fresh job (the user is re-notified); identical re-fires update the
  pending job instead of duplicating.
- Critical jobs are delivered by a worker that wakes every 20s.
- Warning/resolved jobs wait until they are at least one hour old, then a
  single **digest email per recipient** summarizes them; a job stuck for more
  than a week is dropped.

Non-blocking by construction: enqueueing is one fast DB write from the WS
read goroutine; all SMTP I/O happens in background workers (`Delivery.Run`).

## Configuration

```bash
EMAIL_ENABLED=true
SMTP_HOST=smtp.postmarkapp.com   # any provider's SMTP works
SMTP_PORT=587
SMTP_USER=...
SMTP_PASS=...
SMTP_FROM=alerts@yourplatform.app
# ALERT_EMAIL_TO=ops@example.com   # optional override; default = server owner
ALERT_WORK_HOUR_START=9           # resolved-alert emails only between these
ALERT_WORK_HOUR_END=18            # local hours
```

## WhatsApp (post-MVP) — architecture design

Post-MVP delivery is a *second `Notifier`*, not a rewrite. The existing
`alerts.Delivery` applies severity rules once and fans out to senders; WhatsApp
only needs a new implementation of the same seam.

### Channel adapter interface

```go
// One per channel: Email (mailer.Sender today), WhatsApp tomorrow.
type Notifier interface {
    Notify(to string, subject string, a queries.AlertRecord) error
}
```

### Recommended provider: WhatsApp Business API (Cloud API)

- **Gateway**: Meta WhatsApp Cloud API (`graph.facebook.com/v21.0/{phone-id}/messages`)
  — a simple HTTPS POST with a bearer token. No SDK required; the control
  plane already talks HTTP.
- **Number linking**: user scans a QR code in Settings → "Connect WhatsApp" →
  Meta returns a `waba_phone_number_id`; the control plane stores it per user
  in a new `user_notification_channels` table.
- **Template messages**: WhatsApp requires pre-approved message templates for
  business-initiated chats, e.g.:

  ```
  {{1}} — YourPlatform alert
  {{2}} is {{3}} ({{4}}). {{5}}
  ```

  The control plane maps alert type → template + parameters
  (`container_oom` → "ran out of memory", `caddy_down` → "all apps unreachable",
  resolved → "is back to normal"). Free-form text is only allowed in the
  24-hour customer-service window.

### Delivery rules reuse

Same table, one added column: `alert_whatsapp` queue mirroring `alert_emails`,
with the identical severity rules — critical immediate, warning suppressed,
resolved only for critical alerts.

### MVP exclusion rationale

WhatsApp requires (1) a Meta Business account + approved templates, (2) a
phone number, and (3) QR linking UX — significant setup with real-world
approval latency. Email via SMTP covers the "wake someone up at 3am" critical
use case for MVP.

## Done conditions traceability

| Done condition                                   | Where it is satisfied                                 |
| ------------------------------------------------ | ----------------------------------------------------- |
| Alerts in dashboard within 2s                    | WS push (`hub.ForwardToBrowsers`) — existing wiring   |
| Critical → immediate email                       | `Delivery.processImmediate` worker (20s)              |
| Warning → hourly batch email                     | `Delivery.flushDigests` (1h, digest per recipient)    |
| Alert history in CP database                     | `alerts` table + `016_alert_delivery.sql`             |
| Alerts acknowledged from dashboard               | `POST /servers/{id}/alerts/{alertID}/ack` + UI        |
| Resolved alerts update dashboard                 | WS push + `ListAlertsByServer` active-first ordering  |
| WhatsApp architecture designed                   | this document + `Notifier` seam                       |
| Delivery does not block metrics loop             | agent `ws.Client.Send` is buffered/non-blocking; CP delivery runs in background workers |
