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

- **Metadata is optional and stored but not validated.** The assignment says metadata is optional. I accept whatever the provider sends in that field.

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

The in-memory store. At 100 million events/day (~1,150 events/second sustained, but probably bursty), the map holding every event by `event_id` grows without bound. At maybe 200 bytes per event, you're looking at 20 GB/day of data just sitting in memory. You'd run out of RAM in a day or two.

Also, the single `sync.RWMutex` becomes the bottleneck. Every write takes an exclusive lock on the entire store. At 1,000+ writes/second, goroutines start piling up waiting for the lock.

**What I'd change first:**

1. **Move to a real database.** PostgreSQL or a key-value store like DynamoDB. The event store needs to be durable and not limited by RAM. I'd keep the dedup check in the database (unique constraint on `event_id`), which is one less thing to get wrong in application code.

2. **Pre-aggregate counters.** Instead of counting events every time someone asks for stats, maintain running counters that get updated on insert. This is what the current code already does, but it'd need to be done atomically in the database (something like `INSERT ... ON CONFLICT DO UPDATE SET sent = sent + 1`).

3. **Shard or partition the lock.** If I kept the in-memory approach temporarily, I'd shard by campaign_id so different campaigns don't block each other. But honestly, moving to a database is the right call at this scale.

**Would the API contract change?**

Probably not for the read side — `GET /campaigns/{id}/stats` stays the same. For the write side, I might add rate limiting headers or enforce a max batch size. The response shape stays the same.

**Would I introduce a queue?**

Yes. At 100M events/day, I'd put a message queue (SQS, or Kafka if we need ordering guarantees) between the HTTP endpoint and the processing logic. The API handler just validates the event format and drops it onto the queue, then returns immediately. Workers consume from the queue and write to the database.

**What new problems does that create?**

- **At-least-once delivery.** The queue might redeliver a message, so the workers still need dedup. The database unique constraint handles this.
- **Eventual consistency.** There's a lag between receiving an event and it showing up in stats. The dashboard might be a few seconds behind. This is probably fine for marketing stats but you'd want to tell the PM about it.
- **Ordering.** Events can get processed out of order even more than before. But since we already don't depend on ordering, this isn't a new problem — it's just more pronounced.
- **Dead letter handling.** What do you do with events that fail processing repeatedly? You need a dead letter queue and alerting.

**Where can duplicates sneak in at scale?**

- A provider sends an event, the queue accepts it, but the HTTP response times out. The provider retries. Now the event is on the queue twice. Dedup in the database catches this.
- If we have multiple API servers behind a load balancer, two copies of the same event could land on different servers and both get queued. Again, database-level dedup handles it.
- If the database insert succeeds but the queue acknowledgment fails, the worker retries. The database unique constraint saves you.

**How to keep counters trustworthy?**

- Use database transactions for insert + counter update.
- Periodically run a reconciliation job that counts the actual events and compares against the aggregated counters. If they drift, you know something's wrong.
- In the short term, rely on the database's consistency. In the longer term, consider event sourcing where the events are the source of truth and the counters are derived.

**What would I monitor?**

- Event ingestion rate (events/second) — to know when capacity is getting tight.
- Queue depth — if it's growing faster than workers drain it, you need more workers.
- Dedup hit rate — if suddenly 50% of events are duplicates, the provider might have a bug.
- API latency (p50, p95, p99) — to catch lock contention or database slowness.
- Error rates on the processing side — to catch schema changes or bad data.
- Memory and disk usage on the database.

**What would I deliberately NOT solve yet?**

- Multi-region replication. Not until the business actually needs it.
- Real-time streaming stats (WebSockets). Polling every 5-10 seconds is fine for a marketing dashboard.
- Historical event queries with complex filters. The `GET /campaigns/{id}/events` endpoint can wait until someone actually needs it.
- Auto-scaling. Get the basics working on a reasonably sized instance first.

---

## Part 5 — The Angry Marketer

At 10:00: Sent=1,000,000 Delivered=970,000 Opened=250,000 Clicked=20,000
At 10:30: Sent=1,000,000 Delivered=975,000 Opened=248,000 Clicked=20,000

Delivered went up. Opened went down. The marketer says "your dashboard is broken."

### Plausible explanations (before assuming a bug):

1. **Late-arriving delivered events.** Some providers report deliveries with a delay. Between 10:00 and 10:30, 5,000 more delivery confirmations trickled in. Delivered going *up* is completely normal and expected — providers are slow.

2. **Dedup catch-up on opens.** If a batch of duplicate `opened` events was ingested before 10:00 (maybe our dedup had a bug that was then fixed, or a reprocessing job cleaned up duplicates), the count could drop. For example, if we discovered and removed 2,000 duplicate opens from the store.

3. **A data correction or backfill ran.** If someone ran a script to fix inflated open counts (removing duplicates that were incorrectly counted), opens would drop while other numbers stayed the same or grew.

4. **Two different sources of truth.** If the dashboard at 10:00 was reading from a cache and at 10:30 from the live database (or vice versa), and they were slightly out of sync, you'd see inconsistencies. Cache invalidation is a classic culprit.

5. **Provider sent corrections.** Some providers send "un-open" or correction events. If the system processes those as negating previous opens, the count goes down. (This is unusual but not impossible.)

6. **A recount or recomputation happened.** If the system switched from pre-aggregated counters to computing stats from raw events between 10:00 and 10:30, and the pre-aggregated counters had been wrong, you'd see a jump or drop.

7. **Bot/spam filter reclassification.** Some systems reclassify opens detected as bot activity. If 2,000 opens were reclassified as bot opens and excluded from the count, the number drops.

### What I'd check first:

1. **Look at the raw event count in the database.** Has the number of `opened` events actually decreased, or is it a display/aggregation issue? If the raw count is still 250,000 events, the bug is in the aggregation or display layer, not the data.

2. **Check for recent deploys or migrations.** Did anything change between 10:00 and 10:30? A code deploy, a database migration, a backfill job?

3. **Check the dedup logic.** Were duplicate events previously being double-counted and now they're not? That would explain the drop.

4. **Check timestamps on the events that make up the difference.** Can I find the ~2,000 opens that "disappeared"?

### How I'd distinguish a bug from expected behavior:

- If the raw event count didn't change but the stat did → aggregation bug.
- If the raw event count dropped → something deleted events, which is either a cleanup job or a bug.
- If the opened count *never* decreases in the code path → it's not a code bug, it's a data issue.
- If I can point to specific events that were correctly deduplicated → it's expected.

### Is the delivered number rising actually suspicious?

No. Delivered going up is the *most normal thing here*. Providers report deliveries asynchronously. It's completely expected for delivery confirmations to keep trickling in after the send is done. The send is done at 1,000,000 and hasn't changed — that makes sense too, the campaign finished sending. Deliveries catching up is just how the real world works.

The suspicious part is opens going *down*. That's the one that needs investigation.

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
