# BUGS.md

Four bugs found in `debugging/main.go`. Each fix is minimal — just the smallest change needed to correct the behavior.

---

## Bug 1: Deduplication only works within a single batch, not across batches

**What:** The `seen` map in `processBatch()` (line 101 originally) was declared as a local variable. It was recreated empty every time `processBatch` was called. Since the file is read in batches of 200, any duplicate event_id that appeared in a different batch than its original would not be caught.

**Why:** The `seen` map's purpose is to track which event_ids have already been processed so duplicates don't inflate the counters. But because it was local to each `processBatch` call, it only tracked duplicates *within* a 200-event batch. The 20,000-line input file gets split into 100 batches, so any duplicate that spans batch boundaries was counted twice.

**Fix:** Moved the `seen` map to a package-level variable (`globalSeen`) that persists across all `processBatch` calls. Removed the local `seen` declaration.

**Verification:** After the fix, the final counts match `expected_output.txt`. Before the fix, counts were inflated because cross-batch duplicates were being counted multiple times.

---

## Bug 2: Data race on counter increments in apply()

**What:** The `apply()` function increments `cs.Sent`, `cs.Delivered`, `cs.Opened`, and `cs.Clicked` — but it runs inside goroutine workers (the `numWorkers = 8` pool). Multiple goroutines can increment the same campaign's counters simultaneously without synchronization.

**Why:** `processBatch` fans out events to a pool of 8 worker goroutines via a channel. Each worker calls `apply(ev)`, which does `cs.Sent++` etc. The `++` operation on an int is not atomic — it's a read-modify-write. Two goroutines can read the same value, both add 1, and both write the same result, losing an increment. The Go race detector catches this.

**Fix:** Added a `sync.Mutex` field to `CampaignStats` and wrapped the switch block in `apply()` with `cs.mu.Lock()` / `defer cs.mu.Unlock()`.

**Verification:** `go run -race . events.jsonl` no longer reports any data race. Before the fix, the race detector flagged concurrent reads and writes to `cs.Sent`, `cs.Delivered`, etc.

---

## Bug 3: unique_opens key not scoped to campaign

**What:** In `track()`, the `openedBy` map used `ev.ContactID` as the key (line 139 originally). This means a contact who opened campaign A would be considered "already opened" when they opened campaign B — so campaign B's `unique_opens` would be undercounted.

**Why:** The README spec says: "One contact opening five times in a campaign counts once. The same contact opening in two campaigns counts once *in each*." Using just `ev.ContactID` as the key makes uniqueness global across all campaigns, not per-campaign.

**Fix:** Changed the key from `ev.ContactID` to `ev.CampaignID + "|" + ev.ContactID`. This makes the uniqueness check scoped to each campaign.

**Verification:** After the fix, the `unique_opens` values in the output match `expected_output.txt`. Before the fix, they were lower because contacts that appeared in multiple campaigns were only counted as unique in the first campaign they appeared in.

---

## Bug 4: Daily delivered buckets use local time instead of UTC

**What:** In `track()`, the daily delivered bucket date was computed using `ev.Timestamp.Local().Format("2006-01-02")` (line 144 originally). The spec says daily delivered buckets should use the event's UTC date.

**Why:** `.Local()` converts the timestamp to the machine's local timezone before formatting the date. Since the test events span midnight UTC, an event at `2026-08-01T23:00:00Z` would be bucketed as August 2nd in UTC+5:30, for example. This shifts events between days depending on what timezone the machine is set to, making the output non-deterministic across environments.

**Fix:** Changed `ev.Timestamp.Local()` to `ev.Timestamp.UTC()`.

**Verification:** After the fix, the daily delivered buckets match `expected_output.txt` exactly. The output is now identical regardless of the machine's timezone setting.
