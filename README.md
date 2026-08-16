# Relay Engineering Assignment

This repository contains the complete solution for the Relay Engineering Assignment. It consists of a Go backend service that ingests marketing events from third-party message providers and exposes campaign statistics, alongside a fixed debugging command-line utility. The implementation prioritizes correctness under concurrent load, robust validation, idempotent event ingestion, and predictable aggregations.

## Project Structure

* **`starter/`**: The main Go backend application including handlers, store, types, and automated tests.
* **`debugging/`**: A command-line Go utility that aggregates campaign events from a JSONL file, now fixed to resolve all concurrency and logical bugs.
* **`assignment/`**: The original assignment description and PDF specification files.
* **`NOTES.md`**: Explains the technical interpretation, assumptions, scaling roadmap, and an analysis of specific production anomalies.
* **`BUGS.md`**: Detailed documentation of the four bugs found and fixed in the `debugging/` program.
* **`AI_USAGE.md`**: Factual disclosure of AI assistant usage during this assignment.

## Running the Service

The main service requires **Go 1.22** or later and uses only the standard library.

To run the HTTP server:

```bash
cd starter
go run .
```

The server listens on `http://localhost:8080`.

To load the seed data:

```bash
curl -s -X POST http://localhost:8080/events \
  -H 'Content-Type: application/json' \
  --data-binary @seed/events.json
```

## Running Tests

To run the automated tests for the main service:

```bash
cd starter
go test -v ./...
```

If your local environment supports CGO, you can also run with the race detector:

```bash
go test -race ./...
```

## API Specification

### 1. Ingest Events
* **Endpoint**: `POST /events`
* **Body**: JSON array of event objects.
* **Behavior**: Elements are validated and stored independently. If any event contains invalid formatting or conflicts with existing data, it is rejected without affecting valid events in the same batch.

**Example Request:**
```bash
curl -s -X POST http://localhost:8080/events \
  -H 'Content-Type: application/json' \
  -d '[
    {"event_id":"evt_001","campaign_id":"cmp_summer","contact_id":"ct_01","type":"sent","timestamp":"2026-08-10T09:00:00Z"},
    {"event_id":"evt_002","campaign_id":"cmp_summer","contact_id":"ct_01","type":"delivered","timestamp":"2026-08-10T09:05:00Z"}
  ]'
```

**Example Response:**
```json
{
  "accepted": 2,
  "duplicates": 0,
  "rejected": 0
}
```

If some events in the array fail validation or conflict with stored records, the response reflects the breakdown:
```json
{
  "accepted": 1,
  "duplicates": 0,
  "rejected": 1,
  "errors": [
    {
      "index": 1,
      "event_id": "evt_bad",
      "reason": "invalid event type: \"spam_report\""
    }
  ]
}
```

### 2. Campaign Statistics
* **Endpoint**: `GET /campaigns/{campaign_id}/stats`
* **Behavior**: Returns current counters and unique open stats for the specified campaign. If the campaign has no ingested events, returns `404 Not Found`.

**Example Request:**
```bash
curl -s http://localhost:8080/campaigns/cmp_summer/stats
```

**Example Response:**
```json
{
  "campaign_id": "cmp_summer",
  "sent": 1,
  "delivered": 1,
  "opened": 0,
  "clicked": 0,
  "unique_opens": 0
}
```

---

## Design Decisions

* **Idempotency**: We treat `event_id` as the unique provider identifier. Exact duplicate event payloads are tracked and counted only once. If a duplicate `event_id` arrives with conflicting details (e.g. a different event type or campaign), it is rejected as a data integrity violation.
* **Concurrency Safety**: Shared state in the store is protected via a `sync.RWMutex`. Check-and-store operations are fully synchronized to prevent concurrent ingestion from double-counting events.
* **Partial Batch Processing**: We decode the request body into a slice of raw JSON messages (`json.RawMessage`), validating and inserting each one individually so that one malformed element doesn't fail the entire request.
* **Unique Opens**: Scoped per campaign. A contact opening a single campaign multiple times counts as one unique open. The same contact opening different campaigns is counted once in each.
* **In-Memory Storage**: Given the daily traffic scale of thousands of events across 50 active campaigns, an in-memory map with read-write mutex lock protection is highly performant and eliminates database-setup complexity.

## Scope

The optional paginated event list (`GET /campaigns/{campaign_id}/events`) was intentionally skipped. For this time-boxed task, prioritizing complete test coverage, data validation correctness, concurrency safety, and a thorough debugging analysis was chosen over implementing optional features.
