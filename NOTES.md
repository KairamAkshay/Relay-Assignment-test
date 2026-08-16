# NOTES.md

## Part 1 — Interpretation

The problem is building a small webhook receiver for a fictional marketing company. Delivery providers (who actually send the emails/SMS/push) call our API when something happens to a message — it was sent, delivered, opened, or clicked. We store those events and serve per-campaign statistics so a marketer dashboard can show live numbers.

The tricky part isn't the happy path — it's handling all the messy real-world stuff: providers retrying and sending the same event twice, events arriving out of chronological order, batches that contain a mix of valid and garbage data, and concurrent requests hitting the service at the same time.

## Assumptions

- **`event_id` is the idempotency key.** The assignment says "event_id is assigned by the provider and identifies one event." I treat it as globally unique — if I've seen `evt_123` before, I don't count it again.

- **Conflicting duplicates are rejected.** If `evt_029` arrives first as `type: "sent"` and later as `type: "clicked"` (same event_id, different data), something is wrong. Either the provider has a bug or there's data corruption. I reject the second one and include an error in the response. I could also accept-first-wins silently, but I think surfacing the conflict is more useful for debugging provider issues.

- **Event type is case-sensitive.** The contract says the four types are `sent`, `delivered`, `opened`, `clicked`. If a provider sends `OPENED`, that's not one of the four — I reject it. I considered normalizing to lowercase, but the assignment says the contract is fixed, and being strict about it makes bugs in provider integrations visible faster.

- **Timestamp must be RFC3339.** The example payload uses `2026-08-10T09:15:04Z`, which is RFC3339. If a provider sends `10-08-2026 09:00` or `not-a-time`, I reject it. The timestamp describes when the event happened, so it matters that we can actually parse it.

- **All required fields must be present.** An event without a `contact_id` or `campaign_id` can't be meaningfully tracked. Empty string = missing.

- **In-memory storage is fine.** The assignment says "a few thousand events a day across ~50 campaigns." That's a tiny amount of data. An in-memory map with a mutex is simpler, faster, and easier to test than setting up SQLite. The tradeoff is obvious — you lose everything on restart — but for this assignment scope, that's acceptable.

- **Events can arrive in any order.** I don't enforce sent→delivered→opened→clicked ordering. If `opened` arrives before `sent`, both are counted. The timestamp says when it happened at the provider, not when it reached us.

- **`unique_opens` is scoped per campaign.** If contact ct_001 opens campaign A and campaign B, that's one unique opener per campaign. The same contact opening the same campaign 5 times is 5 open events but 1 unique opener.

- **Metadata is optional and stored but not validated.** I treated metadata as an optional object with string values (`map[string]string`) because the assignment doesn't define a more detailed metadata schema. I accept and store whatever the provider sends in that field.

## Ambiguities

- **What should the batch response look like?** The brief doesn't specify. I return `{accepted, duplicates, rejected, errors[]}`. The `errors` array includes the array index and reason for each rejection, which makes it easier for a provider integration engineer to figure out what went wrong.

- **Should we return 200 or 207 for mixed batches?** I return 200 always when the JSON array itself is valid. The body tells you what happened to each event. A 400 only happens if the outer structure isn't a JSON array at all. This is simpler and avoids the question of what HTTP status means "some worked, some didn't."

- **What stats should the dashboard get?** The brief says "how many were sent, delivered, opened, clicked." I also include `unique_opens` because the assignment mentions it and it's a number marketers definitely care about (it tells you how many *people* opened vs how many *times* people opened).

- **Should we accept events for unknown campaigns?** I didn't add a separate concept of "known campaigns." If an event arrives for `cmp_new_campaign`, we create stats for it. In a real system you'd probably want campaign registration, but the assignment doesn't mention it.

## Questions for the PM

1. When the same `event_id` arrives with different data (e.g., first it's `sent`, then it's `clicked`), is that a provider bug we should flag, or can it actually happen legitimately?

2. Should we enforce any ordering validation? Like, should we warn if we get a `clicked` event for a contact that never got a `sent`? Or is that out of scope?

3. Is there a maximum batch size we should enforce? Right now I accept any size array. At scale, a provider sending 100,000 events in one request could be a problem.

4. What's the retention policy? Should old events be cleaned up, or do we just grow forever?

5. For `unique_opens` — does the PM want unique *per campaign* or unique *per campaign per time window* (like daily unique opens)? I went with per-campaign totals.

6. Do we need to handle `metadata` for anything specific, or is it just passed through for potential future use?

## Priorities

Given the 3.5-4 hour box, I prioritized in this order:

1. **Correctness first** — deduplication, validation, accurate statistics. If the numbers are wrong, nothing else matters.
2. **Concurrency safety** — the service handles HTTP requests concurrently by default. If the store isn't thread-safe, you get subtle bugs that only show up under load.
3. **Batch resilience** — one bad event shouldn't torpedo 199 good ones. I used `json.RawMessage` to decode elements individually.
4. **Tests** — focused on the behaviors that would be most embarrassing to break: dedup, concurrent access, validation, unique opens.
5. **Debugging exercise** — four bugs, minimal fixes, verified against expected output.
6. **Documentation** — this file, BUGS.md, README, AI_USAGE.md.

I intentionally skipped the optional `GET /campaigns/{id}/events` endpoint. It wasn't needed for the core requirement and I wanted to spend the time on getting the fundamentals right.

---

## Part 4 — Scale Memo

**What breaks first?**

The current in-memory store. At 100 million events/day (~1,150 events/second sustained, but highly bursty), keeping the complete event set in one process clearly doesn't scale. Even a rough 200 bytes per event would be about 20 GB/day before accounting for Go map, string, and runtime overhead. 

Also, memory is volatile. On restart, all historical campaign states are lost. The major immediate problems at this scale are unbounded memory growth, single-process limits, lack of durability, and the potential for expensive statistics scans.

The single "sync.RWMutex" is another thing I'd measure for contention as concurrency grows. If lock contention becomes significant, I could shard the in-memory state by campaign, although at this scale the bigger problems are durability and unbounded memory growth.

**What I'd change first:**

1. **Move to a durable database.** A relational store like PostgreSQL or a key-value store like DynamoDB/Cassandra. This provides durability and eliminates the RAM bottleneck. I'd delegate the deduplication check to the database using a unique constraint on `event_id`.

2. **Pre-aggregate counters as derived projections.** Instead of scanning raw events to count stats on demand, we update pre-aggregated tables or counters atomically upon event insertion (e.g., using `INSERT ... ON CONFLICT DO UPDATE SET sent = sent + 1`). This decouples query speed from event volume.

**Would the API contract change?**

The read side (`GET /campaigns/{campaign_id}/stats`) would remain identical. For the write side, we would enforce a maximum batch size per request and add standard rate-limiting headers to protect the service from overloading.

**Would I introduce a queue?**

Yes. I'd probably start with a durable queue such as SQS to absorb traffic spikes and separate ingestion from database writes. Kafka becomes more attractive if throughput, replay, partitioning, or stream-processing requirements justify the added complexity. Note that the current system does not require events to arrive in order to calculate campaign statistics, and Kafka ordering depends on how partitions and consumers are configured.

**What new problems does that create?**

- **At-least-once delivery**: Message queues can redeliver messages. Workers must be idempotent, which we handle via the database unique constraint.
- **Eventual consistency**: Ingestion is decoupled from query logic, meaning stats on the dashboard might lag by a few seconds.
- **Dead letter handling**: Malformed or unprocessable events must be directed to a dead-letter queue (DLQ) for alerting and manual intervention.

**Where can duplicates sneak in at scale?**

- **Provider retries**: If a provider times out waiting for our HTTP response, it will retry, queuing the duplicate event.
- **Load balancer distribution**: Multiple API instances could receive the same retry requests and queue them.
- **Worker retries**: If a worker writes to the database successfully but fails to acknowledge the queue message, the queue will redeliver it.

**How to keep counters trustworthy?**

- Use database transactions or upsert logic to pair event insertion with counter increments.
- Periodically run a background reconciliation job that reconstructs counters directly from the raw event log and patches any drifted counters.

**What would I monitor?**

- Ingestion API request rate and latency (p50, p99).
- Queue depth and processing lag.
- Database write latency and CPU utilization.
- Deduplication match rates (alerting if retry volume spikes).
- Dead-letter queue count.

**What would I deliberately NOT solve yet?**

- Multi-region active-active database replication.
- Real-time event streaming pipelines (e.g., WebSockets for real-time dashboard updates).
- Complex custom query filtering on historical events.


---

## Part 5 — The Angry Marketer

At 10:00: Sent=1,000,000 Delivered=970,000 Opened=250,000 Clicked=20,000
At 10:30: Sent=1,000,000 Delivered=975,000 Opened=248,000 Clicked=20,000

Delivered went up. Opened went down. The marketer says "your dashboard is broken."

### Plausible explanations (before assuming a bug):

First, we must make a key distinction: **in the current implementation, accepted "opened" events increase the opened count, and there is no code path that decrements the opened counter**. Therefore, the drop from 250,000 to 248,000 opens cannot simply be explained by late-arriving events. Late-arriving events explain a number increasing over time (e.g. 250,000 → 252,000), not decreasing.

Instead, a decrease should make us investigate outside the simple counter-increment path. Plausible explanations include:
1. **Another aggregation source / different query**: The dashboard might have switched its query logic or query filters between 10:00 and 10:30, or pulled from a different metric definition source.
2. **Dashboard/cache inconsistency**: A caching layer or CDN might have experienced cache invalidation issues, causing the dashboard to read stale data at 10:30 or read corrupted cache entries.
3. **Querying different replica nodes**: The query at 10:30 might have been routed to a read-replica node suffering from replication lag or data partition issues, compared to the 10:00 query which hit a synchronized node.
4. **Data pipeline/configuration changes**: A deployment or configuration update might have altered the data pipeline routing or aggregation logic.
5. **Separate data-correction/reconciliation process**: An independent backend reconciliation job or data-cleanup script (such as a spam/bot-filtering process) might have run to remove duplicate or invalid open events, reducing the count.

Additionally, the delivered count rising from 970,000 to 975,000 is **NOT inherently suspicious**. Delivery confirmations naturally arrive late and out of order from providers, so deliveries continuing to trickle in while Sent remains constant at 1,000,000 is expected production behavior.

### What I'd check first:

1. **Verify raw database counts directly**: Run a direct SQL/key count query on the primary event store to see if the actual open event count is 250,000 or 248,000. If the primary database contains 250,000, the data is intact and the drop is caused by the caching, routing, or dashboard visualization layer.
2. **Review deployment and pipeline logs**: Check for any system deployments, database migrations, configuration rollouts, or background reconciliation jobs that executed between 10:00 and 10:30.
3. **Check replica lag metrics**: Check if any read-replicas fell behind the primary database node during that period.



---

## What I completed / what I intentionally skipped

**Completed:**
- Part 1: Problem analysis, assumptions, ambiguities (this document)
- Part 2: Working service with POST /events and GET /campaigns/{id}/stats
  - Deduplication by event_id
  - Conflicting duplicate detection
  - Per-element batch processing (one bad event doesn't kill the batch)
  - Validation of all required fields, event type, timestamp format
  - Unique opens tracked per campaign
  - Concurrent safety with sync.RWMutex
  - Tests covering all key behaviors (dedup, concurrency, validation, unique opens, out-of-order)
- Part 3: All four debugging bugs found and fixed
- Part 4: Scale memo (above)
- Part 5: Angry marketer analysis (above)
- All required documentation files

**Intentionally skipped:**
- `GET /campaigns/{id}/events` (optional paginated endpoint). The assignment says it's optional and only if I have time. I chose to spend that time on test coverage and documentation instead.
- Frontend. The assignment explicitly says UI earns no extra credit.
- SQLite / persistent storage. Not needed at this scale, and in-memory is simpler.

## If I had another day

- **Add the optional events endpoint.** Paginated list of events for a campaign, sorted by timestamp. Not hard, but I didn't want to rush it.
- **Add request logging middleware.** Log every request with method, path, status code, and duration. Useful for debugging in production.
- **Add a `/health` endpoint.** Simple liveness check that integration tests and load balancers can use.
- **Write a small load test script.** Send a few thousand concurrent requests and verify the numbers are still correct. The race detector tests catch most issues, but a load test would give more confidence.
- **Think harder about the conflicting duplicate policy.** Right now I reject the second event. But maybe the right thing is to accept-first-wins and log a warning? I'd want to discuss this with the PM.
- **Add graceful shutdown.** Handle SIGTERM, drain in-flight requests, etc. Standard production hygiene that I skipped for time.
