# campaign-events

A Go service that receives marketing delivery events from providers and serves per-campaign statistics.

## Requirements

- Go 1.22 or later (`go version` to check)
- No external dependencies — standard library only

## Run

```bash
cd starter
go run .
```

The server starts on port 8080.

## Test

```bash
cd starter
go test ./...
```

With race detector:

```bash
cd starter
go test -race ./...
```

## API

### POST /events

Accepts a JSON array of events. Each event is validated and deduplicated individually — one bad event does not affect the others.

```bash
curl -s -X POST localhost:8080/events \
  -H 'Content-Type: application/json' \
  -d '[
    {"event_id":"evt_1","campaign_id":"cmp_summer_sale","contact_id":"ct_001","type":"delivered","timestamp":"2026-08-10T06:15:00Z"},
    {"event_id":"evt_2","campaign_id":"cmp_summer_sale","contact_id":"ct_002","type":"opened","timestamp":"2026-08-10T09:30:00Z"}
  ]'
```

Response:

```json
{
  "accepted": 2,
  "duplicates": 0,
  "rejected": 0
}
```

If events are rejected, the response includes error details:

```json
{
  "accepted": 1,
  "duplicates": 0,
  "rejected": 1,
  "errors": [
    {"index": 1, "event_id": "evt_bad", "reason": "invalid event type: \"spam_report\""}
  ]
}
```

### GET /campaigns/{campaign_id}/stats

Returns statistics for a single campaign.

```bash
curl -s localhost:8080/campaigns/cmp_summer_sale/stats
```

Response:

```json
{
  "campaign_id": "cmp_summer_sale",
  "sent": 40,
  "delivered": 38,
  "opened": 25,
  "clicked": 10,
  "unique_opens": 20
}
```

Returns 404 if the campaign has no events.

### Load seed data

```bash
curl -s -X POST localhost:8080/events \
  -H 'Content-Type: application/json' \
  --data-binary @seed/events.json
```

## Debugging exercise

The `debugging/` directory (at the repo root) is a separate program. See the root-level `BUGS.md` for findings.

```bash
cd debugging
go run . events.jsonl
go run -race . events.jsonl
```
