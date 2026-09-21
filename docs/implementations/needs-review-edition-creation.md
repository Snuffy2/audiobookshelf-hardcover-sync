# Implementation Plan: Add an Edition to Hardcover from a `needs_review` Book

**Status: 🚧 IN PROGRESS** (2026-09-20)

The feature is delivered as **seven steps**, each its own **upstream** PR (to `drallgood/audiobookshelf-hardcover-sync`)
that leaves `develop` working and shippable. The former combined branch has been split: steps 1-4 exist as four stacked
branches, all validated. Step 1 is published as fork PR #24; steps 2-4 are local only until the owner asks for their fork
PRs. Step 5 is built but still needs a rebase onto step 1. Steps 6 and 7 are not started. No upstream PR exists yet.

**Two stages per step.** `origin` (the fork, `Snuffy2/audiobookshelf-hardcover-sync`) is where each step is developed, tested and
reviewed by AI (CodeRabbit) through a **fork PR** (`Snuffy2:step_N_needs_review_add_edition` -> a fork branch). Only when a
step is ready for the maintainers to consider and merge is an **upstream PR** created
(`Snuffy2:step_N_needs_review_add_edition` -> `drallgood:develop`), and only on the owner's explicit command; nothing
here is ever opened upstream on its own. Fork PR #20 (the old combined branch) is closed and stays closed; its branch is
deleted (see "How the combined branch was split").

## Step Tracker

Update this table as each step lands. Each step must leave `develop` working and shippable on its own. Sizes are
added lines (tests included); "measured" comes from a diff, "estimated" is a guess from the plan and the code the
step touches.

| Step | Scope | Branch | Fork PR | Upstream PR | Depends on | Size | Status |
|------|-------|--------|--------|-------------|-----------|------|--------|
| 1 | ISBN package, reading-format helpers, mismatch export fixes | `step_1_needs_review_add_edition` (tip 30f7e78, 5 commits) | [#24](https://github.com/Snuffy2/audiobookshelf-hardcover-sync/pull/24) (open, base `develop`) | — | `develop` | ~513 (254 prod, 259 tests), measured | Built, validated, pushed to `origin`; fork PR #24 open |
| 2 | Edition creator hardening (duplicate detection, cross-book guard, `edition_format`, token scoping, cover warning, ebook format) and the `edition` CLI field | `step_2_needs_review_add_edition` (tip 1b6f408, 5 commits) | — | — | 1 | ~912 (266 prod, 646 tests), measured | Built and validated; local only, no fork PR yet. See its checklist under "Step checklists" |
| 3 | Read-only draft endpoint (also carries the response write-deadline mechanism, see below) | `step_3_needs_review_add_edition` (tip 61a7ac6, 5 commits) | — | — | 1, 2 | ~2,147 (~1,078 prod and docs, ~1,069 tests), measured | Built and validated; local only, no fork PR yet |
| 4 | Create endpoint, its guards, and docs | `step_4_needs_review_add_edition` (tip 7ce35cd, 4 commits) | — | — | 3 | ~1,525 (~588 prod and docs, ~937 tests), measured | Built and validated; local only, no fork PR yet. Its final tree is identical to the old combined branch's tree |
| 5 | Sync identifier matching | `step_5_needs_review_add_edition` (tip e03754a) | — | — | 1 only | ~500 (~335 tests), measured | Implemented and validated; branch on `origin` (renamed from `feature/sync-identifier-matching` on 2026-09-20), no fork PR yet. Based on an old tip of the combined branch, so it needs a rebase onto `step_1_needs_review_add_edition` (and #190/#191/#192 are already in step 1's base; see the Step 5 notes) |
| 6 | Immediate read-status resync (backend) | `step_6_needs_review_add_edition` | — | — | 4, 5 | ~700-1,000 (about half tests), estimated | Not started |
| 7 | UI: button, preview modal, resync checkbox | `step_7_needs_review_add_edition` | — | — | 6 | ~500-800, estimated | Not started |

**Branch names** are `step_N_needs_review_add_edition` for step N (1-7). The old combined branch
(`feature/edition-from-needs-review-api`, later renamed `legacy_combined_needs_review_add_edition`) no longer exists: it was
deleted locally and on `origin` once steps 1-4 were built.

Stacking and merge order: 1, 2, 3, 4 merge upstream in that order. On the fork, step branches are *stacked* (step N is built
on step N-1) and each step's fork PR uses the previous step's branch as its base, so the AI review sees only that step's
diff. **An upstream PR cannot use a fork-only branch as its base**, so an upstream PR for step N is created (on the owner's
command) only after step N-1 has merged into `drallgood:develop`, with the branch first rebased onto that `develop` so its
diff is just that step. Step 5 needs only step 1's `internal/isbn`, so its fork PR can be based on step 1 and its upstream PR
can follow as soon as step 1 has merged, in parallel with steps 2-4. Step 6 needs the create endpoint (4) and the sync
matching (5) merged, and step 7 needs step 6. Step 6 also needs `develop` at or after a2ad4b4 (#188 changed
`internal/sync/service.go`, which `SyncBook` will call into). After step 4 the feature is usable with curl; step 7 is the
first thing a user sees. Steps 6 and 7 build on the create response shape from step 4, so a change requested in step 4's
review carries into them. Each step's CHANGELOG lines carry its own upstream PR number as `(#NNN)`, added when that
upstream PR is created; fork PR numbers such as #20 are not that number.

## Step checklists

**These checklists are the follow-up list. Anything not written here will not happen.** Before preparing a step's PR (fork
or upstream), open this section, do or consciously drop every unchecked item under that step, and tick it here. When a new
follow-up turns up during the work, add it here under the right step at once (in the same commit as the change that found
it), rather than only in a report or a reply. Keep the tracker table's Status column in step with these lists.

**Every step (fork PR and upstream PR)**
- [ ] Before opening: `gofmt`, `go build`, `go vet`, `make test`, `make lint` (Go 1.26.7 toolchain), `node --test web/app.test.js`
      all pass, and the branch is rebased onto the latest `develop`. Only open a PR on the owner's command.
- [ ] The PR description follows `AGENTS.md` and `.github/pull_request_template.md`, has no issue links (fork PR, and the
      upstream base check), no attribution lines, and carries the "Multi-Step Project" block with this step marked `(this PR)`.
- [ ] After a fork PR is opened, read the CodeRabbit and other review feedback and address every valid item (do not wait to be
      asked), then report the disposition of each item.
- [ ] When a step merges upstream: strike it through in the block below (and leave its text unedited), update this tracker, and
      rebase the next step onto the new upstream `develop`.
- [ ] The CHANGELOG `(#NNN)` is the upstream PR number; add it only when that upstream PR exists.
- [ ] Nothing here has run against real Hardcover (image upload, `insert_edition`, its duplicate behavior for ISBN/ASIN, and
      the ebook reading format id 4 with the `Ebook` label) or live Audnex. Say so in each PR's testing notes for steps 2-4,
      and do a manual check against a real Hardcover account before the upstream PRs for steps 2-4 if the owner wants one.

**Step 1** (fork PR #24 is open)
- [ ] Read the CodeRabbit feedback on #24 and address the valid items (F1, the stale publisher ID in `ToEditionExport`, is
      already fixed in this step).
- [ ] The PR body says `isbn.Result.ISBN10()/ISBN13()/Counterpart` have their first production caller in step 2; keep that note.

**Step 2** (`edition` CLI behavior changes)
- [ ] Add CHANGELOG lines for the Audiobookshelf token scoping (`SetAudiobookshelfBaseURL`) and the cover-upload failure
      signal (`EditionResult.ImageError`, and `Existing` in the `edition` command's JSON output). Neither branch nor the old
      combined branch has them.
- [ ] The fork PR description says `Creator.SetAudiobookshelfBaseURL` has no production caller until step 4 (`internal/multiuser`), so
      token scoping is tested here but inert for the `edition` CLI until then; and that the `edition` command's behavior changes
      (honored `edition_format`, duplicate and cross-book detection, `existing` in its output, optional `reading_format`).
- [ ] Decide CodeRabbit item F3: the pre-existing `CheckRedirect` in `edition.NewCreator` that copies the `Authorization` header
      to redirects. Either fix it here or open a separate small PR (owner's choice); do not let it drop.

**Step 3**
- [ ] Have the CHANGELOG lines carry their final wording where possible, so step 4's CHANGELOG diff is additive instead of
      reshuffling earlier lines (today step 4 replaces step 3's draft-only entry and folds step 2's ebook line into step 1's).
- [ ] The PR description says the response write-deadline mechanism (`extendEditionWriteDeadline`, `Unwrap()` in the logger, and
      `multiuser.EditionCreateTimeout`) lives here because a draft makes many paced Hardcover lookups, and that
      `internal/edition/editiontest` includes helpers first used in step 4 (`HoldInsert`, `HoldSearches`, `FailWith`).
- [ ] Some comments keep create wording (`editionWriteDeadline`, `editionRequestIDs`, the `newHeldCreateFixture` test helper name)
      so step 4 stays additive; reword them only if it does not make step 4 non-additive.

**Step 4**
- [ ] The PR description says its CHANGELOG diff also reshuffles a few earlier lines into the single combined entry, and that
      `CreateEditionFromRunBook` has no caller until the handler commit (consider squashing those two commits).
- [ ] Re-check CodeRabbit item F2 (the shutdown drain of in-flight creates) against this step's code.

**Step 5** (built; needs work before its fork PR)
- [ ] Rebase `step_5_needs_review_add_edition` onto `step_1_needs_review_add_edition`; expect conflicts with #190 in
      `internal/sync/service.go` and `internal/api/hardcover/client.go`; re-check the "unchanged by decision" claims (the
      reading-format filters) against the current code; re-run all gates.
- [ ] Add a test that the ASIN query keeps its reading-format filter (currently only the ISBN queries are guarded).
- [ ] Tighten the CHANGELOG sentence about bad-checksum ISBNs.
- [ ] Reword commit 5190293's message (it still mentions ordering, which was removed), while rebasing.
- [ ] `mismatch.AddWithMetadata`'s own enrichment searches do not try the converted ISBN form; decide whether to include it or
      leave it out of scope, and note the decision.

**Step 6** (not started; concurrency-sensitive)
- [ ] All items in the plan sections "Backend 3-4" and the Step 6 tests: `Service.SyncBook`, `bookOps` exclusivity, opt-in
      `resync`, dry-run skip, `-race` tests, and the regression test that `StartSync` is unaffected when no book operation runs.

**Step 7** (not started)
- [ ] All items in the "Frontend" section and its tests, plus: show the draft's `reading_format` and hide narrator and
      duration fields for an ebook; render the 409 (no identifier, existing edition not confirmed) and 422 messages; the
      user-facing README note and the Known limitation text.
- [ ] Verify in the browser pane (button only on eligible needs_review records, modal, resync result, error and dry-run paths,
      the modal surviving a status poll).

**Known, unrelated**
- [ ] `TestProcessBookSnapshotKeepsEnrichedSecondLookupFailure` is flaky on the second run of `-count>=2` (shared
      `/tmp/test-cache`); it also fails on `develop`. Not caused by this feature; consider a separate fix.

## Step summary for PR descriptions

Every step's fork PR and upstream PR description carries the same "Multi-Step Project" section, in the style of upstream PR #186. It sits
after the "Summary of Changes" and before "Testing Instructions", and ends with a link to this document. Rules:

- Completed steps (already merged) are struck through with `~~...~~` and their text is not edited afterwards.
- The step the PR delivers is marked `(this PR)`; later steps stay plain.
- The current and future steps may be reworded as the build progresses. When one changes, update the block here first,
  then paste it into the open PRs.
- Each step stays to one or two sentences.

The block to paste (with `(this PR)` moved to the PR's own step and merged steps struck through):

```markdown
## Multi-Step Project

1. ISBN and export foundations: Add shared ISBN normalization and reading-format helpers, and fix the mismatch export (hyphenated ISBNs are kept, no default publisher, ebook items export as ebook editions).

2. Edition creator hardening: Make the edition creator reuse an existing edition of the same book by ASIN or ISBN and refuse another book's, honor the requested edition format, send the Audiobookshelf token only to its own server, report cover failures, and create ebook editions.

3. Edition draft endpoint: Add a read-only endpoint that drafts a new Hardcover edition from a needs-review book's Audiobookshelf item, with author, narrator, and publisher resolved.

4. Edition create endpoint: Add the endpoint that creates the drafted edition on Hardcover, with request validation, a per-book in-flight guard, shutdown draining, and dry-run support.

5. Sync identifier matching: Match an existing edition by ASIN, ISBN-13, or ISBN-10 (either form) during sync so the book no longer lands in Needs review.

6. Immediate read-status resync: After an edition is created, re-sync that one book's read status right away, without overlapping a full sync and without waiting for the next one.

7. Sync Status UI: Add an "Add edition to Hardcover" action on needs-review records, with a preview, confirmation, and a resync option.

[Full Plan Document](https://github.com/Snuffy2/audiobookshelf-hardcover-sync/blob/docs/needs-review-edition-plan/docs/implementations/needs-review-edition-creation.md)
```

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

## Delivery: seven steps, not one

The whole feature is too large for one reviewable PR. The repo's recent history (#183-#187) is small vertical
slices that each merge on their own, so it is split the same way. The plan began as three slices, grew a sync
matching slice, and the first slice then grew to about 5,000 added lines (2,050 production, 3,000 tests, 435 docs)
because it mixed changes to existing behavior with a new API. Splitting it by layer gives four PRs, so the plan is
now seven steps. Nothing is user-visible until step 7, so no half-finished button ships.

- **Step 1 - ISBN and export foundations** (`step_1_needs_review_add_edition`, from `develop`). The `internal/isbn`
  package, `models.ReadingFormat`/`ReadingFormatID` and the reading-format context helper (with
  `hardcover.WithReadingFormat` delegating to it), and the mismatch export fixes: the unresolved-publisher default
  (1 -> 0), the hyphenated-ISBN split, publisher ID capture, ebook `reading_format` in `BookMismatch`/`EditionExport`
  (ebook label, no `Unabridged`, no audio length), and removal of the unused `ToEditionInput`. It also swaps the
  sync service's local reading-format helper for `AudiobookshelfBook.ReadingFormat()`. It changes existing export
  output, so its behavior changes each get their own commit and a PR-description note.
- **Step 2 - Edition creator hardening** (`step_2_needs_review_add_edition`, stacked on 1). `edition.Creator`:
  Audiobookshelf token scoping, the honored `edition_format`, proactive duplicate detection by ASIN, ISBN-13, ISBN-10
  and converted forms with the cross-book guard (`ErrEditionBelongsToOtherBook`, `EditionResult.Existing`), the
  duplicate-error fallback via the same lookup, the cover `ImageError`, `EditionInput.ReadingFormat` (ebook: reading
  format 4, `Ebook` label, no narrators or audio length, format-scoped lookups), `GetEditionByISBN10`, and the
  `edition` CLI's optional `reading_format`. It changes what the `edition` CLI does, so this is its own PR.
- **Step 3 - Draft endpoint** (`step_3_needs_review_add_edition`, stacked on 2). `GET .../edition-draft` (with the response write-deadline mechanism and `Unwrap()` on the logger's response wrapper, since a draft makes many paced lookups):
  `audiobookshelf.Client.GetLibraryItem`, the `internal/edition/draft` package (reusing `AddWithMetadata` ->
  `ToEditionExport`), `MultiUserService.PrepareEditionDraft` with eligibility (needs_review, numeric Hardcover book,
  ebook or audiobook, identifier requirement), the `newHardcoverClient` extraction, admission checks, the handler
  and route, the shared `internal/edition/editiontest` fakes, README and OpenAPI for the draft, and CHANGELOG.
  A read-only endpoint that works on its own and is exercisable with curl.
- **Step 4 - Create endpoint** (`step_4_needs_review_add_edition`, stacked on 3).
  `POST .../edition`: `CreateEditionFromRunBook`, request validation and normalization, the in-flight guard, the
  dedicated edition wait group so `Shutdown` cancels syncs before draining creates, the detached 2-minute create
  context, the create handler and
  route, and the write-deadline, shutdown and cover tests, README, OpenAPI and CHANGELOG. It has neither resync nor
  the full-sync lock: creating an edition touches neither the profile state file nor the sync caches.
- **Step 5 - Sync identifier matching** (`step_5_needs_review_add_edition`, stacked on step 1 only). An existing
  edition with the same ASIN, ISBN-13 or ISBN-10 must be matched by the sync instead of ending up under Needs
  review, which is also what makes an edition created by step 4 useful. It uses step 1's `internal/isbn` and
  deliberately changes existing sync matching (see the Step 5 notes).
- **Step 6 - Immediate read-status resync (backend)** (`step_6_needs_review_add_edition`). Backend 3
  (`Service.SyncBook`), the `bookOps` exclusivity with `StartSyncWithAcceptedRun`, the `resync` request field and
  response block, and the dry-run skip. This is the concurrency-sensitive part and gets reviewed on its own. It
  depends on steps 4 and 5: its own `findBookInHardcover` call must find the created edition, including through the
  ISBN-10/13 forms. Because step 3 and 4 refuse an edition for a book with neither an ASIN nor an ISBN, the resync
  only ever applies to books that have an identifier.
- **Step 7 - UI** (`step_7_needs_review_add_edition`). The Frontend section, JS tests, README user-facing note and
  the Known limitation. All APIs are already reviewed by then. The button is shown only for `needs_review` records
  that have a Hardcover candidate and an ASIN or ISBN (the outcome record carries `asin` and `isbn`), and the UI must
  render the 409 messages (no identifier; an existing edition that could not be confirmed to belong to the book) and
  the 422 messages. It shows the draft's `reading_format` and hides the narrator and duration fields for an ebook.

**Standalone rule - every step must leave `develop` fully working and shippable on its own:**
- **Builds and passes on its own:** `gofmt`, `go build ./...`, `go vet ./...`, `make test`, `make lint`, and
  `node --test web/app.test.js` are all green at the tip of each step, run *before* the step is reported done.
- **No dead or half-wired surface:** a step adds only routes/UI that work end to end within that step. No UI
  references an endpoint that doesn't exist yet; no endpoint depends on a later step. Steps 1 and 2 add helpers and
  creator behavior that the `edition` CLI and the mismatch export already use, so nothing they add is unreachable.
  Docs (`README.md`, `docs/openapi.yaml`, `CHANGELOG.md`) describe only what that step actually delivers.
- **Existing behavior unchanged unless the step says so:** ordinary full syncs, `StartSync`/`CancelSync`, the
  status/details APIs, the mismatch export and the `edition` CLI behave as before. The intentional changes are: step
  1's export changes (publisher default, hyphenated ISBN, ebook items exported as ebook editions); step 2's
  `edition.Creator` changes, which the `edition` CLI shares (`edition_format`, duplicate and cross-book detection,
  `reading_format`); and step 5's identifier search (an edition stored under the other ISBN form is now matched). The
  `newHardcoverClient` extraction from `performSync` (step 3) is a pure behavior-preserving refactor, covered by the
  existing multiuser tests. Each change is isolated in its own commit and called out in its PR description.
- **Additive API evolution:** the `resync` request field is **opt-in (default `false`)** so step 6 does not change
  what a step 4 caller gets; the response's `resync` block is additive. The step 7 UI sends `resync: true` from its
  checked-by-default checkbox.
- **Step 6 regression guard:** the `StartSync` lock check only ever triggers while a book operation is in flight,
  which only the new endpoint creates. A test proves `StartSync` is unaffected when none is active.

## How the combined branch was split (done, 2026-09-20)

The combined branch `feature/edition-from-needs-review-api` (40 commits over `develop`, rebased onto `90b4906`, tip
a6e9de9) held steps 1-4. Its commits were interleaved fixes and cleanups, so each step's branch was built from the previous
one by taking that step's files and hunks, as a few clean commits, then gated: `gofmt`, build, vet, `make test`, lint on
the Go 1.26.7 toolchain, the JS tests, and every commit builds. An independent validator confirmed all four, that no
step contains code from a later one, and that `step_4` has **no diff at all** against the old combined branch, so nothing
was lost. Both `upstream/develop` and `origin/develop` were at `90b4906` at the time.

What happened to the old branch and PR:

- Steps 1-4 were built locally in order; only step 1 was pushed and opened as fork PR #24 (on the owner's command).
- The old combined branch was renamed with GitHub's branch-rename API and then deleted, locally and on `origin`.
  **The rename closed fork PR #20** (it did not follow the new name, contrary to what this plan had assumed). The owner
  chose to leave #20 closed. Lesson: do not rename the head branch of an open PR expecting it to survive; a closed PR keeps
  its comments, and the CodeRabbit threads on it are re-checked in whichever step's fork PR contains that code.
- Steps 2, 3 and 4 stay local until the owner asks for their fork PRs; each fork PR is based on the previous step's
  branch. Upstream PRs are created only on the owner's command, one at a time as described under "Stacking and merge
  order": step 1 first; then 2, 3, 4 each after the previous has merged, and step 5 after step 1. Each is rebased onto the
  current upstream `develop` first and carries the "Multi-Step Project" block.

Where the files actually went (differs slightly from the first plan):

- **Step 1:** `internal/isbn/`, `internal/models/reading_format*.go` (helpers and the `AudiobookshelfBook.ReadingFormat`
  method), `internal/mismatch/` (hyphenated ISBN, publisher default and captured ID, ebook export, removal of the unused
  `ToEditionInput`), the reading-format delegation in `internal/api/hardcover/client.go`, the `book.ReadingFormat()` calls
  in `internal/sync/service.go`, and its CHANGELOG lines.
- **Step 2:** `internal/edition/creator.go` and its tests, `GetEditionByISBN10` in `internal/api/hardcover/client.go`,
  `cmd/edition/README.md`, and its CHANGELOG lines.
- **Step 3:** `internal/api/audiobookshelf/client.go` (+ test), `internal/edition/draft/`, `internal/edition/editiontest/`
  (the whole shared-fakes package), the draft half of `internal/multiuser/edition.go` and `service.go`
  (`newHardcoverClient`, `admissionErrorLocked`, `checkEditionAdmission`), `GetEditionDraft` and its route, and **the
  response write-deadline mechanism** (`extendEditionWriteDeadline`, `editionWriteDeadline`, `Unwrap()` in
  `internal/logger/logger.go`, and `multiuser.EditionCreateTimeout`), because a draft makes many paced Hardcover lookups and
  needs the extended deadline too. The draft docs and its CHANGELOG entry.
- **Step 4:** the create half of `internal/multiuser/edition.go` and `service.go` (validation, in-flight guard, edition wait
  group and `Shutdown` drain), the POST route and `CreateEdition` handler, the create, cover and shutdown tests, and the
  create docs. It restores the wording that steps 1-3 had narrowed, so its CHANGELOG diff also reshuffles a few earlier
  lines into the single combined "Create a Hardcover edition" entry.

The follow-ups found while splitting are tracked as checkboxes in "Step checklists" near the top of this document, under the
step they belong to.

**Current state (2026-09-20):** steps 1-4 are built and validated (step 1 published as fork PR #24; 2-4 local only). Step 5 is
built and needs its rebase. No upstream PR exists. Steps 6 and 7 do not exist. Each remaining step is started only on the
owner's go-ahead. Each pushed branch tracks `origin/<same-name>`; nothing is pushed or opened without permission.

## Implementation notes for the original combined Slice 1 (now steps 1-4)

These notes were recorded when steps 1-4 were still one slice ("Slice 1"), implemented and validated, then extended. The
feature-to-step mapping is in "How the combined branch was split". They first described branch tip 374b7b2 (13 commits
over `develop` a2ad4b4); that branch (last tip a6e9de9, 40 commits over `develop` 90b4906) is now deleted and its content lives in
steps 1-4; fork PR #20, its head, is closed. The sections below were written against earlier tips (8248c1a and
before); the commit hashes they cite predate the rebases onto `develop`, so match them by subject rather than by hash.
Where this differs from the plan above, this section is what shipped.

**Cleanup done after the ebook work** (a cleanup loop over the branch, all local): the ebook rule and Hardcover
reading-format ids now live once in `internal/models` (`AudiobookshelfBook.ReadingFormat`, `ReadingFormatID`) instead
of in the sync service, the draft package and both the Hardcover client and creator; the creator's duplicate-error
fallback reuses `findExistingEdition` (so it also covers ISBN-10); ISBN-10/13 request normalization shares one helper;
the unused `mismatch.ToEditionInput` and its `EditionCreatorInput` type were removed; and the three copies of the
Hardcover and Audiobookshelf test fakes (service, API and server tests) became one package,
`internal/edition/editiontest`.

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
  94c5de8, cafe76d, 8248c1a) keep them in step with the code. The CHANGELOG entries still lack the `(#NNN)` PR number,
  which is the upstream PR number of the step that carries them, added when that PR is opened.
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

### Review of fork PR #20 (CodeRabbit)

- **F1**, publisher ID stale in `ToEditionExport`: fixed (9cd84b0).
- **F2**, the shutdown wait: fixed (see Shutdown above).
- **F3**, the pre-existing CLI-only `CheckRedirect` in `edition.NewCreator` copying `Authorization` to redirects:
  skipped for this PR as pre-existing; a candidate for a separate small PR.
- **F4**, docstring-coverage check: skipped (no repo rule; lint is clean).

The review threads were not yet replied to or resolved when this was written, and PR #20 has since been closed (see "How the
combined branch was split"). The threads are re-checked in whichever step's fork PR contains the code they refer to: F1 in step 1
(mismatch export), F2 in step 4 (shutdown), F3 in step 2 (creator/CLI).

## Step 5 implementation notes

Branch `step_5_needs_review_add_edition`, formerly `feature/sync-identifier-matching` (4 commits over the old combined-branch tip 8248c1a: 5190293, 378bbc7, 08492ac,
e03754a; on `origin` at e03754a, no PR yet). It is about 500 added lines, of which about 335 are tests. **It has not been
rebased onto the current combined branch, and the plan now stacks it on step 1 only** (it needs `internal/isbn`), so
its rebase target is step 1's branch (and `develop` eb50565 with #190). #190 (ebook detection and matching) changed
the same two files, `internal/sync/service.go` and `internal/api/hardcover/client.go` (it added the reading-format
context and format-aware ASIN/ISBN filters), so expect conflicts there, and step 1 also touches `service.go` (the
`book.ReadingFormat()` calls). Re-check the "unchanged by decision" claim below (the reading-format filters) against
the new code. The reading-format context helper now lives in `internal/models` (`WithReadingFormat`), and
`hardcover.WithReadingFormat` delegates to it (step 1). Six files change: `internal/sync/service.go`,
`internal/api/hardcover/client.go`, their tests (`internal/sync/isbn_matching_test.go`,
`internal/api/hardcover/search_identifier_test.go`, `outcomes_test.go`) and `CHANGELOG.md`.

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

1. **Preview modal, then confirm** (not one-click). Step 7.
2. **Button only for `needs_review` records with a Hardcover candidate.** Steps 3-4 eligibility; step 7 adds the
   identifier condition.
3. **Resync right after creating.** Step 6, opt-in via `resync`.
4. **Reuse `AddWithMetadata` -> `ToEditionExport`** instead of a parallel builder, with the publisher-default fix. Steps 1 and 3.
5. **Steps are standalone and additive** (first three slices, then four, now seven steps); the resync is opt-in.
6. **Shutdown:** first skipped, then fixed once a reviewer reproduced that `Shutdown` skipped cancelling syncs. Step 4.
7. **Books without an ASIN or ISBN cannot create an edition** (409). Step 3 (draft) and step 4 (create).
8. **Format rule:** only same-format matches count (an audiobook matches only an audiobook edition, an ebook item only an
   ebook edition); a different or unset format is not a match. Steps 2 and 5.
9. **Sync-matching changes go in a separate follow-up PR** built on the API work. That became step 5, which now depends only on step 1.
10. **The unverified `order_by` is not used.** Step 5.
11. **Ebook-only items get ebook editions** (the "support" option), not a rejection. Steps 1-4 (export, creator, draft, create). The format is decided
    by the Audiobookshelf item on the server, never by the request.
12. **Split the combined branch into seven steps** (owner: "steps" over "slices"). The first slice had grown to about
    5,000 added lines, so it becomes steps 1-4 by layer (export foundations, creator, draft endpoint, create endpoint) and
    the sync matching, resync and UI become steps 5-7. Step 5 depends only on step 1. The split itself is not done yet.
13. **Two stages per step.** `origin` is for development, testing and AI review (fork PRs, CodeRabbit); an upstream PR is
    created only on the owner's explicit command, once the step is ready for the maintainers. An upstream PR cannot be
    based on a fork-only branch, so fork PRs stack on the previous step's branch but upstream PRs open one at a time after
    the previous step merged, rebased onto upstream `develop`. The CHANGELOG `(#NNN)` is the upstream PR number.
14. **The combined branch is retired.** Steps 1-4 were split out, the old branch deleted, and fork PR #20 left closed (the
    rename that retired it closed the PR; see "How the combined branch was split").

## Ebook items: ebook editions (resolved, steps 1-4)

**Decision (owner): an ebook-only `needs_review` item gets an ebook edition through the same routes.** This
replaces the earlier open question, which offered rejecting ebooks or supporting them. A rejection was implemented
briefly and then reverted in favor of support.

Background: Audiobookshelf reports `mediaType: "book"` for ebooks too. #190 (on `develop`) recognizes an
ebook-only item from its media content with `AudiobookshelfBook.IsEbook()` (an ebook file or format and no audio),
honors `include_ebooks`, and matches ebook items to Hardcover **ebook** editions (reading format id 4). An ebook-only
item can therefore end a run as `needs_review` and is eligible for the button.

What steps 1-4 do now (creator in step 2, export in step 1, draft in step 3, create in step 4):

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

For the step 7 UI: show the draft's `reading_format` in the preview so the user can see whether an audiobook or an
ebook edition will be created, and hide the narrator and duration fields for an ebook.

## Reuse (already in the repo)

- `internal/edition/creator.go`: `EditionInput`, `Creator.CreateEdition` (validates, dry-run
  short-circuit, duplicate check by ASIN, ISBN-13, ISBN-10 and converted forms (a same-book edition is
  returned untouched, another book's is refused; see the implementation notes), GraphQL `insert_edition`, cover upload),
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

1. **[Step 3] ABS client** — `internal/api/audiobookshelf/client.go`: add
   `GetLibraryItem(ctx, itemID) (*models.AudiobookshelfBook, error)` → `GET /api/items/{id}?expanded=1`
   (same auth/timeout style as `GetLibraryItems`; 404 -> typed not-found error). Not added to
   `AudiobookshelfClientInterface` (avoids breaking existing mocks); callers use the concrete client.

2. **[Steps 1 and 3] Draft = existing pipeline, one small fix** (the fix in `internal/mismatch/mismatch.go` is step 1; the new `internal/edition/draft/draft.go`, package `draft`, is step 3; see the implementation notes):
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

3. **[Step 6] Single-book sync** — `internal/sync/service.go`: new exported
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

4. **Service orchestration** — new `internal/multiuser/edition.go`. Step 3: draft, eligibility, admission.
   Step 4: per-book in-flight guard, create, dry-run. Step 6: full-sync exclusivity (`bookOps`) and the resync call.
   - `PrepareEditionDraft(ctx, profileID, runID, bookID)` and
     `CreateEditionFromRunBook(ctx, profileID, runID, bookID, edits, resync bool)`.
   - Both load the run snapshot via `GetSyncRunSnapshot`, require a record with matching `book_id`,
     `outcome == needs_review`, and a numeric `hardcover_book_id`; otherwise `ErrEditionNotEligible`.
     The Hardcover book ID comes from the record, never the client.
   - Admission via `beginSyncStart`/`endSyncStart` so shutdown and profile deletion are respected.
   - **[Step 4] Double-submit guard:** an in-flight set keyed by `profileID/bookID` -> `ErrEditionInProgress`
     (409). Plain edition creation touches neither the profile state file nor the sync caches, so in step 4 it
     may run alongside a full sync and does not touch `StartSyncWithAcceptedRun` at all.
   - **[Step 6] Exclusivity, only when `resync` is requested:** the one-book resync writes the same per-profile
     state file and caches as a full sync, so it must never overlap one. Add a `bookOps` set (guarded by
     `syncMutex`): a create-with-resync is rejected up front with `ErrSyncAlreadyActive` (409 "sync in progress,
     try again when it finishes", before any edition is created) if a full sync is active/queued for the
     profile, and `StartSyncWithAcceptedRun` rejects (same sentinel -> 409) while a resync holds the profile.
     Creation *without* resync keeps the step 4 behavior.
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

5. **[Step 2] Creator token scoping** — `internal/edition/creator.go` (~line 221) attaches the ABS token only
   when the URL `Contains("audiobookshelf")`, which fails for hosts like `abs.home`. Add an optional
   `audiobookshelfBaseURL` (setter) so the token is sent only to URLs under that base; keep the legacy
   behavior when unset so `cmd/edition` is unchanged.

6. **[Steps 3 and 4; `resync` field and response block added in step 6] HTTP handlers + routes** — new `internal/api/handlers_edition.go`, routes in
   `internal/server/server.go` under `apiMux` (auth middleware already wraps `/api/`):
   - `GET  /api/profiles/{id}/runs/{runID}/books/{bookID}/edition-draft`
   - `POST /api/profiles/{id}/runs/{runID}/books/{bookID}/edition`
     body: editable scalars, ID lists; from step 6, an optional `resync` (default `false`, opt-in).
   - Both use `authorizeProfileMetadata(..., mutation=true)` (viewers 403; foreign profiles 404). POST body
     capped with `http.MaxBytesReader` (64 KiB), unknown fields rejected.
   - Status mapping: bad IDs 400; profile/run/book not found or not eligible 404/409; same-book submit in flight
     409 (step 4); full sync active while `resync` is requested 409 (step 6); validation failure (e.g. no author
     resolved) 422 with a readable message; upstream ABS/Hardcover failure 502.
     POST success: `{edition_id, dry_run}` in step 4; step 6 adds `resync: {attempted, outcome, reason, error}`.
     Added after validation: the step 4 POST success `data` is `{edition_id, dry_run, warnings}`, where `warnings` is a
     `[]string` that is always present (an empty array when nothing went wrong). It holds one fixed message when the
     edition was created but its cover could not be uploaded; the status stays 200.
     Also added after validation: 409 when the book has neither an ASIN nor an ISBN, or when an existing edition could
     not be confirmed to belong to the book; 422 when a create request has none of `asin`, `isbn_10`, `isbn_13`; 503
     while the service is shutting down.

## [Step 7] Frontend (`web/static/app.js`, `index.html`, `styles.css`)

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

## Known limitation (step 7 docs; also stated in the step 6 handoff)

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

## Tests (step noted per group; each step ships its own tests)

- **Step 3** — `internal/api/audiobookshelf/client_test.go`: `GetLibraryItem` success, 404, auth header/path.
- **Step 1** — `internal/mismatch/mismatch_test.go`: other `AddWithMetadata` tests (incl. Audnex region fallback) pass unchanged;
  the publisher assertion is updated and a resolved-publisher case added.
- **Step 3** — `internal/edition/draft/draft_test.go` (fake Hardcover client): people/publisher IDs carried through, Hardcover book ID
  taken from the run record, unresolved author/publisher/date -> warnings, cover URL forced to the ABS base URL,
  ISBN forms (`ToInput()` was removed).
- **Step 2** — `internal/edition/creator_test.go`: ABS token attached only under the configured base URL.
  `internal/edition/creator_reuse_test.go`: an existing edition is detected by every identifier before inserting, and
  another book's edition is refused. `internal/multiuser/edition_cover_test.go` (step 4): an ISBN match never touches another
  book's edition.
- **Step 1** — `internal/isbn/isbn_test.go`: `Normalize`, `Parse` and the derived forms.
- **Steps 1-4** — ebook editions: `internal/edition/creator_reuse_test.go` (ebook and audiobook dto fields, lookups scoped
  to the input's format, invalid `reading_format`), `internal/mismatch/reading_format_test.go` (ebook export),
  `internal/edition/draft/draft_test.go` (ebook draft) and `internal/api/handlers_edition_test.go` (ebook draft and
  create, a request cannot set `reading_format`).
- **Step 5** — `internal/api/hardcover/search_identifier_test.go` and `internal/sync/isbn_matching_test.go`: the given ISBN
  form is searched first and then the counterpart, each in its own field; ISBN normalization (lowercase `x`,
  separators); the ASIN is trimmed and blank identifiers are ignored; a 979 or bad-checksum ISBN has no counterpart
  search; the ISBN reading-format filter is kept (audiobook 2, ebook 4).
- **Step 6** — `internal/sync/`: `SyncBook` via existing test mocks — a book with an in-progress ABS state and a newly
  discoverable ASIN edition ends `synced` with the read/status mutations issued and state checkpointed;
  already-synced -> `already_current`; unread with `process_unread_books` off -> `skipped`; Hardcover lookup
  failure -> `failed`; **dry-run issues no Hardcover mutation and persists no state** (real mutation boundary).
- **Steps 3 and 4** — `internal/api/handlers_edition_test.go` (fixture style of `handlers_status_test.go`; httptest ABS +
  Hardcover): non-`needs_review` / no candidate -> not eligible; viewer 403, foreign owner 404; draft success;
  create sends the record's Hardcover `book_id` and ignores client-supplied `book_id`/`image_url`; dry-run -> no
  Hardcover mutation; same-book concurrent submit -> 409; validation -> 422; creation is unaffected by an active
  full sync; `StartSync`/`CancelSync` behave exactly as before.
- **Step 6** — same file plus `internal/multiuser`: resync outcome returned; resync failure still returns 200 with
  the failure in `resync`; dry-run -> no resync; `resync=true` while a full sync is active -> 409 with **no edition
  created**; `StartSync` -> 409 while a resync is in flight and **unaffected when none is** (regression guard);
  `-race` for the exclusivity tests.
- **Step 7** — `web/app.test.js`: button only for eligible `needs_review` records and hidden for viewers; result
  state replaces the button; modal HTML escapes ABS-supplied strings; checkbox hidden for dry-run; request sends
  `resync: true` only when checked.

## Docs (each step documents only what it delivers)

- **Steps 1-4:** `README.md` two endpoint rows (draft in step 3, create in step 4, no resync) and a short API note (identifier requirement,
  ISBN matching, hyphen handling); `docs/openapi.yaml` for both operations; `CHANGELOG.md` `[Unreleased]` -> `### Added`,
  plus `### Changed`/`### Fixed` lines for the publisher export default, the honored `edition_format`, the hyphenated
  ISBN fix and the cross-book guard, plus the ebook support (`reading_format`, the mismatch export change, and the
  `edition` CLI field). Each step adds only the lines for what it delivers: the export fixes in step 1, the creator changes in step 2, and
  the draft and create endpoints (README, OpenAPI) in steps 3 and 4.
- **Step 5:** `CHANGELOG.md` entry "Sync finds an edition stored under the other ISBN form".
- **Step 6:** the opt-in `resync` field/response block in README and OpenAPI; CHANGELOG entry.
- **Step 7:** the user-facing "Add an edition from Sync Status" note (eligibility, preview/confirm, immediate
  read-status resync, dry-run behavior, the Known limitation); CHANGELOG entry.

## Verification (run at the tip of **each** step before reporting it done)

1. `gofmt -l .` clean; `go build ./...`; `go vet ./...`; `go build ./cmd/edition-tool`.
2. Focused `go test` for the touched packages (steps 1-2: `./internal/isbn/... ./internal/models/... ./internal/mismatch/... ./internal/edition/... ./internal/api/hardcover/...`;
   steps 3-4 add `./internal/api/... ./internal/multiuser/... ./internal/server/...`; step 5:
   `./internal/sync/... ./internal/api/hardcover/... ./internal/isbn/...`; step 6 adds `-race`).
3. `make test` (race + coverage) and `make lint` (golangci-lint; needs the CI-matching Go 1.26.7 toolchain first on
   `PATH`).
4. `node --test web/app.test.js` (all steps; step 7 adds new cases).
5. Exercise the step's real surface: steps 3, 4 and 6 by curl or the httptest fixture against stub ABS/Hardcover
   servers; steps 1, 2 and 5 through their unit and sync tests against a stub Hardcover; step 7 in the browser pane (button only on
   eligible `needs_review` records, modal shows the draft, edits POST, resync result renders, error/dry-run paths, modal survives a status poll). Confirm a normal
   full sync and Sync Status still behave as before. A live create against real Hardcover is not part of
   automated verification — I will state that explicitly in each handoff.
6. Commit locally on the step's branch; report that nothing
   was pushed.
