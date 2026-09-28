# Plan: Import Audible ASIN Audiobooks Without a Hardcover Book ID

**Status:** Planned. Not started. Delivered as two PRs.

This plan follows the
[needs-review edition creation plan](https://github.com/Snuffy2/audiobookshelf-hardcover-sync/blob/docs/needs-review-edition-plan/docs/implementations/needs-review-edition-creation.md)
(the "edition plan") and starts after its Step 11. It reuses that plan's
local association, exact Audible `book_mappings` lookup, regional
`upsert_book` resolver, Audnexus region discovery, state-file lock,
stale-record checks, capability reporting, and dry-run rules without
restating them. Where this plan is silent, the edition plan applies.

## Outcome and boundaries

This plan applies **only to an ABS audiobook with a usable source ASIN**
(exactly ten ASCII letters or digits, as defined in the edition plan's
Step 3). For such an item, the ASIN identifies the book, not a title/author
guess, so:

- the item does not need a Hardcover `book_id` to be added;
- sync does not run title/author discovery for it;
- the user validates that the **Audnexus** record for the regional ASIN
  matches the **ABS** item; there is no Hardcover-to-ABS candidate review.

Unchanged:

- ebooks keep the anchored `insert_edition` flow;
- audiobooks without a usable ASIN (ISBN-only or malformed ASIN) keep
  title/author discovery and the anchored regional import with the run
  record's book ID;
- an existing local association and an exact regional `book_mappings`
  match still take priority over everything in this plan;
- sync never writes to the Hardcover catalogue; imports happen only on an
  explicit user action (create API, `edition create`, or UI);
- `insert_book_mapping` is never called;
- dry run makes no Hardcover mutation and saves no association.

## Delivery

Two PRs to `develop`, created only when requested. Each must work on its
own when merged.

| Step | Branch | Scope | Depends on |
|---|---|---|---|
| 1 | `audible_asin_import_step_1` | Backend: sync classification, draft comparison, unanchored resolver, create API, CLI. Usable through the create API and `edition create` without the UI. | Edition plan Step 11 |
| 2 | `audible_asin_import_step_2` | Sync Status UI for Audible imports. | Step 1, edition plan Step 10 |

The two PRs could later be combined if review prefers one change.

No live-API gate remains: `upsert_book` without `book_id` has been confirmed
against Hardcover for this flow (see
[Resolved decisions and evidence](#resolved-decisions-and-evidence)).

### Step 1: backend

**Sync classification.** After edition plan Step 11, an audiobook is matched
only by a valid local association or the exact regional `book_mappings`
lookup. For an audiobook with a usable ASIN that neither resolves, skip
title/author search and record `needs_review` with a new reason,
`audible_import_available`, and no Hardcover book or edition ID. This
replaces both the title/author `needs_review` and the `not_found` outcomes
for these items and removes their title/author Hardcover reads. Record the
same source snapshot (normalized ASIN, ISBN-10, ISBN-13, reading format) the
create path already uses to detect stale records. Keep the reason visible in
the run record and mismatch export so the UI and CLI can tell an Audible
import item from a title/author candidate. The edition plan's Step 11 migration
already routes affected checkpoints into this path; no extra migration is
needed.

**Draft: Audnexus-to-ABS comparison.** For these items the edition draft adds
an `audnexus_record` next to the existing ABS preview, taken from the Audnexus
response that established the region: title, subtitle, authors, narrators,
series and position, publisher, release date, runtime, language, and cover
URL. Add a per-field comparison (`match`, `differs`, `missing`) using the
existing name and title normalization. Differences are informational, never
an automatic rejection. An unknown or temporarily unavailable region yields
no record and cannot be confirmed. The draft accepts an optional corrected
ASIN and region so the user can preview a corrected regional identifier
before submitting it. The draft still makes no Hardcover request.

**Unanchored resolver mode.** Add an explicit unanchored mode to the regional
`upsert_book` resolver (for example an `Anchor` field on
`RegionalAudiobookInput`) instead of treating a zero `BookID` as "no book",
so an unset ID in the anchored path still fails validation. The unanchored
mode:

- sends `upsert_book` without `book_id`;
- keeps the existing polling bounds, rate limiting, retries, dry-run
  refusal, and `failed`/`loaded`/`created` handling;
- verifies with a fresh read that the returned edition exists, belongs to
  the returned book, and has audiobook reading format; there is no expected
  book to compare;
- treats a non-audiobook edition or inconsistent IDs as an identity
  conflict: report it, save nothing, and state that Hardcover may already
  have changed.

**Create API.** For an audiobook create with a usable ASIN, the create POST:

- requires `audnexus_confirmed: true` with the regional identifier (ASIN and
  region) the user confirmed; missing or mismatched confirmation is 400
  before any mutation;
- refetches the ABS item and applies the existing stale-record check against
  the run snapshot (409 on change);
- re-reads Audnexus for exactly the confirmed region and requires a response
  for that ASIN: a miss is 409 (the confirmed record no longer resolves); a
  rate limit or transient error is retryable; no region sweep is repeated;
- imports through the unanchored resolver. Never send the run record's book
  ID, a title/author candidate, or a client-supplied book ID for these
  imports;
- returns the resolved book and edition IDs and the book title for display.
  This is informational, not a review gate.

Save the association under the state-file lock as the existing create API
does, with provenance `audible_import_unanchored`, the regional external ID,
the source snapshot, and the Audnexus confirmation (region and time). The next
sync uses it like any other association. Capability reporting, the profile
run guard, 409 during sync, the distinct "remote success, local save failed"
outcome, and dry run are unchanged.

**CLI.** `edition create` accepts audiobook input with a usable ASIN and no
`book_id`. It prints the Audnexus record for the regional ASIN and, when an ABS
item ID is given, the ABS metadata and per-field comparison, then requires
interactive confirmation or `--confirm-audnexus` before importing without
`book_id`. Input that includes a `book_id` keeps the existing anchored
import, so existing export files behave as before. The mismatch export omits
`book_id` for `audible_import_available` items and includes the reason.
Association saving, the lock, and dry run follow the existing CLI behavior.

### Step 2: UI

Extend the Sync Status add-edition flow for `audible_import_available`
items:

- use the same "Add Edition" button as other add-edition items, so the row
  looks the same;
- the modal may differ from the standard add-edition modal only as needed to
  show the data to validate: ABS and Audnexus values side by side with
  differences highlighted, and no Hardcover candidate;
- no separate confirmation checkbox: submitting the modal is the user's
  confirmation, and the UI sends `audnexus_confirmed` with the regional
  identifier shown;
- offer the regional identifier correction with a re-preview;
- disable the action while the region is unknown or temporarily
  unavailable, honoring `Retry-After`;
- after success, show the returned Hardcover book and edition.

Capability, dry-run, sync-in-progress, and optional resync behavior follow
the existing UI. Escape all ABS- and Audnexus-provided strings.

## Acceptance

- An unmatched audiobook with a usable ASIN is
  `needs_review`/`audible_import_available` with no title/author request.
- Creating it sends `upsert_book` without `book_id` only after an explicit
  Audnexus confirmation, saves a verified association for `loaded` and
  `created`, and the next sync matches it without another import.
- Audiobooks without a usable ASIN and all ebooks behave as before.
- Dry run makes no mutation and saves nothing.

## Step checklists

### Step 1 — backend

- [ ] Classify unmatched usable-ASIN audiobooks as `needs_review` with reason
  `audible_import_available`, no Hardcover IDs, the source snapshot, and no
  title/author search. Leave other audiobooks and ebooks unchanged.
- [ ] Add the draft's `audnexus_record` and per-field ABS comparison; allow
  previewing a corrected regional identifier; make no Hardcover request.
- [ ] Add the resolver's explicit unanchored mode with the same bounds and
  fresh-read verification; keep the anchored mode's zero-ID validation.
- [ ] In the create API, require `audnexus_confirmed` with the matching
  regional identifier, apply the stale-record check, re-read Audnexus for the
  confirmed region, import without `book_id`, and save the association with
  `audible_import_unanchored` provenance under the state-file lock.
- [ ] In `edition create`, accept ASIN-only audiobook input, show the Audnexus
  record (and ABS comparison with an item ID), require confirmation or
  `--confirm-audnexus`, and keep the anchored path when `book_id` is given.
  Export `audible_import_available` items without `book_id`.
- [ ] Test classification (no title/author request), draft comparison
  (match, differs, missing, unknown and unavailable region), missing or
  mismatched confirmation, stale record, Audnexus miss or 429 at create,
  `loaded` and `created` without `book_id`, non-audiobook result, failed
  import, timeout, local-save failure, next-sync reuse, CLI with and without
  `book_id`, and dry run at the HTTP, client, and command boundaries. Assert
  no `book_id` is sent in unanchored imports.
- [ ] Update README, OpenAPI, `cmd/edition/README.md`, and the field
  crosswalk; add one CHANGELOG bullet; run the PR gates below.

### Step 2 — UI

- [ ] Use the existing "Add Edition" button; adapt the modal only to show
  the ABS/Audnexus side-by-side comparison with highlighted differences and
  no Hardcover candidate; no confirmation checkbox (submit sends
  `audnexus_confirmed`).
- [ ] Support identifier correction with re-preview; disable the action for
  unknown or unavailable regions and honor `Retry-After`.
- [ ] Show the returned Hardcover book and edition after success; escape ABS
  and Audnexus strings.
- [ ] Test the button matches other add-edition items, submit sends
  `audnexus_confirmed` with the shown identifier, differences shown, correction re-preview,
  region unavailable, success, errors, and dry run at the web boundary.
- [ ] Update the user-facing README for the UI flow; add one CHANGELOG
  bullet; run the PR gates below.

### PR gates (each PR)

- [ ] Run `gofmt`, `make test`, `make lint`, `make build`, and
  `go build ./cmd/edition-tool` if touched; run
  `node --test web/app.test.js` when web code changes.
- [ ] Use the repository PR template.

## Changelog wording

- **Step 1 — Changed:** `**Audible ASIN audiobooks import without a
  Hardcover match**: Unmatched audiobooks with an Audible ASIN skip title and
  author search; after you confirm the Audible record matches the ABS item,
  the create API and `edition create` import them without a Hardcover book
  ID. By @Snuffy2`.
- **Step 2 — Added:** `**Add Edition for Audible ASIN audiobooks in Sync
  Status**: The Add Edition modal compares the audiobook with its Audible
  record before importing it. By @Snuffy2`.

## Resolved decisions and evidence

1. **No book ID for Audible ASIN audiobooks (user decision):** an audiobook
   with a usable source ASIN is imported through `upsert_book` without
   `book_id`, so it needs no title/author match first. The user confirms that
   the Audnexus record for the regional ASIN matches the ABS item; there is no
   Hardcover-to-ABS review. This applies only to audiobooks with a usable
   ASIN, and it gives those items an in-app path where the edition plan left
   them `not_found`.
2. **No-`book_id` import confirmed:** the owner confirmed against the live
   Hardcover API that `upsert_book` without `book_id` behaves correctly for
   this flow, so no live-API gate remains before implementation.
