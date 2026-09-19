# Implementation Plan: Add an Edition to Hardcover from a `needs_review` Book

**Status: 🚧 PLANNED** (2026-09-19)

## Slice Tracker

Update this table as each slice lands. Each slice must leave `develop` working and shippable on its own.

| Slice | Branch | PR | Status |
|-------|--------|----|--------|
| 1 — Create-edition API | `feature/edition-from-needs-review-api` | — | Not started |
| 2 — Immediate read-status resync (backend) | `feature/needs-review-edition-resync` | — | Not started |
| 3 — UI | `feature/needs-review-edition-ui` | — | Not started |


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

## Delivery: three PRs, not one

This is too large for one reviewable PR (ABS client, a behavior fix in the existing export, the edition
creator, a new sync entry point, locking against full syncs, two API routes, and a modal UI). The repo's
recent history (#183-#187) is small vertical slices that each merge on their own, so split the same way.
Nothing is user-visible until Slice 3, so no half-finished button ships.

- **Slice 1 — Create-edition API** (branch `feature/edition-from-needs-review-api`, from `develop`).
  Backend 1, 2, 5, 6 and the draft/create path of 4, **without** resync and without the full-sync lock:
  creating an edition touches neither the profile state file nor the sync caches, so it only needs a
  per-`profileID/bookID` in-flight guard. Includes README endpoint rows, `docs/openapi.yaml`, `CHANGELOG.md`.
  The `AddWithMetadata` publisher-default fix goes in its **own commit** (it changes existing export behavior).
  Exercisable with curl.
- **Slice 2 — Immediate read-status resync (backend)** (branch `feature/needs-review-edition-resync`).
  Backend 3 (`Service.SyncBook`), the `bookOps` exclusivity with `StartSyncWithAcceptedRun`, the `resync`
  request field and response block, and the dry-run skip. This is the concurrency-sensitive part and gets
  reviewed on its own.
- **Slice 3 — UI** (branch `feature/needs-review-edition-ui`). The Frontend section, JS tests, README
  user-facing note and the Known limitation. All APIs are already reviewed by then.

**Standalone rule — every slice must leave `develop` fully working and shippable on its own:**
- **Builds and passes on its own:** `gofmt`, `go build ./...`, `go vet ./...`, `make test`, `make lint`, and
  `node --test web/app.test.js` are all green at the tip of each slice, run *before* the slice is reported done.
- **No dead or half-wired surface:** a slice adds only routes/UI that work end to end within that slice. No UI
  references an endpoint that doesn't exist yet; no endpoint depends on a later slice. Docs (`README.md`,
  `docs/openapi.yaml`, `CHANGELOG.md`) describe only what that slice actually delivers.
- **Existing behavior unchanged unless the slice says so:** ordinary full syncs, `StartSync`/`CancelSync`, the
  status/details APIs, the mismatch export and the `edition` CLI behave as before. The only intentional
  change to existing behavior is the `AddWithMetadata` publisher-default fix, isolated in its own commit and
  called out in the PR description. The `newHardcoverClient` extraction from `performSync` is a pure
  behavior-preserving refactor, covered by the existing multiuser tests.
- **Additive API evolution:** the `resync` request field is **opt-in (default `false`)** so Slice 2 does not
  change what a Slice 1 caller gets; the response's `resync` block is additive. The Slice 3 UI sends
  `resync: true` from its checked-by-default checkbox.
- **Slice 2 regression guard:** the `StartSync` lock check only ever triggers while a book operation is in
  flight, which only the new endpoint creates. A test proves `StartSync` is unaffected when none is active.

**Approval scope:** approving this plan authorizes **Slice 1 only**. I will commit locally, report, and stop
before Slice 2, since Slices 2 and 3 build on it. If you'd rather I continue straight through, say so and the
slices become stacked branches. Each branch is created from `develop` (stack on the previous branch only if it
has not merged yet) and tracks `origin/<same-name>` via `git config branch.<name>.remote/merge` — no push
without permission.

## Reuse (already in the repo)

- `internal/edition/creator.go`: `EditionInput`, `Creator.CreateEdition` (validates, dry-run
  short-circuit, ASIN-exists check that returns the existing edition instead of duplicating,
  GraphQL `insert_edition`, cover upload), `NewCreatorWithHTTPClient`.
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

2. **[Slice 1] Draft = existing pipeline, one small fix** (`internal/mismatch/mismatch.go`, new `internal/edition/draft.go`):
   - **Fix in place:** in `AddWithMetadata`, stop defaulting `publisherID := 1`; leave `0` (unresolved).
     `ToEditionExport` and `Creator.createEdition` already treat `0` as "no publisher" (`if input.PublisherID > 0`).
     This changes the mismatch JSON export too: an unresolved publisher now exports `publisher_id: 0` instead of
     `1`, so the `edition` CLI would stop stamping publisher 1 on imports. I can't verify offline what Hardcover
     publisher ID 1 is, so this is called out for review. Update the assertion at `mismatch_test.go:488` and add a
     case for "publisher name resolves -> its ID" / "unresolved -> 0". No other change to `AddWithMetadata`.
   - **Draft:** `edition.NewDraft(ctx, absBook, hardcoverBookID, absBaseURL, hc, region)`:
     1. `mismatch.NewCollector().AddWithMetadata(MediaMetadata{...from the ABS item...}, book.ID, "", reason,
        duration, book.ID, hc, region)` — the same call the sync service makes (`service.go` ~2327);
     2. set `m.HardcoverBookID` to the run record's Hardcover book ID (overriding whatever enrichment guessed);
     3. `m.ToEditionExport(ctx, hc)` -> `EditionExport` (author/narrator ID lookups, `Audible Audio` format when an
        ASIN exists, `Unabridged`, cover preference);
     4. wrap as `Draft`: the export's fields + display names from `Export.Info` (`author_names`, `narrator_names`,
        `publisher_name`) + `warnings []string` (no author resolved -> `EditionInput.Validate` would fail; missing
        release date; unresolved publisher) + `dry_run`. `Draft.ToInput()` maps the export to `edition.EditionInput`.
        `image_url` is forced server-side to `<profile ABS base URL>/api/items/<bookID>/cover`.
   - No new metadata-mapping logic: Audnex date, ISBN split, publisher/author/narrator lookups all stay where they are.

3. **[Slice 2] Single-book sync** — `internal/sync/service.go`: new exported
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
   per-book in-flight guard, create, dry-run. Slice 2: full-sync exclusivity (`bookOps`) and the resync call.
   - `PrepareEditionDraft(ctx, profileID, runID, bookID)` and
     `CreateEditionFromRunBook(ctx, profileID, runID, bookID, edits, resync bool)`.
   - Both load the run snapshot via `GetSyncRunSnapshot`, require a record with matching `book_id`,
     `outcome == needs_review`, and a numeric `hardcover_book_id`; otherwise `ErrEditionNotEligible`.
     The Hardcover book ID comes from the record, never the client.
   - Admission via `beginSyncStart`/`endSyncStart` so shutdown and profile deletion are respected.
   - **[Slice 1] Double-submit guard:** an in-flight set keyed by `profileID/bookID` -> `ErrEditionInProgress`
     (409). Plain edition creation touches neither the profile state file nor the sync caches, so in Slice 1 it
     may run alongside a full sync and does not touch `StartSyncWithAcceptedRun` at all.
   - **[Slice 2] Exclusivity, only when `resync` is requested:** the one-book resync writes the same per-profile
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

6. **[Slice 1; `resync` field and response block added in Slice 2] HTTP handlers + routes** — new `internal/api/handlers_edition.go`, routes in
   `internal/server/server.go` under `apiMux` (auth middleware already wraps `/api/`):
   - `GET  /api/profiles/{id}/runs/{runID}/books/{bookID}/edition-draft`
   - `POST /api/profiles/{id}/runs/{runID}/books/{bookID}/edition`
     body: editable scalars, ID lists; from Slice 2, an optional `resync` (default `false`, opt-in).
   - Both use `authorizeProfileMetadata(..., mutation=true)` (viewers 403; foreign profiles 404). POST body
     capped with `http.MaxBytesReader` (64 KiB), unknown fields rejected.
   - Status mapping: bad IDs 400; profile/run/book not found or not eligible 404/409; same-book submit in flight
     409 (Slice 1); full sync active while `resync` is requested 409 (Slice 2); validation failure (e.g. no author
     resolved) 422 with a readable message; upstream ABS/Hardcover failure 502.
     POST success: `{edition_id, dry_run}` in Slice 1; Slice 2 adds `resync: {attempted, outcome, reason, error}`.

## [Slice 3] Frontend (`web/static/app.js`, `index.html`, `styles.css`)

- `renderOutcomeRecord` (app.js ~1726): for `record.outcome === 'needs_review'` with a
  `hardcover_book_id`, and `!this.isViewer()`, render an **Add edition to Hardcover** button
  (`data-add-edition`, `data-book-id`) in the `.book-service-links` area. If the book was handled this
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

## Known limitation (Slice 3 docs; also stated in the Slice 2 handoff)

The retained run report is a durable, generation-checked historical record; this change does **not**
rewrite it. After a successful resync the book still appears under Needs review in that run's details
after a page reload until the next full sync; re-clicking is idempotent (the Creator's ASIN check returns
the existing edition and the resync reports `already_current`). Updating the retained report/counts is a
possible follow-up.

## Tests (slice noted per group; each slice ships its own tests)

- **Slice 1** — `internal/api/audiobookshelf/client_test.go`: `GetLibraryItem` success, 404, auth header/path.
- **Slice 1** — `internal/mismatch/mismatch_test.go`: other `AddWithMetadata` tests (incl. Audnex region fallback) pass unchanged;
  the publisher assertion is updated and a resolved-publisher case added.
- **Slice 1** — `internal/edition/draft_test.go` (fake Hardcover client): people/publisher IDs carried through, Hardcover book ID
  taken from the run record, unresolved author/publisher/date -> warnings, cover URL forced to the ABS base URL,
  `ToInput()` mapping.
- **Slice 1** — `internal/edition/creator_test.go`: ABS token attached only under the configured base URL.
- **Slice 2** — `internal/sync/`: `SyncBook` via existing test mocks — a book with an in-progress ABS state and a newly
  discoverable ASIN edition ends `synced` with the read/status mutations issued and state checkpointed;
  already-synced -> `already_current`; unread with `process_unread_books` off -> `skipped`; Hardcover lookup
  failure -> `failed`; **dry-run issues no Hardcover mutation and persists no state** (real mutation boundary).
- **Slice 1** — `internal/api/handlers_edition_test.go` (fixture style of `handlers_status_test.go`; httptest ABS +
  Hardcover): non-`needs_review` / no candidate -> not eligible; viewer 403, foreign owner 404; draft success;
  create sends the record's Hardcover `book_id` and ignores client-supplied `book_id`/`image_url`; dry-run -> no
  Hardcover mutation; same-book concurrent submit -> 409; validation -> 422; creation is unaffected by an active
  full sync; `StartSync`/`CancelSync` behave exactly as before.
- **Slice 2** — same file plus `internal/multiuser`: resync outcome returned; resync failure still returns 200 with
  the failure in `resync`; dry-run -> no resync; `resync=true` while a full sync is active -> 409 with **no edition
  created**; `StartSync` -> 409 while a resync is in flight and **unaffected when none is** (regression guard);
  `-race` for the exclusivity tests.
- **Slice 3** — `web/app.test.js`: button only for eligible `needs_review` records and hidden for viewers; result
  state replaces the button; modal HTML escapes ABS-supplied strings; checkbox hidden for dry-run; request sends
  `resync: true` only when checked.

## Docs (each slice documents only what it delivers)

- **Slice 1:** `README.md` two endpoint rows (draft + create, no resync) and a short API note; `docs/openapi.yaml`
  for both operations; `CHANGELOG.md` `[Unreleased]` -> `### Added`, plus a `### Changed` line for the publisher
  export default.
- **Slice 2:** the opt-in `resync` field/response block in README and OpenAPI; CHANGELOG entry.
- **Slice 3:** the user-facing "Add an edition from Sync Status" note (eligibility, preview/confirm, immediate
  read-status resync, dry-run behavior, the Known limitation); CHANGELOG entry.

## Verification (run at the tip of **each** slice before reporting it done)

1. `gofmt -l .` clean; `go build ./...`; `go vet ./...`; `go build ./cmd/edition-tool`.
2. Focused `go test` for the touched packages (Slice 1: `./internal/mismatch/... ./internal/edition/...
   ./internal/api/... ./internal/multiuser/... ./internal/server/...`; Slice 2 adds `./internal/sync/...` and
   `-race`).
3. `make test` (race + coverage) and `make lint` (golangci-lint).
4. `node --test web/app.test.js` (all slices; Slice 3 adds new cases).
5. Exercise the slice's real surface: Slice 1/2 by curl or the httptest fixture against stub ABS/Hardcover
   servers; Slice 3 in the browser pane (button only on eligible `needs_review` records, modal shows the draft,
   edits POST, resync result renders, error/dry-run paths, modal survives a status poll). Confirm a normal
   full sync and Sync Status still behave as before. A live create against real Hardcover is not part of
   automated verification — I will state that explicitly in each handoff.
6. Commit locally on the slice's branch (`feature/edition-from-needs-review-api` first); report that nothing
   was pushed.
