# Plan: Import Audible ASIN Audiobooks Without a Hardcover Book ID

**Status:** Split and published as three stacked PRs. Local validation passed; merge and CI status are tracked in the PRs.

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

Three PRs to `develop`, created only when requested, stacked and merged in
order. Step 1 establishes the shared no-book-ID import path; Step 2 enables it
for sync and the web API; Step 3 adds the UI.

| Step | Branch | Base | Scope | Depends on | PR |
|---|---|---|---|---|---|
| 1 | `audible_asin_import_step_1` | `origin/develop` | CLI Audible import and shared support: Audnexus comparison/confirmation, unanchored regional import, verification/recovery, association reuse, additive source-identifier outcome snapshots. | Edition plan Step 11 | [#52](https://github.com/Snuffy2/audiobookshelf-hardcover-sync/pull/52) |
| 2 | `audible_asin_import_step_2` | `audible_asin_import_step_1` | Sync classification and exports, API draft/create/revalidation/recovery, and OpenAPI. | Step 1, edition plan Step 11 | [#53](https://github.com/Snuffy2/audiobookshelf-hardcover-sync/pull/53) |
| 3 | `audible_asin_import_step_3` | `audible_asin_import_step_2` | Sync Status UI for Audible imports. | Step 2, edition plan Step 10 | [#56](https://github.com/Snuffy2/audiobookshelf-hardcover-sync/pull/56) |

Stack: `origin/develop -> audible_asin_import_step_1 -> audible_asin_import_step_2 -> audible_asin_import_step_3`.
Merge each phase in order. The later branches include the preceding steps.

No live-API gate remains: `upsert_book` without `book_id` has been confirmed
against Hardcover for this flow (see
[Resolved decisions and evidence](#resolved-decisions-and-evidence)).

### Step 1: CLI and shared import support

Step 1 adds the CLI path and the reusable support needed by later steps. Keep
existing sync matching and web API behavior unchanged; new outcome fields are
additive. No sync matching behavior changes in this step. The existing
association matcher is provenance-agnostic and already reuses saved
associations while their source identifiers and reading format match; Step 1
adds coverage for the new provenance.

**Audnexus comparison and confirmation.** For an audiobook with a usable
source ASIN and no `book_id`, the CLI discovers or accepts the regional
identifier and shows the Audnexus record: title, subtitle, authors, narrators,
series and position, publisher, release date, runtime, language, and cover
URL. When an ABS item ID is supplied, show its metadata beside the Audnexus
record and compare fields using the existing name and title normalization,
excluding cover because the ABS value is a local path and the Audnexus value
is an image URL. Differences are informational, not an automatic rejection.
Unknown or temporarily unavailable region discovery cannot be confirmed;
allow retry or an explicit supported regional identifier. Preview makes no
Hardcover request. Require interactive confirmation or an explicit
`--confirm-audnexus` acknowledgement before an import. Keep
`--confirm-identifier-correction` for its existing purpose.

Use the CLI's existing configuration path and names: global `--config`,
`audiobookshelf.audnexus_region` (environment
`AUDIOBOOKSHELF_AUDNEXUS_REGION`) as the preferred region, and input
`asin_region` or its existing `region` alias when explicitly supplied.
Preserve current default-region behavior and reject unsupported regions.

**Shared regional importer.** Add an explicit unanchored mode to
`RegionalAudiobookInput` (for example, `Unanchored`), which requires a zero
`BookID`; do not make zero mean "no book" in the existing anchored mode.
Unanchored mode sends `upsert_book` without `book_id`. Preserve the
Hardcover client's mutation safety boundary, rate limiting, retry behavior,
polling bounds, timeout budget, dry-run refusal, and `failed`, `loaded`,
and `created` handling. Verify the returned IDs and regional identifier,
then read the edition back uncached and confirm that it belongs to the
returned book and has audiobook reading format. There is no expected book ID
to compare in this mode.

Treat inconsistent IDs as an identity conflict: save no association and
explain that Hardcover may already have changed. A non-audiobook read-back is
also the established wrong-format outcome: return HTTP 409 with the edition
link and format guidance; save nothing. Preserve the existing response that
retrying resolves the same edition and asks the operator to report its format
on Hardcover. Do not retry the remote mutation automatically after an
ambiguous timeout. Tell the operator to verify the Hardcover result before
retrying, since retrying may create another edition. Preserve the existing
distinct outcome when Hardcover succeeds but the local association save fails.

**Association state and recognition.** Save the confirmed regional external
ID, resolved Hardcover book and edition IDs, normalized ABS source
identifiers, reading format, correction, and `audible_import_unanchored`
provenance when an ABS item ID is supplied. Persist any additional
Audnexus-confirmation audit data additively and keep existing state files
readable. Save under the existing state-file lock; a lock failure happens
before a Hardcover mutation. Dry run performs no mutation and saves no
association. Rely on the existing provenance-agnostic association matcher
to reuse this association while the item's source identifiers and reading
format still match. Do not change the existing fallback matching behavior or
add a new sync outcome in this step.

Add normalized source ASIN and separate ISBN-10 and ISBN-13 values to the
in-memory outcome snapshot additively. Keep current ASIN, ISBN, and display
Format JSON fields and meanings intact. These fields support source-identity
checks; they do not activate the new sync classification.

**CLI.** `edition create --input FILE` accepts audiobook input with a usable
ASIN and no `book_id`; ebook input and audiobook input that supplies
`book_id` keep their current validation and anchored behavior. Use the
confirmed or discovered region for the unanchored import. If `abs_item_id`
is supplied in the input or through `--abs-item-id`, refetch and verify the
ABS item, then save the association under `--state-file` or the configured
`sync.state_file`. Without an ABS item ID, do not save an association. The
existing mutation budget remains in force. Update the CLI README and user
documentation. This step does not change mismatch exports.

### Step 2: sync and API integration

**Sync classification.** After edition plan Step 11, retain the existing
association-first and exact regional `book_mappings` behavior. Once existing
identifier matching has not resolved an audiobook with a usable ASIN, do not
fall through to title/author candidate search. Record `needs_review` with
reason `audible_import_available`, no Hardcover book or edition ID, and the
source snapshot. This replaces the title/author review and `not_found`
outcomes for the affected items. Keep audiobooks without a usable ASIN and
ebooks on their existing matching paths. No extra state migration is needed
for the edition plan's Step 11 checkpoint handling.

**Draft and create API.** Extend the existing edition draft for this outcome
with the Audnexus record and per-field ABS comparison described in Step 1.
Allow a corrected ASIN and region to be previewed. Keep the existing
source_metadata_preview for ABS values and hardcover_title behavior. An
unknown or temporarily unavailable Audnex result remains a successful draft
response with its region status and warning; the caller can retry or supply a
regional identifier. The draft remains read-only with respect to Hardcover.

Extend `POST /api/profiles/{id}/edition-drafts/create` for
`audible_import_available` records:

- Require `audnexus_confirmed: true` and the regional identifier the user
  confirms. Missing confirmation or identifier is 400; a malformed identifier
  (not `ASIN:region` with a valid ASIN and supported region) is 422, before
  mutation. Reject `audnexus_confirmed` for other create paths with 400.
  The service has no stored record of a prior preview, so it imports the
  submitted identifier after re-reading it; that identifier is saved as the
  association's correction.
- Re-fetch the ABS item and compare its source identifiers and reading format
  with the completed run snapshot. A changed source is 409.
- Re-read Audnexus for exactly the confirmed region. A miss is 409 because
  the confirmed record no longer resolves; a rate limit or transient failure
  is retryable. Do not repeat region discovery after confirmation.
- Import through the Step 1 unanchored resolver. Do not use a run-record,
  candidate, or client-supplied Hardcover book ID. Return the verified book
  and edition IDs and book title for display; these are informational, not a
  review gate.
- Save the association under the existing state-file lock, with
  `audible_import_unanchored` provenance, the regional external ID, source
  snapshot, and confirmation audit data. The next sync reuses it like any
  other valid association.

Keep existing profile authorization and run checks, active-sync guard, dry-run
refusal, mutation reserve, retry/timeout behavior, and the distinct
"remote success, local save failed" outcome. A failed import saves nothing.
For an uncertain remote result, check import status, regional mapping, and
fresh edition read-back without resending the mutation. If the result remains
ambiguous after timeout or read-back failure, return the existing recovery
guidance to verify Hardcover before retrying. Map a wrong-format read-back to
the established HTTP 409 response with its edition link and format guidance.

**Import recovery.** Extend `check-import` to resolve the book and edition
from the existing regional status/mapping recovery and verify them with a fresh
read. Save a confirmed association without resubmitting creation.

**Exports and API contract.** Include `audible_import_available` and its
source snapshot in the mismatch/outcome exports; omit `book_id` where the
format permits omission so the ASIN-only CLI input can consume the result.
Update the API schema/OpenAPI and the field crosswalk for the new draft,
create confirmation, source snapshot, and result fields. Keep existing
fields and meanings compatible. Document that non-transient Audnexus draft
errors now return HTTP 200 with `region_status: unknown` and a non-retryable
warning instead of 502. Drafts add `source_metadata_preview`; create/check
results add the informational `hardcover_title`.

### Step 3: UI

Extend the Sync Status add-edition flow for `audible_import_available`
items:

- Use the same "Add Edition" button as other add-edition items, so the row
  looks the same.
- Adapt the modal only as needed to show ABS and Audnexus values side by side
  with differences highlighted and no Hardcover candidate.
- Do not add a separate confirmation checkbox: submitting the modal is the
  user's confirmation, and the UI sends `audnexus_confirmed` with the
  regional identifier shown.
- Discover the region automatically and offer **Refresh preview** to retry.
  Preserve the exact submitted identifier when restoring a request for retry.
- Disable the action while the region is unknown or temporarily unavailable,
  honoring `Retry-After`.
- After success, show the returned Hardcover book and edition.

Capability, dry-run, sync-in-progress, and optional resync behavior follow the
existing UI. Escape all ABS- and Audnexus-provided strings.

## Acceptance

- Step 1: `edition create` imports an ASIN-only audiobook without
  `book_id` only after Audnexus confirmation, verifies the returned
  audiobook edition, saves a source-checked association when an ABS item ID
  is supplied, and the next sync reuses that association.
- Step 1: anchored audiobook imports, ebooks, current sync matching and
  web API behavior remain as before; dry run performs no mutation or save.
- Step 2: an unmatched audiobook with a usable ASIN becomes
  `needs_review`/`audible_import_available` without title/author fallback or
  Hardcover IDs, and its exported record can drive the ASIN-only CLI path.
- Step 2: the API imports only after Audnexus confirmation, rechecks the exact
  ABS source snapshot and confirmed regional identifier, verifies the returned
  edition, and saves an association for `loaded` and `created`.
- Step 3: the UI confirms the displayed regional identifier, supports preview
  refresh, preserves restored identifiers for retry, and shows the verified result.
- All steps preserve dry-run's no-mutation/no-association behavior and expose
  recovery guidance when a remote result or local save is ambiguous.

## Step checklists

### Step 1 — CLI and shared support

- [ ] Add an Audnexus record and per-field ABS comparison for CLI preview;
  cover is shown but not compared. Support a corrected regional identifier
  preview and require explicit confirmation before import.
- [ ] Add the resolver's explicit unanchored mode with existing polling,
  rate limiting, retry, timeout, dry-run, status, and fresh-read verification;
  keep anchored zero-ID validation.
- [ ] Save source-checked associations with regional ID and provenance under
  the state-file lock; verify the existing generic matcher reuses this provenance.
  Keep old state files readable without a required migration.
- [ ] Add normalized source ASIN and separate ISBN-10/ISBN-13 values
  additively to outcome snapshots. Preserve existing display Format and other
  JSON fields. Do not change sync classification or existing web API behavior.
- [ ] In `edition create`, accept ASIN-only audiobook input with no
  `book_id`, keep existing anchored and ebook paths, and support confirmation
  by prompt or `--confirm-audnexus`. Update README and
  `cmd/edition/README.md`.
- [ ] Cover preview comparison (match, differs, missing, unknown and
  unavailable region), explicit confirmation, `loaded` and `created`
  without `book_id`, non-audiobook result, identity conflict, failed import,
  timeout, local-save failure, next-sync reuse, anchored compatibility, and
  dry run at the client and command boundaries.

### Step 2 — sync and API

- [ ] Classify only unmatched usable-ASIN audiobooks as
  `needs_review`/`audible_import_available`, with no title/author fallback,
  Hardcover IDs, or removed existing identifier paths.
- [ ] Include the outcome in mismatch exports without a required `book_id`;
  preserve the added source snapshot fields.
- [ ] Extend the draft with Audnexus data/comparison and identifier
  correction/re-preview; make no Hardcover request.
- [ ] Extend API create validation, stale-source checks, confirmed-region
  Audnexus reread, unanchored import, association persistence, and recovery
  responses. Cover missing/wrong confirmation (400), malformed identifiers
  (422), stale/missing record (409), retryable transient failure, loaded and
  created, wrong format/identity conflict, timeout, failed import, local-save
  failure, dry run, and next-sync reuse. Assert no `book_id` is sent.
- [ ] Update OpenAPI and the field crosswalk; document the API and sync behavior
  in README and add one CHANGELOG bullet.

### Step 3 — UI

- [ ] Use the existing "Add Edition" button; adapt the modal only to show
  ABS/Audnexus comparison with highlighted differences and no Hardcover
  candidate. Submit sends `audnexus_confirmed` with the shown identifier.
- [ ] Support automatic region discovery and preview refresh; preserve a
  restored request's identifier, disable the action for unknown or unavailable
  regions, and honor `Retry-After`.
- [ ] Show the returned Hardcover book and edition after success; escape ABS
  and Audnexus strings.
- [ ] Cover submit payload, differences, preview refresh and restored identifiers, region
  unavailable, success, errors, and dry run at the web boundary. Update the
  user-facing README and add one CHANGELOG bullet.

### PR gates (each PR)

- [ ] Run `gofmt`, `make test`, `make lint`, `make build`, and
  `go build ./cmd/edition-tool` if touched; run
  `node --test web/app.test.js` when web code changes.
- [ ] Use the repository PR template.

### PR description guidance

Insert a `Multi-Step Project` section between the PR template's Summary of
Changes and Testing Instructions. Keep three numbered lines describing Step 1
(CLI and shared support), Step 2 (sync and API), and Step 3 (Sync Status UI).
Mark the delivered line `(this PR)` and strike through a step only after its PR
has merged. Link this full plan. This three-step block replaces the earlier
edition plan's project block for these PRs.

## Changelog wording

- **Step 1 — Added:** `**Import Audible ASIN audiobooks from the edition CLI**:
  The edition command accepts Audible ASIN audiobooks without a Hardcover
  book ID after Audnexus confirmation, verifies the imported edition, and can
  save the association for later syncs. By @Snuffy2.`
- **Step 2 — Changed:** `**Sync Audible ASIN audiobooks without a Hardcover
  match**: Unmatched audiobooks with a usable Audible ASIN skip title/author
  fallback and can be imported through the API after confirming the Audnexus
  record. By @Snuffy2.`
- **Step 3 — Added:** `**Add Edition for Audible ASIN audiobooks in Sync
  Status**: The Add Edition modal compares the audiobook with its Audible
  record before importing it. By @Snuffy2.`

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
