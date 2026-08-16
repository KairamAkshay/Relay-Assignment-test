# AI_USAGE.md

## Tools used

- **Antigravity IDE (Claude)** — used throughout the assignment for code generation, debugging analysis, and documentation drafting.

## What I used AI for

- Analyzing the seed data to categorize duplicates, conflicting duplicates, and malformed events (finding patterns in 235 events by hand is tedious and error-prone).
- Generating the initial skeleton for store.go, the test files, and documentation drafts.
- Reasoning through the four debugging bugs — I described the code structure and the expected vs actual output, and used AI to help identify the root causes.
- Drafting the scale memo and angry marketer sections (though I restructured and rewrote parts to make sure the reasoning was mine).

## One suggestion I rejected or changed

AI initially suggested normalizing event types to lowercase before validation (i.e., treating `OPENED` the same as `opened`). I rejected this because the assignment says the event payload contract is fixed — the four types are lowercase. Being strict about case makes it easier to catch provider integration bugs. If a provider sends `OPENED`, that's not conforming to the contract, and we should tell them about it rather than silently fixing it. This is the kind of decision that's easy to change later if the PM disagrees.

## One thing AI helped me understand

The distinction between "dedup only within a batch" vs "global dedup" in the debugging exercise. Looking at the code, `processBatch` processes 200 events at a time, and I initially almost missed that the `seen` map being local to `processBatch` meant duplicates that land in different batches would slip through. AI pointed out that the batch boundary is the key issue, which made the bug immediately obvious once I looked at the code with that framing.

## What AI generated that I had to debug

Nothing major needed fixing in the generated code itself. The main thing I double-checked carefully was the concurrency test — making sure the test actually exercises the race condition (all goroutines submitting the same event_id) rather than just testing that "concurrent requests don't crash." I also verified that the `json.RawMessage` approach for batch processing correctly handles the case where the outer array is valid JSON but individual elements are not.
