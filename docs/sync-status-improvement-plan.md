# Sync status improvement plan

Status: planning only. This document does not change sync or UI behavior.

## Desired behavior

The status card shows progress through the current run and one concise set of
outcome counts. View Details explains every processed book through mutually
exclusive categories, with live lists for items needing attention. At every
snapshot, the category counts add up to the number of books processed so far.
The pre-counted library size remains a separate progress denominator.

## Current behavior and causes

- `web/static/app.js` renders `Books Synced` in both the progress text and the
  status badges. View Details repeats the same synced/processed counts without
  accounting for skips and failures.
- The details message `All books were found in Hardcover` depends only on an
  empty `books_not_found` array. Title/author-only matches are placed in the
  separate potential-mismatches list, so the message overstates certainty.
- `internal/sync/service.go` records `BooksNotFound` during processing, but
  copies the global mismatch collector into the sync summary only after all
  libraries finish. `internal/multiuser/service.go` exposes that summary during
  a run, so live mismatch counts remain zero until the end.
- Other no-match paths add a mismatch without adding a `BooksNotFound` entry.
  Lookup timeouts and canceled requests can also be labeled `not found`. The
  current lists are therefore not an exhaustive or reliable partition.
- `TotalBooksProcessed` increments for every book entering `processBook`,
  including skipped and failed books. `BooksSynced` follows a `bookProcessed`
  boolean that is also set for some missing books, mismatches, and intentional
  no-ops. It cannot be used as an exclusive success category or added to the
  other current counts.
- The status card polls every five seconds, but an open View Details panel is
  built only when opened. Its contents do not update with status polling.
- A new run replaces the previous profile status. The new run's counters and
  lists must be labeled as current-run data; last-run results need a distinct
  view or explicit timestamp if retained.

## Proposed outcome contract

Assign each book exactly one primary outcome when its processing attempt ends.
Use the Audiobookshelf item ID as the key so later enrichment updates details
without adding another outcome. Keep secondary attributes such as match method,
target reading status, and whether a mutation occurred outside this partition.

| Primary outcome | Meaning | Useful details |
| --- | --- | --- |
| Synced | Required Hardcover changes completed successfully | Match method, edition, action |
| Already current | An eligible, confirmed match needed no change | Incremental/no-change or equivalent-state reason |
| Skipped | Intentionally excluded by configuration or threshold | Ebook, unread, filter, disabled action, threshold |
| Needs review | A possible book match exists but the intended edition is unverified | Title/author candidate, ASIN/ISBN comparison, reason |
| Not found | A completed lookup found no suitable Hardcover book | Attempted identifiers and search methods |
| Failed | An API, timeout, cancellation, or processing error prevented a reliable result | Stage, error, retryability |
| Would sync (dry run) | A dry run reached an action that would mutate Hardcover | Planned action; no mutation claimed |

`processed_so_far = synced + already_current + skipped + needs_review +
not_found + failed + would_sync`. Decide whether a dry-run match that needs no
change belongs in `already_current` (recommended) before implementation. A
book cannot be both `not_found` and `needs_review`. An inconclusive lookup must
be `failed`, even if the current code would call it missing. `books_total`
denotes pre-counted candidates, not the sum of processed outcomes. If a library
cannot be fetched at all, report that separately as a run-level error rather
than silently treating unseen books as processed.

## Implementation sequence

1. Add a per-run, per-profile outcome store to the sync service. Replace the
   `bookProcessed` counting decision with one final outcome per `processBook`
   exit path. Update the processed count, one category count, and any associated
   book record together under the summary lock. Keep a clear mapping for early
   filters, incremental skips, confirmed matches, title-only matches, true
   no-results, API failures, and Hardcover write failures. Preserve existing
   sync-state and dry-run safety rules.
2. Record `needs review` and `not found` as soon as each attempt resolves.
   Enrichment may later replace details for the same book ID; it must not delay
   the live count or duplicate the book. Keep mismatch-file persistence as a
   separate operation. Remove reliance on the package-global mismatch list for
   per-profile status, since concurrent profiles must remain isolated.
3. Return one consistent, race-safe current-run snapshot through both status
   and summary API paths. Include run ID, start time, state, processed count,
   candidate total, category counts, and book lists for attention categories.
   Preserve or version existing API fields deliberately and document their
   semantics. The UI should not combine counts from one response with lists
   from another run.
4. Show `processed_so_far / books_total` and the primary categories once on
   the status card. Use View Details for the full breakdown, skip reasons,
   action/match-method details, and live attention lists. Refresh an open
   details panel from the same polled snapshot without resetting its scroll,
   selection, or expanded items. Show current-run and last-run times distinctly.
5. Remove `All books were found in Hardcover`. Show a neutral empty state such
   as `No missing books reported in this run so far` while running, and `No
   missing books reported in this run` when complete. Keep potential matches
   visibly separate and say that they still need verification. Only show a
   definitive clean state when the run has completed without unresolved or
   failed outcomes.
6. Validate through the real sync-service to status/summary API path and UI
   rendering: a title-only candidate appears before the next book completes;
   a conclusive no-result appears immediately; an API timeout appears under
   Failed; skips and dry-run actions reconcile to processed; simultaneous
   profiles do not share results; completion preserves the live counts; and
   every observed snapshot satisfies the category-sum invariant. Verify the
   existing dry-run no-mutation contract remains intact.

## Acceptance example

If 12 books have been attempted out of 20 candidates, the UI might show:
`3 synced + 2 already current + 2 skipped + 3 needs review + 1 not found +
1 failed = 12 processed`. The `needs review` and `not found` lists are already
visible while the run continues. A dry run would use `would sync` in place of
any mutation claim.
