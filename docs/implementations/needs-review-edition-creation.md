# Implementation Plan: Add an Edition to Hardcover from a `needs_review` Book

**Status: 🚧 IN PROGRESS** (2026-09-20)

Slices 1 and 2 are implemented and validated; Slices 3 and 4 are not started. Slice 1 also creates ebook editions for
ebook-only items (see "Ebook items"). Slice 2 needs a rebase onto the current Slice 1 tip.

## Slice Tracker

Update this table as each slice lands. Each slice must leave `develop` working and shippable on its own.

| Slice | Branch | PR | Status |
|-------|--------|----|--------|
| 1 — Create-edition API | `feature/edition-from-needs-review-api` | [#20](https://github.com/Snuffy2/audiobookshelf-hardcover-sync/pull/20) (open, ready for review, base `develop`) | Implemented and validated, including ebook editions; rebased onto `develop` eb50565 (#190); the PR body is stale relative to the branch and must be rewritten |
| 2 — Sync identifier matching | `feature/sync-identifier-matching` | — | Implemented and validated; branch on `origin`, no PR yet; based on an old Slice 1 tip, so it needs a rebase onto the current one and will likely conflict with #190 (see the Slice 2 notes) |
| 3 — Immediate read-status resync (backend) | `feature/needs-review-edition-resync` | — | Not started |
| 4 — UI | `feature/needs-review-edition-ui` | — | Not started |

Stacking: Slice 2 builds on Slice 1 (it needs Slice 1's `internal/isbn`), Slice 3 on Slices 1 and 2, and Slice 4 on
Slice 3. While Slice 1's PR is unmerged, Slice 2's PR base is the Slice 1 branch; afterwards it is `develop`. Slice 3
also needs `develop` at or after a2ad4b4 (#188 changed `internal/sync/service.go`, which `SyncBook` will call into).


## Context

When a sync run marks a book `needs_review` (matched by title/author only, or the Hardcover
book has no verified edition), the only remedy today is to hand-edit exported mismatch JSON
and run the separate `edition` CLI, then wait for the next full sync. This adds an in-app path:
a button on each `needs_review` record in the Sync Status details view that builds a new
Hardcover edition from the book's Audiobookshelf metadata, shows a preview, creates it on
confirmation, and **immediately re-syncs that one book's read status** through the normal
per-book sync path — no waiting for the next sync.

Decisions confirmed with the user:
- **Preview modal, then confirm** (not one-click).
- Button only for `needs_review` records that have a `hardcover_book_id` (an edition must attach
  to an existing Hardcover book).
- After creating the edition, re-sync that book's read status right away ("in that sync loop").

## Delivery: four PRs, not one

This is too large for one reviewable PR (ABS client, a behavior fix in the existing export, the edition
creator, a new sync entry point, locking against full syncs, two API routes, and a modal UI). The repo's
recent history (#183-#187) is small vertical slices that each merge on their own, so split the same way.
Nothing is user-visible until Slice 4, so no half-finished button ships. The plan started as three slices; the
sync identifier matching was added as Slice 2 (see decision 9 in the Decision log), so the resync and UI became
Slices 3 and 4.

- **Slice 1 — Create-edition API** (branch `feature/edition-from-needs-review-api`, from `develop`).
  Backend 1, 2, 5, 6 and the draft/create path of 4, **without** resync and without the full-sync lock:
  creating an edition touches neither the profile state file nor the sync caches, so it only needs a
  per-`profileID/bookID` in-flight guard. Includes README endpoint rows, `docs/openapi.yaml`, `CHANGELOG.md`.
  The `AddWithMetadata` publisher-default fix goes in its **own commit** (it changes existing export behavior).
  Exercisable with curl. It grew during review; see the Slice 1 implementation notes for what shipped.
- **Slice 2 — Sync identifier matching** (branch `feature/sync-identifier-matching`, stacked on Slice 1).
  An existing edition with the same ASIN, ISBN-13 or ISBN-10 must be matched by the sync instead of ending up
  under Needs review, which is also what makes an edition created by Slice 1 useful. It uses Slice 1's
  `internal/isbn` and deliberately changes existing sync matching (see the Slice 2 implementation notes).
- **Slice 3 — Immediate read-status resync (backend)** (branch `feature/needs-review-edition-resync`).
  Backend 3 (`Service.SyncBook`), the `bookOps` exclusivity with `StartSyncWithAcceptedRun`, the `resync`
  request field and response block, and the dry-run skip. This is the concurrency-sensitive part and gets
  reviewed on its own. It depends on Slices 1 and 2: its own `findBookInHardcover` call must find the created
  edition, including through the ISBN-10/13 forms. Because Slice 1 refuses to create an edition for a book with
  neither an ASIN nor an ISBN, the resync only ever applies to books that have an identifier; the earlier concern
  that title/author-only books cannot be fixed by creating an edition is closed by that block.
- **Slice 4 — UI** (branch `feature/needs-review-edition-ui`). The Frontend section, JS tests, README
  user-facing note and the Known limitation. All APIs are already reviewed by then. The button is shown only for
  `needs_review` records that have a Hardcover candidate and an ASIN or ISBN (the outcome record carries `asin`
  and `isbn`), and the UI must render the 409 messages (no identifier; an existing edition that could not be
  confirmed to belong to the book) and the 422 messages.

**Standalone rule — every slice must leave `develop` fully working and shippable on its own:**
- **Builds and passes on its own:** `gofmt`, `go build ./...`, `go vet ./...`, `make test`, `make lint`, and
  `node --test web/app.test.js` are all green at the tip of each slice, run *before* the slice is reported done.
- **No dead or half-wired surface:** a slice adds only routes/UI that work end to end within that slice. No UI
  references an endpoint that doesn't exist yet; no endpoint depends on a later slice. Docs (`README.md`,
  `docs/openapi.yaml`, `CHANGELOG.md`) describe only what that slice actually delivers.
- **Existing behavior unchanged unless the slice says so:** ordinary full syncs, `StartSync`/`CancelSync`, the
  status/details APIs, the mismatch export and the `edition` CLI behave as before. Slice 1's intentional changes
  to existing behavior are the `AddWithMetadata` publisher-default fix, the hyphenated-ISBN export fix, and the
  honored `edition_format` and duplicate/cross-book detection in `edition.Creator` (which the `edition` CLI shares);
  each is isolated in its own commit and called out in the PR description. Slice 2's is the sync's identifier
  search (an edition stored under the other ISBN form is now matched). The `newHardcoverClient` extraction from
  `performSync` is a pure behavior-preserving refactor, covered by the existing multiuser tests.
- **Additive API evolution:** the `resync` request field is **opt-in (default `false`)** so Slice 3 does not
  change what a Slice 1 caller gets; the response's `resync` block is additive. The Slice 4 UI sends
  `resync: true` from its checked-by-default checkbox.
- **Slice 3 regression guard:** the `StartSync` lock check only ever triggers while a book operation is in
  flight, which only the new endpoint creates. A test proves `StartSync` is unaffected when none is active.

**Current state (2026-09-20):** Slices 1 and 2 are implemented and validated. Slice 1 is open as PR #20; the Slice 2
branch is on `origin` but has no PR yet, and Slices 3 and 4 do not exist yet. Each remaining slice is started only
on the owner's go-ahead. Each branch is created from `develop` (stacked on the previous branch while that one is
unmerged) and tracks `origin/<same-name>` via `git config branch.<name>.remote/merge`; nothing is pushed without
permission.

## Slice 1 implementation notes

Recorded after Slice 1 was implemented and validated, then extended. It first described branch tip 374b7b2
(13 commits over `develop` a2ad4b4); the branch is now at ad2750b (34 commits over `develop` eb50565, which includes #190)
and is the head of PR #20. The last commit squashes three changes (a rejection of ebook items, its revert, and ebook
edition support) into the net ebook support; its subject line still reads "reject ebook-only items" and is misleading.
The sections below were written against earlier tips (8248c1a and before); the commit hashes they cite predate the
rebases onto `develop`, so match them by subject rather than by hash.
Where this differs from the plan above, this section is what shipped.

- **Draft sub-package.** The draft builder is `internal/edition/draft/draft.go` (`draft.New`, `draft.Draft`,
  `draft.CoverURL`), not `internal/edition/draft.go` / `edition.NewDraft`. It is a sub-package because `edition` ->
  `mismatch` -> `api/hardcover` -> `edition` would be an import cycle.
- **Extra request validation** (`validateEditionInput` in `internal/multiuser/edition.go`, failures return 422):
  author and narrator ID lists are at most 50 entries each and every ID must be positive; publisher, language,
  country and audio length must not be negative; `edition_format` is limited to 100 runes after trimming
  ("edition format must be at most 100 characters").
- **Extra statuses** beyond the plan: 503 when the service is shutting down, 409 when the profile is being deleted,
  400 for an oversize body or a body that is not exactly one JSON object, and 401/500 from the handler path
  (authentication and unexpected failures). The two further 409s (no identifier; existing edition not confirmed)
  are described under "Added after the first validation".
- **Detached create context.** Creation runs on a context detached from the request (`context.WithoutCancel`) with
  a 2-minute timeout, so a client disconnect cannot leave an edition without its cover.
- **Cover URL hardening (9d3b3e6).** `draft.CoverURL` strips credentials, query and fragment from the profile's
  Audiobookshelf base URL before building the cover URL.
- **`newHardcoverClient` refactor (91d6d29).** The extracted helper's debug log no longer carries `profile_id`.
- **Edition format fix (240d2fa).** `Creator.createEdition` previously hardcoded `edition_format: "Audiobook"` and
  ignored `EditionInput.EditionFormat`. It now sends the trimmed requested format and falls back to "Audiobook" when
  empty; `reading_format_id` stays 2. This also changes the behavior of the `edition` CLI. The Hardcover schema shows
  `BookDtoInput.edition_format` is a free-text String.
- **Cover failure signal (319955d).** Cover upload failures were previously swallowed. `EditionResult` now has an
  additive `ImageError` (`image_error,omitempty`, a fixed step label only), and the POST response carries a
  `warnings` array (see Backend 6). The status is still 200 when only the cover failed.
- **Test seam (12b2c47).** An unexported `newEditionCreator` seam on `MultiUserService` allows service-level tests
  proving the ABS token goes only to the ABS cover host, and that removing the service's `hcClient.SetDryRun` makes
  the dry-run test fail.
- **Docs commits.** 85e610d and 374b7b2 hold the first README, OpenAPI and CHANGELOG changes; later commits (7290c10,
  94c5de8, cafe76d, 8248c1a) keep them in step with the code. The CHANGELOG entries still lack the `(#NNN)` PR number
  (#20), to be added.
- **Repeat submits.** The in-flight guard only stops concurrent submits. The original limitation (a repeat sequential
  submit creating a duplicate, because the run record stays `needs_review`) is now largely closed by the proactive
  duplicate detection below; what remains is in "Known limitation".
- **Validation.** Independently validated PASS: build, vet, `make test`, `make test-all`, `-race`, the JS tests and lint.
  `make lint` passes only with the CI-matching Go 1.26.7 toolchain first on `PATH`; the default Go 1.27.1 cannot
  typecheck the repo with the pinned golangci-lint. A blind adversarial review returned PASSED and the claim audit
  returned CLEAR after three iterations; the create-side requirements added afterwards were validated PASS again.
- **Not exercised:** nothing ran against real Hardcover (image upload, `insert_edition`, its duplicate handling) or
  live Audnex.

### Added after the first validation

Grounded in the code on the branch at that time (8248c1a); the ebook support that followed is in "Ebook items".

- **Cross-book guard.** `edition.Creator` refuses to adopt an existing edition that belongs to another book or whose
  book cannot be confirmed (`ErrEditionBelongsToOtherBook`); the API answers 409 with a fixed message ("An edition with
  this ASIN or ISBN already exists on Hardcover and could not be confirmed to belong to this book."). A reused
  same-book edition is returned untouched (no cover upload, no mutation), and `EditionResult.Existing` records this
  (the `edition` CLI prints `"existing": true`). Commits e3de025, 60fd16f, c6a2d65.
- **Shutdown.** A dedicated edition wait group means `Shutdown` first cancels running syncs and then drains in-flight
  creates (f6b79c9); a draft holds no admission gate (it only refuses to start once shutdown or profile deletion has
  begun). Profile deletion still waits for an in-flight create (documented tradeoff).
- **Response write deadline.** The two routes use `audiobookshelf.RequestTimeout` (30s) + `EditionCreateTimeout` (2m) +
  15s, which required adding `Unwrap()` to the logger middleware's response writer (6151e70, 71eadba).
- **Small cleanups.** Blank titles are rejected (422); `Draft.ToInput` was removed as unused (2a1a3d7).
- **ISBN helpers.** New leaf package `internal/isbn` (f703cb5): `Normalize`, and `Parse` with 978 <-> ISBN-10
  conversion; the counterpart form is derived only when the input checksum is valid, and a 979 ISBN-13 has no ISBN-10.
- **Hyphenated-ISBN fix (15e0d05).** The old length-only ISBN split in `mismatch.AddWithMetadata` dropped hyphenated
  ISBNs from the mismatch export and the draft. It now uses `internal/isbn`, and the draft also fills the derived
  counterpart form.
- **Identifier requirement (c156853, fd9510b).** A book whose Audiobookshelf item has neither an ASIN nor a parseable
  ISBN gets 409 (`ErrEditionNoIdentifier`, fixed message) on both the draft and the create route, before any Hardcover
  call. A create request needs at least one of `asin`, `isbn_10`, `isbn_13` (422), and its ISBNs are normalized and
  shape-checked. Such books can still be `needs_review` through the title/author match but can never be auto-matched,
  so an edition created for them could never help a sync.
- **Proactive duplicate detection (f5ff579).** Before inserting, the creator looks up an existing edition by ASIN, then
  ISBN-13, then ISBN-10, then the derived counterpart forms (audiobook format only; up to three extra reads; skipped in
  dry run). The "already exists" insert-error fallback remains as a safety net. `GetEditionByISBN10` was added to
  `hardcover.Client`.
- **Format and publisher.** `edition_format` is honored by the creator (trimmed, fallback "Audiobook", at most 100
  characters else 422), which also affects the `edition` CLI. The mismatch export's unresolved publisher is 0 instead of
  1, and a publisher resolved during export is exported (bbfb8f1, 9cd84b0).
- **Response shape.** The create response is `{edition_id, dry_run, warnings}`; `warnings` carries one fixed message if
  the cover could not be uploaded (`EditionResult.ImageError` internally).

### Review of PR #20 (CodeRabbit)

- **F1**, publisher ID stale in `ToEditionExport`: fixed (9cd84b0).
- **F2**, the shutdown wait: fixed (see Shutdown above).
- **F3**, the pre-existing CLI-only `CheckRedirect` in `edition.NewCreator` copying `Authorization` to redirects:
  skipped for this PR as pre-existing; a candidate for a separate small PR.
- **F4**, docstring-coverage check: skipped (no repo rule; lint is clean).

The review threads were not yet replied to or resolved when this was written.

## Slice 2 implementation notes

Branch `feature/sync-identifier-matching` (4 commits over the old Slice 1 tip 8248c1a: 5190293, 378bbc7, 08492ac, e03754a;
on `origin` at e03754a, no PR yet). **It has not been rebased onto the current Slice 1 tip (ad2750b) or `develop`
eb50565.** #190 (ebook detection and matching) changed the same two files, `internal/sync/service.go` and
`internal/api/hardcover/client.go` (it added the reading-format context and format-aware ASIN/ISBN filters), so expect
conflicts there and re-check the "unchanged by decision" claim below (the reading-format filters) against the new
code. Also, #190 moved the reading-format context helper; Slice 1 now keeps it in `internal/models`
(`WithReadingFormat`) and `hardcover.WithReadingFormat` delegates to it. Six files change: `internal/sync/service.go`, `internal/api/hardcover/client.go`, their tests
(`internal/sync/isbn_matching_test.go`, `internal/api/hardcover/search_identifier_test.go`, `outcomes_test.go`) and
`CHANGELOG.md`.

- **Purpose.** An existing edition with the same ASIN, ISBN-13 or ISBN-10 must be matched by the sync instead of ending
  up under Needs review. A code map found the gaps in the identifier search: an Audiobookshelf ISBN-10 missed an edition
  storing only the ISBN-13 and the reverse (no 978 conversion); `searchBookByISBN` did not normalize a lowercase `x` or
  separators other than `-` and space; the ASIN was not trimmed. `needs_review` only arises when every identifier search
  misses and a title/author candidate exists, because a title/author hit is always a mismatch.
- **What it does.** `findBookInHardcover` builds an ISBN candidate list from the Audiobookshelf ISBN via
  `internal/isbn`, searches the form the item has first and then the derived counterpart, each in its own field
  (`SearchBookByISBN13` / `SearchBookByISBN10`), and stops at the first hit. An unparseable value falls back to the
  previous behavior. `searchBookByISBN` normalizes with `isbn.Normalize`, and the ASIN is trimmed (trim only; case is
  not changed).
- **Unchanged by decision.** The strict same-format rule and every reading-format filter (the query text is
  byte-identical), the title/author flow, `processFoundBook`, the ASIN cache and `canonical_id`.
- **Removed: `order_by: { id: asc }`.** A deterministic-ordering addition was tried and removed (e03754a) because it could
  not be verified against the hosted Hardcover API, and `AGENTS.md` warns that the hosted API disables some schema
  operators; a rejection would fail every ASIN/ISBN lookup.
- **Behavior changes versus the base**, all rated improvements by the validator: the given form is searched first, then
  the counterpart; a 13-digit ISBN with no derivable counterpart (979 or a bad checksum) is no longer also searched in
  the ISBN-10 field; whitespace-only ISBNs are skipped; normalization applies to every caller of the client; the ASIN is
  trimmed. The query count never exceeds the base.
- **Validation.** Independently validated PASS (full suite, `-race`, lint on the Go 1.26.7 toolchain). The only failure
  was the pre-existing flaky `TestProcessBookSnapshotKeepsEnrichedSecondLookupFailure` on the second run of `-count=3`
  from a shared `/tmp/test-cache`, identical at the base. A merge probe against another local branch,
  `fix/ebook-detection-and-matching` (someone else's ebook detection work), showed only a CHANGELOG conflict; the code
  auto-merges, builds and passes tests, and its intent is compatible with the strict same-format rule.
- **Known follow-ups.** An ASIN-query test does not guard the format filter; one CHANGELOG sentence about bad-checksum
  ISBNs could be tighter; commit 5190293's message still mentions ordering; `mismatch.AddWithMetadata`'s own enrichment
  searches do not try the counterpart form (out of scope).
- **Not verified:** nothing ran against real Hardcover.

## Decision log

Decisions made by the owner and where they ended up.

1. **Preview modal, then confirm** (not one-click). Slice 4.
2. **Button only for `needs_review` records with a Hardcover candidate.** Slice 1 eligibility; Slice 4 adds the
   identifier condition.
3. **Resync right after creating.** Slice 3, opt-in via `resync`.
4. **Reuse `AddWithMetadata` -> `ToEditionExport`** instead of a parallel builder, with the publisher-default fix. Slice 1.
5. **Slices are standalone and additive** (first three, now four); the resync is opt-in.
6. **Shutdown:** first skipped, then fixed once a reviewer reproduced that `Shutdown` skipped cancelling syncs. Slice 1.
7. **Books without an ASIN or ISBN cannot create an edition** (409). Slice 1.
8. **Format rule:** only same-format matches count (an audiobook matches only an audiobook edition, an ebook item only an
   ebook edition); a different or unset format is not a match. Slices 1 and 2.
9. **Sync-matching changes go in a separate follow-up PR** built on Slice 1. That became Slice 2.
10. **The unverified `order_by` is not used.** Slice 2.
11. **Ebook-only items get ebook editions** (the "support" option), not a rejection. Slice 1. The format is decided
    by the Audiobookshelf item on the server, never by the request.

## Ebook items: ebook editions (resolved, Slice 1)

**Decision (owner): an ebook-only `needs_review` item gets an ebook edition through the same routes.** This
replaces the earlier open question, which offered rejecting ebooks or supporting them. A rejection was implemented
briefly and then reverted in favor of support.

Background: Audiobookshelf reports `mediaType: "book"` for ebooks too. #190 (on `develop`) recognizes an
ebook-only item from its media content with `AudiobookshelfBook.IsEbook()` (an ebook file or format and no audio),
honors `include_ebooks`, and matches ebook items to Hardcover **ebook** editions (reading format id 4). An ebook-only
item can therefore end a run as `needs_review` and is eligible for the button.

What Slice 1 does now:

- **Format comes from the item.** `draft.ReadingFormat(item)` (`ebook` or `audiobook`, the same `IsEbook()` rule as the
  sync) is set by the server on `EditionInput.ReadingFormat`. A create request cannot choose it: `reading_format` is
  not an editable field, so it is rejected as an unknown field (400). The draft reports it as `reading_format`. An
  audiobook that also has an ebook file is still an audiobook.
- **`EditionInput.ReadingFormat`** (`reading_format`, optional: `audiobook` by default, or `ebook`; anything else fails
  validation). For an ebook the creator sends `reading_format_id: 4`, defaults `edition_format` to `Ebook`, and sends
  no narrators and no `audio_seconds`. An audiobook is unchanged (`reading_format_id: 2`, `Audiobook`). The `edition`
  CLI reads the same JSON field, so it can create ebook editions too.
- **Duplicate detection is per format.** The creator puts the input's reading format on the context, so its ASIN and
  ISBN lookups only consider editions of that format (an ebook item never adopts an audiobook edition, and the
  reverse). This is the "same format only" rule (decision 8) applied to creation.
- **Shared context helper.** The reading-format context key moved to `internal/models/reading_format.go`
  (`WithReadingFormat`, `ReadingFormatFromContext`, and the `ReadingFormatAudiobook`/`ReadingFormatEbook` constants);
  `hardcover.WithReadingFormat` delegates to it, so nothing else changes. The creator cannot import the Hardcover
  package (it would be a cycle), and this way it sets the format itself instead of relying on every caller.
- **Mismatch export.** `BookMismatch` and `EditionExport` carry an optional `reading_format`. For an ebook,
  `AddWithMetadata` records `Ebook`/`ebook`, and `ToEditionExport` uses the `Ebook` format, skips the audiobook
  platform hints and the `Unabridged` default, and exports no audio length. An audiobook exports exactly as before.
  This changes exports of ebook items only (they were audiobook-shaped before), and is in the CHANGELOG.
- **Draft.** An ebook draft has no narrators, no narrator warning, no audio length, and no `Unabridged` default.
- **Docs and tests.** README, OpenAPI, the CHANGELOG and `cmd/edition/README.md` describe it. Tests cover the creator
  (ebook and audiobook fields, format-scoped lookups, invalid format), the mismatch export, the draft, and the API
  (ebook draft and create, a request that tries to set `reading_format`).

Still not verified against live Hardcover: that `insert_edition` accepts `reading_format_id: 4` with the `Ebook`
edition format label, and that its ISBN/ASIN duplicate behavior for ebooks matches the audiobook case. The format
filters on the lookups are covered only through fakes. Kindle ASINs for ebooks are not checked against Audible-only
lookups.

For the Slice 4 UI: show the draft's `reading_format` in the preview so the user can see whether an audiobook or an
ebook edition will be created, and hide the narrator and duration fields for an ebook.

## Reuse (already in the repo)

- `internal/edition/creator.go`: `EditionInput`, `Creator.CreateEdition` (validates, dry-run
  short-circuit, duplicate check by ASIN, ISBN-13, ISBN-10 and converted forms (a same-book edition is
  returned untouched, another book's is refused; see the Slice 1 notes), GraphQL `insert_edition`, cover upload),
  `NewCreatorWithHTTPClient`.
- `internal/mismatch`: `Collector.AddWithMetadata` (5 production callers in `internal/sync`) already
  does the ABS-metadata -> edition-fields mapping: Audnex release date with region fallback and date
  normalization, ISBN-10/13 split, publisher-ID lookup. `BookMismatch.ToEditionExport` already turns that
  into the edition-import shape (author/narrator ID lookup, `Audible Audio` format when an ASIN exists,
  `Unabridged` default, ABS-cover-first image URL). The mismatch JSON files the `edition` CLI imports come
  from exactly this path, so the in-app draft will match what the file flow would have produced.
  Also `people.go`/`publisher.go` lookups.
- `internal/sync/service.go`: `processBook` (the exact per-book logic the full loop runs — lookup by
  ASIN/ISBN, user-book creation, read/progress/status updates, outcome recording),
  `checkpointState`, `enhanceBookProgressFromUserData`, `NewServiceWithRunIdentity`.
  `SearchBookByASIN` is a direct GraphQL `books(where: editions.asin ...)` query, not the search
  index, so a freshly created edition is discoverable immediately.
- `internal/multiuser/service.go`: `GetSyncRunSnapshot`, `GetProfile` (decrypted tokens),
  `createProfileSpecificConfig` (per-profile state-file path), `beginSyncStart`/`endSyncStart`
  (admission: rejects during shutdown/profile deletion and lets `Shutdown` wait), and the Hardcover
  client config block in `performSync` (~1130-1153) — extract into a small `newHardcoverClient(token)`
  helper and reuse.
- `internal/api/handlers_new.go`: `authorizeProfileMetadata`, `writeErrorResponse`, `GetRunDetails` as the
  route/auth pattern; `internal/server/server.go` route table.
- Hardcover client dry-run boundary: `SetDryRun` makes `GraphQLMutation` a no-op.

Decision: **use `AddWithMetadata` -> `ToEditionExport` directly, with no extraction refactor and no
parallel builder.** Accepted costs, all bounded and user-initiated (once per modal open): passing the
Hardcover client (needed for the publisher lookup) also runs its Hardcover-candidate enrichment (a handful of
extra rate-limited calls); its Audnex/publisher lookups use their own 15s/10s timeouts rather than the request
context; it appends to a collector (a throwaway `mismatch.NewCollector()` is used and discarded). The one
real defect — it defaults `PublisherID` to `1` when nothing resolves, so an unresolved publisher would be
written onto a public edition — is fixed in place (see Backend 2).

## Backend

1. **[Slice 1] ABS client** — `internal/api/audiobookshelf/client.go`: add
   `GetLibraryItem(ctx, itemID) (*models.AudiobookshelfBook, error)` → `GET /api/items/{id}?expanded=1`
   (same auth/timeout style as `GetLibraryItems`; 404 -> typed not-found error). Not added to
   `AudiobookshelfClientInterface` (avoids breaking existing mocks); callers use the concrete client.

2. **[Slice 1] Draft = existing pipeline, one small fix** (`internal/mismatch/mismatch.go`, new `internal/edition/draft/draft.go`, package `draft`; see Slice 1 implementation notes):
   - **Fix in place:** in `AddWithMetadata`, stop defaulting `publisherID := 1`; leave `0` (unresolved).
     `ToEditionExport` and `Creator.createEdition` already treat `0` as "no publisher" (`if input.PublisherID > 0`).
     This changes the mismatch JSON export too: an unresolved publisher now exports `publisher_id: 0` instead of
     `1`, so the `edition` CLI would stop stamping publisher 1 on imports. I can't verify offline what Hardcover
     publisher ID 1 is, so this is called out for review. Update the assertion at `mismatch_test.go:488` and add a
     case for "publisher name resolves -> its ID" / "unresolved -> 0". No other change to `AddWithMetadata`.
   - **Draft:** `draft.New(ctx, absBook, hardcoverBookID, absBaseURL, hc, region)`:
     1. `mismatch.NewCollector().AddWithMetadata(MediaMetadata{...from the ABS item...}, book.ID, "", reason,
        duration, book.ID, hc, region)` — the same call the sync service makes (`service.go` ~2327);
     2. set `m.HardcoverBookID` to the run record's Hardcover book ID (overriding whatever enrichment guessed);
     3. `m.ToEditionExport(ctx, hc)` -> `EditionExport` (author/narrator ID lookups, `Audible Audio` format when an
        ASIN exists, `Unabridged`, cover preference);
     4. wrap as `Draft`: the export's fields + display names from `Export.Info` (`author_names`, `narrator_names`,
        `publisher_name`) + `warnings []string` (no author resolved -> `EditionInput.Validate` would fail; missing
        release date; unresolved publisher) + `dry_run`. `Draft.ToInput()` mapped the export to `edition.EditionInput` (removed later as unused).
        `image_url` is forced server-side to `<profile ABS base URL>/api/items/<bookID>/cover`.
   - No new metadata-mapping logic: Audnex date, ISBN split, publisher/author/narrator lookups all stay where they are.

3. **[Slice 3] Single-book sync** — `internal/sync/service.go`: new exported
   `(*Service).SyncBook(ctx, book models.AudiobookshelfBook) (BookOutcomeRecord, error)`:
   - Reset run-local outcome/collector state (without run-phase transitions), clear the in-memory ASIN
     cache and `hardcover.ClearUserBookCache()`, fetch user progress via `GetUserProgress` (marking
     `userProgressUnavailableContextKey` on failure exactly like `Sync`), then call the **existing**
     `processBook` so behavior is identical to the loop (dry-run guards, incremental state, reads,
     status, ownership) — no duplicated sync logic.
   - Call `checkpointState(book.ID)` (persists the profile state file unless dry-run) and save the
     persistent ASIN/user-book caches as `Sync` does at its end.
   - Return the book's final `BookOutcomeRecord` (`synced`, `already_current`, `skipped`,
     `needs_review`, `not_found`, `failed`, `would_sync`) plus reason/error; `ErrSkippedBook` is
     translated into that record rather than surfaced as an error.

4. **Service orchestration** — new `internal/multiuser/edition.go`. Slice 1: draft, eligibility, admission,
   per-book in-flight guard, create, dry-run. Slice 3: full-sync exclusivity (`bookOps`) and the resync call.
   - `PrepareEditionDraft(ctx, profileID, runID, bookID)` and
     `CreateEditionFromRunBook(ctx, profileID, runID, bookID, edits, resync bool)`.
   - Both load the run snapshot via `GetSyncRunSnapshot`, require a record with matching `book_id`,
     `outcome == needs_review`, and a numeric `hardcover_book_id`; otherwise `ErrEditionNotEligible`.
     The Hardcover book ID comes from the record, never the client.
   - Admission via `beginSyncStart`/`endSyncStart` so shutdown and profile deletion are respected.
   - **[Slice 1] Double-submit guard:** an in-flight set keyed by `profileID/bookID` -> `ErrEditionInProgress`
     (409). Plain edition creation touches neither the profile state file nor the sync caches, so in Slice 1 it
     may run alongside a full sync and does not touch `StartSyncWithAcceptedRun` at all.
   - **[Slice 3] Exclusivity, only when `resync` is requested:** the one-book resync writes the same per-profile
     state file and caches as a full sync, so it must never overlap one. Add a `bookOps` set (guarded by
     `syncMutex`): a create-with-resync is rejected up front with `ErrSyncAlreadyActive` (409 "sync in progress,
     try again when it finishes", before any edition is created) if a full sync is active/queued for the
     profile, and `StartSyncWithAcceptedRun` rejects (same sentinel -> 409) while a resync holds the profile.
     Creation *without* resync keeps the Slice 1 behavior.
   - The POST body echoes the draft's editable fields (scalars, ID lists, duration, format, language/country) so
     what was previewed is exactly what is created, with no second round of Hardcover lookups. The server always
     sets `book_id` (from the run record) and `image_url` (from the profile's ABS base URL) itself and never
     accepts them from the request: the Creator attaches the ABS bearer token to the image download, so a
     client-supplied URL would be a token-exfiltration/SSRF vector.
   - Dry-run profile: `hcClient.SetDryRun(true)`, Creator `dryRun=true` (returns `edition_id: 0`), and the
     resync is **not** attempted (`resync.attempted=false, reason="dry run"`) — nothing was created for
     it to find. Honors the AGENTS.md dry-run safeguard.
   - After a real create, if `resync` is requested: fetch the item + progress, build the profile-scoped
     `sync.Service` (`createProfileSpecificConfig`, `NewServiceWithRunIdentity`), call `SyncBook`.
     A failing resync **does not fail** the request — the edition already exists; the failure is
     returned in the `resync` block.
   - Use `NewCreatorWithHTTPClient` with a TLS-verifying client (default `NewCreator` sets
     `InsecureSkipVerify: true`).

5. **[Slice 1] Creator token scoping** — `internal/edition/creator.go` (~line 221) attaches the ABS token only
   when the URL `Contains("audiobookshelf")`, which fails for hosts like `abs.home`. Add an optional
   `audiobookshelfBaseURL` (setter) so the token is sent only to URLs under that base; keep the legacy
   behavior when unset so `cmd/edition` is unchanged.

6. **[Slice 1; `resync` field and response block added in Slice 3] HTTP handlers + routes** — new `internal/api/handlers_edition.go`, routes in
   `internal/server/server.go` under `apiMux` (auth middleware already wraps `/api/`):
   - `GET  /api/profiles/{id}/runs/{runID}/books/{bookID}/edition-draft`
   - `POST /api/profiles/{id}/runs/{runID}/books/{bookID}/edition`
     body: editable scalars, ID lists; from Slice 3, an optional `resync` (default `false`, opt-in).
   - Both use `authorizeProfileMetadata(..., mutation=true)` (viewers 403; foreign profiles 404). POST body
     capped with `http.MaxBytesReader` (64 KiB), unknown fields rejected.
   - Status mapping: bad IDs 400; profile/run/book not found or not eligible 404/409; same-book submit in flight
     409 (Slice 1); full sync active while `resync` is requested 409 (Slice 3); validation failure (e.g. no author
     resolved) 422 with a readable message; upstream ABS/Hardcover failure 502.
     POST success: `{edition_id, dry_run}` in Slice 1; Slice 3 adds `resync: {attempted, outcome, reason, error}`.
     Added after validation: the Slice 1 POST success `data` is `{edition_id, dry_run, warnings}`, where `warnings` is a
     `[]string` that is always present (an empty array when nothing went wrong). It holds one fixed message when the
     edition was created but its cover could not be uploaded; the status stays 200.
     Also added after validation: 409 when the book has neither an ASIN nor an ISBN, or when an existing edition could
     not be confirmed to belong to the book; 422 when a create request has none of `asin`, `isbn_10`, `isbn_13`; 503
     while the service is shutting down.

## [Slice 4] Frontend (`web/static/app.js`, `index.html`, `styles.css`)

- `renderOutcomeRecord` (app.js ~1726): for `record.outcome === 'needs_review'` with a
  `hardcover_book_id` and an `asin` or `isbn` (an edition cannot be created without one), and `!this.isViewer()`,
  render an **Add edition to Hardcover** button (`data-add-edition`, `data-book-id`) in the `.book-service-links` area. If the book was handled this
  session (`open.editionResults[bookId]`), show the result ("Edition created · read status synced" etc.).
- The details view re-renders on every poll (`renderDetailsSnapshot` rewrites `innerHTML`), so the dialog
  must live **outside** `#sync-summary-content`: add `#add-edition-modal` to `index.html` modeled on
  `#edit-user-modal`, plus styles reusing the existing modal classes.
- Delegated click handler for `[data-add-edition]` -> `openAddEditionModal(bookId)`: uses
  `this.openSummary.{profileId,runId}`, fetches the draft with the same auth-generation / abort /
  `handleAuthExpiry` guards as `fetchAndRenderDetails`, renders read-only context (cover, resolved
  author/narrator/publisher names, duration, format, target Hardcover book, warnings) and editable text
  inputs (title, subtitle, ASIN, ISBN-10, ISBN-13, release date, edition information), all through
  `escapeHtml`/`escapeHtmlAttribute`, plus a checked-by-default checkbox
  **"Also sync this book's read status now"** (hidden/disabled for dry-run profiles).
- **Create edition** POSTs; the button disables while in flight; errors render inline; success closes the
  modal, toasts a two-part result (edition created / dry-run note; resync outcome and reason, e.g.
  `synced`, `already_current`, `skipped: unread book`, or `needs_review`/`failed` with its reason), records
  `editionResults[bookId]`, and re-renders.

## Known limitation (Slice 4 docs; also stated in the Slice 3 handoff)

The retained run report is a durable, generation-checked historical record; this change does **not**
rewrite it. After a successful resync the book still appears under Needs review in that run's details
after a page reload until the next full sync; re-clicking is idempotent (the Creator finds the
existing edition by identifier and returns it untouched, and the resync reports `already_current`). Updating the
retained report/counts is a possible follow-up.

### Repeat submits and accepted residual risks

Identifiers are required and duplicates are detected proactively by ASIN, ISBN-13, ISBN-10 and the converted forms, for
the same format only, so a repeat submit normally returns the existing edition. Accepted gaps and risks:

- The shutdown drain (30s default) is shorter than a create (up to 2 minutes), and profile deletion waits behind an
  in-flight create.
- A same-identifier edition of a different or unset format is not matched, so a duplicate is possible if Hardcover does
  not reject it; Hardcover's real "already exists" behavior for duplicate ISBNs is unverified.
- The Hardcover client and its rate limiter are created per request.
- The end-of-sync mismatch export re-runs publisher lookups for unresolved publishers.
- ASIN drafts call the live Audnex API (not stubbed in tests).
- Numeric fields and identifiers have only loose bounds.
- Books with neither an ASIN nor an ISBN cannot get an edition through this feature at all.

## Tests (slice noted per group; each slice ships its own tests)

- **Slice 1** — `internal/api/audiobookshelf/client_test.go`: `GetLibraryItem` success, 404, auth header/path.
- **Slice 1** — `internal/mismatch/mismatch_test.go`: other `AddWithMetadata` tests (incl. Audnex region fallback) pass unchanged;
  the publisher assertion is updated and a resolved-publisher case added.
- **Slice 1** — `internal/edition/draft/draft_test.go` (fake Hardcover client): people/publisher IDs carried through, Hardcover book ID
  taken from the run record, unresolved author/publisher/date -> warnings, cover URL forced to the ABS base URL,
  ISBN forms (`ToInput()` was removed).
- **Slice 1** — `internal/edition/creator_test.go`: ABS token attached only under the configured base URL.
  `internal/edition/creator_reuse_test.go`: an existing edition is detected by every identifier before inserting, and
  another book's edition is refused. `internal/multiuser/edition_cover_test.go`: an ISBN match never touches another
  book's edition.
- **Slice 1** — `internal/isbn/isbn_test.go`: `Normalize`, `Parse` and the derived forms.
- **Slice 1** — ebook editions: `internal/edition/creator_reuse_test.go` (ebook and audiobook dto fields, lookups scoped
  to the input's format, invalid `reading_format`), `internal/mismatch/reading_format_test.go` (ebook export),
  `internal/edition/draft/draft_test.go` (ebook draft) and `internal/api/handlers_edition_test.go` (ebook draft and
  create, a request cannot set `reading_format`).
- **Slice 2** — `internal/api/hardcover/search_identifier_test.go` and `internal/sync/isbn_matching_test.go`: the given ISBN
  form is searched first and then the counterpart, each in its own field; ISBN normalization (lowercase `x`,
  separators); the ASIN is trimmed and blank identifiers are ignored; a 979 or bad-checksum ISBN has no counterpart
  search; the ISBN reading-format filter is kept (audiobook 2, ebook 4).
- **Slice 3** — `internal/sync/`: `SyncBook` via existing test mocks — a book with an in-progress ABS state and a newly
  discoverable ASIN edition ends `synced` with the read/status mutations issued and state checkpointed;
  already-synced -> `already_current`; unread with `process_unread_books` off -> `skipped`; Hardcover lookup
  failure -> `failed`; **dry-run issues no Hardcover mutation and persists no state** (real mutation boundary).
- **Slice 1** — `internal/api/handlers_edition_test.go` (fixture style of `handlers_status_test.go`; httptest ABS +
  Hardcover): non-`needs_review` / no candidate -> not eligible; viewer 403, foreign owner 404; draft success;
  create sends the record's Hardcover `book_id` and ignores client-supplied `book_id`/`image_url`; dry-run -> no
  Hardcover mutation; same-book concurrent submit -> 409; validation -> 422; creation is unaffected by an active
  full sync; `StartSync`/`CancelSync` behave exactly as before.
- **Slice 3** — same file plus `internal/multiuser`: resync outcome returned; resync failure still returns 200 with
  the failure in `resync`; dry-run -> no resync; `resync=true` while a full sync is active -> 409 with **no edition
  created**; `StartSync` -> 409 while a resync is in flight and **unaffected when none is** (regression guard);
  `-race` for the exclusivity tests.
- **Slice 4** — `web/app.test.js`: button only for eligible `needs_review` records and hidden for viewers; result
  state replaces the button; modal HTML escapes ABS-supplied strings; checkbox hidden for dry-run; request sends
  `resync: true` only when checked.

## Docs (each slice documents only what it delivers)

- **Slice 1:** `README.md` two endpoint rows (draft + create, no resync) and a short API note (identifier requirement,
  ISBN matching, hyphen handling); `docs/openapi.yaml` for both operations; `CHANGELOG.md` `[Unreleased]` -> `### Added`,
  plus `### Changed`/`### Fixed` lines for the publisher export default, the honored `edition_format`, the hyphenated
  ISBN fix and the cross-book guard, plus the ebook support (`reading_format`, the mismatch export change, and the
  `edition` CLI field).
- **Slice 2:** `CHANGELOG.md` entry "Sync finds an edition stored under the other ISBN form".
- **Slice 3:** the opt-in `resync` field/response block in README and OpenAPI; CHANGELOG entry.
- **Slice 4:** the user-facing "Add an edition from Sync Status" note (eligibility, preview/confirm, immediate
  read-status resync, dry-run behavior, the Known limitation); CHANGELOG entry.

## Verification (run at the tip of **each** slice before reporting it done)

1. `gofmt -l .` clean; `go build ./...`; `go vet ./...`; `go build ./cmd/edition-tool`.
2. Focused `go test` for the touched packages (Slice 1: `./internal/mismatch/... ./internal/edition/...
   ./internal/api/... ./internal/multiuser/... ./internal/server/... ./internal/isbn/...`; Slice 2:
   `./internal/sync/... ./internal/api/hardcover/... ./internal/isbn/...`; Slice 3 adds `-race`).
3. `make test` (race + coverage) and `make lint` (golangci-lint; needs the CI-matching Go 1.26.7 toolchain first on
   `PATH`).
4. `node --test web/app.test.js` (all slices; Slice 4 adds new cases).
5. Exercise the slice's real surface: Slices 1 and 3 by curl or the httptest fixture against stub ABS/Hardcover
   servers; Slice 2 through the sync tests against a stub Hardcover; Slice 4 in the browser pane (button only on
   eligible `needs_review` records, modal shows the draft, edits POST, resync result renders, error/dry-run paths, modal survives a status poll). Confirm a normal
   full sync and Sync Status still behave as before. A live create against real Hardcover is not part of
   automated verification — I will state that explicitly in each handoff.
6. Commit locally on the slice's branch (`feature/edition-from-needs-review-api` first); report that nothing
   was pushed.
