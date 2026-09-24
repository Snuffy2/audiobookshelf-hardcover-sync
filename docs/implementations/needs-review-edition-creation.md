# Plan: Resolve Audiobookshelf Identifiers and Add Hardcover Editions

**Status:** Revised after the Audible mapping investigation; implementation and
PR boundaries remain subject to review. Steps 1 and 2 are merged into `develop`.
Step 3 and later are not merged.

This is the current plan. The [legacy seven-step version](needs-review-edition-creation-legacy.md)
is retained for its completed-work record and earlier decisions; its unmerged
step instructions and audiobook ASIN assumptions are superseded here. The
[field crosswalk](needs-review-edition-field-crosswalk.md) still records useful
ABS metadata and ISBN transformations; its Audible R4 row, create pipeline,
and old step allocations must be revised before the affected PRs are ready.
The separate [Audible mapping findings](../hardcover-audible-mapping-findings.md)
are evidence, not implementation instructions.

## Outcome and boundaries

An ABS audiobook has a bare source ASIN, without a region. A known regional
Audible `book_mappings` row identifies the Hardcover book and edition. When
that row is absent, a region-qualified `upsert_book` can still return the
correct existing edition as `loaded`, but it need not save the missing mapping.
The application must retain that confirmed resolution locally so subsequent
syncs find the same edition after restarts. A successful resolution has no
routine expiry.

The user-approved identifier rules for the completed flow (after Step 6) are:

- Use a durable, profile-scoped association from the ABS item and its source
  identifier to the resolved Hardcover `book_id` and `edition_id`. Record the
  region-qualified identifier used for resolution and how the IDs were
  confirmed. Invalidate or re-evaluate the association when the ABS identifier
  changes, the target edition is definitively unavailable, or the user
  explicitly forgets the match. It is correctable, not a permanent assertion
  about mutable catalogues.
- Prefer an existing confirmed local association, then an exact Audible
  `book_mappings` match. Discover the source ASIN's region before a regional
  import; do not assume that an unqualified ASIN is US. Conflicting or
  unresolvable regions remain reviewable.
- For Audnex discovery, try the configured preferred region first (US when no
  preference is set), then sweep the remaining regions once in this fixed
  order: US, CA, UK, AU, DE, FR, ES, IN, IT, JP. These are the ten regions in
  Audnex's published `/books/{ASIN}` API schema; skip the preferred region
  when it appears in the list, so there are at most ten requests. Stop at the
  first response for the requested ASIN. Use that response's region for the
  regional identifier and its `releaseDate` for edition metadata; the preference is not an assertion that
  every ABS ASIN belongs to that marketplace. Do not reject an ASIN hit merely
  because Audnex title, author, narrator, or edition details differ from ABS.
  Keep Hardcover book/edition identity checks separate from this lookup.
- For a missing mapping and a credible candidate Hardcover book, submit the
  regional external ID through `upsert_book`. A validated `loaded` result is a
  successful resolution even if Hardcover never stores that alias. A validated
  `created` result can identify the new edition. Persist either result locally
  only after its book and edition IDs are confirmed.
- Do not call `insert_book_mapping`. Ordinary API credentials cannot be
  assumed to have `write:catalog:map`, even though ordinary web users may add
  Audible identifiers through Hardcover's webpage.
- Do not use `editions.asin` to match an Audible audiobook, choose a candidate,
  or block creation as a duplicate guard. An older edition that contains an
  Audible ASIN only in that field may coexist with a newly imported edition
  that has a proper Audible mapping. This accepted outcome does not change
  ISBN or genuine retail-ASIN behavior for other formats.
- Replace both the in-memory ASIN shortcut and its 24-hour persisted positive
  cache as sources of match identity. Do not migrate its entries wholesale:
  some came from the old `editions.asin`
  lookup and are not verified Audible mappings. A separate short-lived
  performance cache is not part of this plan.
- Provide a profile-scoped API action to forget one ABS item's local
  association and its incremental checkpoint. The next sync reruns the normal
  matching priority order; it may choose the same edition again if the
  Hardcover catalogue is unchanged, or a different edition if the previous
  target changed or disappeared or a higher-priority match is now available.
  This action does not delete a Hardcover edition or `book_mappings` row.
- If a sync operation suggests the associated edition was deleted, confirm
  that exact edition is absent with a fresh Hardcover read that bypasses the
  edition cache. Only a definitive edition-not-found result removes the local
  association and checkpoint; missing user-book/read records, permissions,
  timeouts, rate limits, and other transient errors do not. Leave that book
  retryable for the next sync while other books continue. A `book_mappings`
  row can legitimately be absent for a valid association, so its absence
  alone never triggers automatic invalidation.

Normal sync must continue for profiles without catalogue-write capability.
An `upsert_book` permission failure, timeout, ambiguous candidate, or unknown
region yields a reviewable unresolved item rather than a failed whole sync or
an unverified local association. Dry run may perform reads but neither
mutates Hardcover nor persists an association that would skip a later real
sync. No step may rely on a subsequent PR to make its newly shipped behavior
safe.

Distinguish an actual region miss from a transient Audnex failure. If a valid
local association, exact Hardcover mapping, or other independently verified
match has resolved the edition, an Audnex rate limit affects only optional
release-date enrichment; normal sync continues with the ABS date fallback.
Otherwise, when HTTP 429 or another transient Audnex error prevents required
region discovery, stop the regional sweep. Other independent matching reads
may still resolve the item; if none does, record that book as retryable
`failed` with a specific reason. Do not mark it `not_found` or `needs_review`,
import an unconfirmed regional ID, or persist state that would cause an
unchanged incremental sync to skip retrying it. Other books in the run
continue. A completed sweep with genuine misses is different: the unresolved
region remains `needs_review`.

Keep the existing Hardcover `Owned`-list ownership checks and Audiobookshelf
finished-state behavior throughout the matching and resync changes.

## Delivery sequence

Each step is one reviewable PR to `develop`. The sequence can be split further
if an actual diff becomes too broad; a split must still leave both resulting
PRs working independently. New branches and PRs are created or published only
when requested. Branch names below are provisional after Step 3 because the
existing Step 4 and Step 5 branches implement the old ordering.

| Step | Scope and standalone result | Depends on | Current state |
|---|---|---|---|
| 1 | ISBN and export foundations | `develop` | Merged |
| 2 | Edition creator hardening and ebook support | 1 | Merged |
| 3 | Rework the read-only ABS/Audnex edition draft. Its audiobook ASIN is a source Audible identifier, never an instruction to write `edition.asin`. Discover its region with preferred-first, bounded Audnex lookup and take `releaseDate` from the region that resolves it. Show a region only when established and distinguish unknown/ambiguous region. Audiobook metadata is preview-only; the submitted Audible identifier may be corrected. The endpoint still makes no Hardcover request and works by itself. | 2 | Existing draft branch and PR need revision; full rework is allowed |
| 4 | Land the existing ISBN-10/ISBN-13 counterpart matching as its own PR. Keep its diff confined to ISBN behavior and format protection. | 2 | Existing `step_5_needs_review_add_edition` work is reusable after restacking |
| 5 | Add the durable local association, a profile-scoped forget-match API, read-first lookup, shared preferred-first Audnex region discovery, and exact Audible `book_mappings` query. Invalidate on a freshly confirmed deleted edition and clear the item's incremental checkpoint. Replace the positive 24-hour cache; persist only verified matches. A discovery-blocking 429 is retryable per-book `failed`, not a catalogue miss. Keep the current legacy fallback for genuine misses during this additive step, without saving its result as a verified association. | 3, 4 | New work |
| 6 | Add the bounded `upsert_book` fallback for a missing regional mapping, validate `loaded`/`created` IDs, and persist the resolution. Remove audiobook `editions.asin` matching and its duplicate guard in this same PR. A failure or missing write scope remains `needs_review`; normal sync stays usable. | 5 | New work |
| 7 | Provide create eligibility and capability reporting for `upsert_book` imports by ISBN or regional Audible identifier. Keep the route independently useful without exposing a create action that is not implemented. | 3, 6 | Extract from the old Step 4 branch and revise |
| 8 | Add the create POST using `upsert_book` for ISBN/ebook and regional Audible imports. Validate the returned book and reading format, save a local association before reporting success, and work through the API without resync or UI. | 3, 6, 7 | Rebuild from the old Step 4 branch |
| 9 | Add opt-in single-book read-status resync after creation, sharing the normal sync path and excluding overlapping full syncs. | 8 | Not started |
| 10 | Add the Sync Status preview, confirmation, capability, optional resync, and forget-match UI for already matched items. | 9 | Not started |

### Step 3: source draft

The draft fetches the expanded ABS item and may make bounded Audnex reads. It
keeps the original bare ABS ASIN separate from an optional confirmed
marketplace and from any user-submitted correction. It does not use Hardcover
to decide whether an edition exists. An unknown or ambiguous region is visible
to the caller; it is not silently changed to US. The region discovery logic
must be reusable by the Step 5 and Step 6 sync paths rather than reimplemented
inside the draft.

The configured Audnex region is the first lookup preference, defaulting to US
when unset. If the requested ASIN is absent there, probe the remaining regions
once in this order: US, CA, UK, AU, DE, FR, ES, IN, IT, JP. Skip the preferred
region in that list, cap the sweep at ten requests, and stop at the first
response for that ASIN. Normalize a configured preference to lowercase; an
unsupported value warns and uses US as the lookup preference. Do not make
title/author/narrator/ISBN/runtime similarity a hard gate for region discovery; these are not the Hardcover identity checks. Use
`releaseDate` from the successful response, regardless of whether it came
from the preferred or a fallback region. If that response has no release date,
fall back to the existing ABS published date/year precedence. If no supported
region resolves the ASIN, keep the region unknown and use the same ABS date
fallback; do not automatically import a regional Audible identifier. A
transport error or rate limit is not evidence that the ASIN is absent; report
the lookup as temporarily unavailable rather than turning that failure into
a guessed region. In the read-only draft, surface this as a retryable warning,
not as a sync outcome; use the ABS date fallback for the preview. Align
config validation, environment values, and documentation with this ten-region Audnex list before enabling the sweep; a region seen only in
Hardcover mappings is not thereby a supported Audnex lookup region.

The existing draft PR is not protected from redesign. Reuse its ABS decoding,
metadata preparation, eligibility, and tests where they still fit. Remove or
change fields and assertions that present an audiobook ASIN as a future
`BookDtoInput.asin`. For audiobooks, offer a correction to the submitted
regional Audible identifier (ASIN and region), which Step 8 passes to
`upsert_book`; show title, subtitle, date, edition information, ISBNs, and
other imported metadata as read-only preview. Do not accept audiobook metadata
edits that the import cannot save. For ebooks, offer only a correction to the
ISBN submitted to `upsert_book`; all edition metadata is read-only preview. An
ebook without a usable ISBN stays reviewable because this create path has no
import identifier. Existing reading-format, date, author/narrator,
language-warning, and dry-run behavior remain within the draft's read-only
scope.

Acceptance: the draft works with a bare ABS ASIN, reports a discovered region
when one is supported by evidence, reports uncertainty otherwise, and issues
zero Hardcover requests. Its release date comes from the Audnex response for
the discovered region, with ABS date fallback when that response has no date
or no region resolves. Its API description distinguishes source identifiers
from Hardcover edition fields.

### Step 4: ISBN counterpart matching

The previous Step 5 implementation can supply this PR, but it must be rebased
onto current `develop` and narrowed to ISBN normalization, counterpart search,
and reading-format behavior. It must not reintroduce an ASIN fallback or
silently modify the later Audible resolver. The PR is useful immediately for
ISBN-only and ebook items; no later step is needed for those matches to work.

### Step 5: durable association and read-only Audible matching

Extend the existing versioned `sync.state_file` JSON store, rather than the
cache directory or the web-only database. The one-time CLI already uses this
file; the web service derives a separate state path per profile, and deployment
examples mount it in persistent `/data` or `/app/data`. Keep the association
and incremental checkpoint in the same state-file transaction. The record
needs the ABS item identity, the ABS source ASIN, the resolved regional
external ID, the Hardcover book and edition IDs, and resolution provenance;
the profile is identified by the state-file path. Bump the schema version and
read existing v3 checkpoint files without losing their books. Serialize
read-modify-write operations with a per-file lock, reload under that lock,
reject stale overwrites, and retain the existing atomic replace/directory-sync
durability. Coordinate forget-match with in-flight profile sync so a later
stale save cannot recreate the forgotten association. A changed source
identifier cannot reuse a stale association. Add an authenticated,
profile-authorized API action to forget one ABS item's association, clear its
incremental checkpoint, and make the next sync perform ordinary matching
again. Serialize this with any in-flight sync for that item/profile so a
concurrent save cannot restore the forgotten record. Return the previous
resolution and whether an association was removed; an absent association is a
safe no-op. The action changes no Hardcover catalogue or library record.
Dry run does not delete the association or checkpoint; the API reports that
no persistent change was made.
It does not promise a different result: unchanged Hardcover identifiers may
resolve to the same edition; a changed, removed, or newly higher-priority
candidate may resolve differently. Writes must be safe across restarts and
concurrent syncs; failures must not masquerade as a saved match.

An unchanged item may be skipped by incremental sync before any Hardcover
read, so this plan does not promise continuous deletion detection. When an
item is processed and a Hardcover operation indicates the mapped edition may
be gone, bypass the existing `GetEdition` cache and check that edition ID
directly. Only a definitive absent edition invalidates the association and
checkpoint. Do not infer edition deletion from a missing user book, missing
read, GraphQL transport failure, permission error, or absent regional
`book_mappings` row. Record the current item as retryable `failed` without
applying progress to another edition; let the next sync rematch. Clearing an
association must not erase historical Hardcover reads or ownership.

On a local miss, probe exact regional Audible mappings with supported
marketplace identifiers and confirm that any multiple hits agree on the same
book and edition and have the expected audiobook format. Audnex may establish
a region for a bare ASIN when the mapping is absent, using Step 3's
preferred-first, bounded, first-ASIN-hit lookup. Do not impose a strict
Audnex-to-ABS metadata comparison on that lookup. An absence or conflict with
independent Hardcover evidence is not proof that the book is absent. Continue
to ISBN and title/author candidate discovery as appropriate.

If a 429 or other transient error interrupts discovery, do not continue
probing regions. Complete any independent matching reads; if they cannot
verify an edition, the per-book outcome is retryable `failed`, not
`needs_review` or `not_found`. An already verified match is unaffected by an
optional Audnex enrichment failure.

During this step only, preserve the existing `edition.asin` fallback on an
unresolved audiobook so behavior does not regress before Step 6. Never write
that fallback into the durable association. Retire the old 24-hour positive
ASIN cache without importing it into the new store.

Acceptance: exact mapping and already-confirmed local association produce the
correct IDs after a restart; old unverified cache entries do not; an
unresolved regional alias remains reviewable; the forget API forces the next
sync to re-evaluate; a freshly confirmed deleted edition invalidates only its
local association; dry run writes no association.

### Step 6: missing-mapping resolution and the matching switchover

Only a credible, unambiguous candidate book may be supplied to `upsert_book`.
The existing title/author result is a candidate, not by itself an edition
match. The no-`book_id` form has been tested only with identifiers already present
in Hardcover, including one that resolved to a different book; supply the
confirmed candidate book ID when resolving a missing regional mapping. Bound asynchronous polling, preserve
the client's rate limiting and retry behavior, and distinguish `failed`,
`loaded`, and `created`. Confirm returned book and edition IDs, the expected
candidate book, and audiobook reading format before saving the association.
If the candidate or returned identity conflicts, leave the item for review.

Switch audiobook matching away from `edition.asin` in this PR. Do not query it
as a fallback or duplicate guard. A title already represented only by a
legacy `edition.asin` may receive another edition with a correct Audible
mapping; that is accepted. A missing permission, external lookup failure, or
poll timeout must not leave a partial local success record or make the whole
sync fail. Existing library progress and ownership rules continue to use the
normal sync path.

Acceptance: when Hardcover returns an existing edition as `loaded` but keeps
the regional alias absent, the next sync uses the durable association and
finds that edition without another import. A `created` edition is also
recorded. A normal token without import permission retains ordinary sync and
produces an actionable review outcome for the unresolved item.

### Steps 7–8: capability and create API

The current create branch assumes `insert_edition` plus a duplicate search in
`edition.asin` can handle an Audible ASIN. That behavior cannot merge
unchanged. Split its capability route, authorization, request validation,
in-flight guard, and tests into reviewable Step 7 and Step 8 diffs. The new
application create endpoint uses `upsert_book` exclusively: platform 32 with a
regional Audible identifier for audiobooks, or virtual platform 8 with an ISBN
for ebooks. It supplies no title, author, narrator, or other edition metadata.
Existing Step 2 `edition.Creator` and its CLI callers are not automatically
changed by this plan. Keep book and reading-format checks for both formats.

The live test with an ordinary full API token successfully called `upsert_book`
for a known book and regional Audible ASIN; import status became `created`
and a subsequent read found the new edition and mapping. The same token's
`update_edition` calls returned an ID and no errors for both a populated
title and an empty subtitle, but fresh reads retained the original title and
null subtitle. A success response is therefore not proof that submitted
metadata persisted. Step 3 and Step 8 must not promise edition
metadata edits. The supported corrections are the regional Audible identifier for an audiobook and ISBN for
an ebook; the returned edition metadata is preview-only. Hardcover's published
capability map permits `upsert_book` and `update_edition` under
`write:catalog:append` (or the broader `write:catalog:edit`/`write:catalog`
scopes); `write:catalog:map` is separate. Runtime authorization and read-back
remain the source of truth for a given token.

Hardcover staff also describe an ISBN import variant of `upsert_book` with
`platform_id: 8` and the ISBN as `external_id`. Platform 8 is not returned
by the live `platforms` catalogue, so treat it as an undocumented import
source rather than a discoverable mapping platform. A live call with an
existing Outland ISBN and the known book ID returned the existing physical
edition immediately; `book_import_statuses` still reported `not_found` for
that platform and ISBN. This confirms ISBN reuse, not creation of a new
edition or reliable status polling for that source. A second live request
omitted `book_id` and returned Outland's existing ebook edition for ISBN
`9781507000885` (format 4); the same
no-`book_id` form returned its existing audiobook for `B07NHP9F58:uk`
(format 2). Omitting `book_id` is valid for these existing identifiers, but
these calls do not establish where an absent identifier will be imported.
A requested no-`book_id` import of ISBN `9781680681420` returned existing
physical edition `33271167` (format 1) on a different Outland book
(`2933828`), not a new ebook on the intended book (`461193`). A fresh read
confirmed no ebook with that ISBN was added. Another ISBN,
`9781680680065`, had no direct edition match before import. Its no-`book_id`
upsert returned null IDs, then import status `loaded` resolved to **Flybot**
book `1931466`, audiobook edition `32083414` (format 2); no ebook was added.
Step 8 supplies the confirmed run-record book ID for an unresolved ISBN or
ASIN so an import is directed to the intended book, then verifies the returned
book and format. The mutation cannot request ebook format: if the ISBN resolves
to a physical edition or another book, leave it reviewable rather than report
a successful ebook create. Do not treat an immediate ID or a `not_found` ISBN
import status alone as proof of a suitable edition.

Create refetches the ABS item and uses the run record's Hardcover book ID
when importing an unresolved identifier, never a client-supplied book ID. It
handles a changed submitted Audible ASIN as an explicit user correction: retain both the original ABS source value and
the regional identifier submitted for import in the local association. A
create-time lookup of that submitted ASIN uses the same preferred-first
discovery rule; when release date is server-derived, its baseline comes from
the response for the region that actually resolves that ASIN, not a separate
lookup forced to the configured region. Audiobook date is not an editable
request field. A create response must distinguish an edition already
created remotely from a failure to save the local association, so retry cannot quietly create an
unrelated second edition. Dry run makes no Hardcover mutation and saves no
association. Step 8's POST must be usable with curl and findable by the next
normal sync before Step 9 exists.

### Step 9: optional immediate resync

Add `Service.SyncBook` through the existing per-book processing path. An
opt-in create-with-resync cannot overlap a full sync for the same profile; an
ordinary create without resync remains available. A resync failure is
reported separately because the edition may already exist. Dry run attempts
no resync and persists no progress or association. Test success, no-op,
failure, cancellation, and concurrent full-sync paths with the race detector.

### Step 10: UI

Show the create action only when the profile, run record, and specific create
capability permit it. Preview the ABS source identifier and any established
region without implying that `edition.asin` is the Audible destination. Show
only edits that Step 8 can honor, escape ABS-provided strings, and display
uncertain region or unresolved candidate results as review states. Confirmation
uses the create POST; the resync checkbox is opt-in. Dry run is clearly
identified and does not offer a real resync.

Also expose the Step 5 forget-match action for already matched items, not only
`needs_review` items. Show the current Hardcover book/edition target and ask
for confirmation. Explain that the next sync reruns normal matching and may
select the same edition if Hardcover has not changed. Do not describe this as
deleting a Hardcover edition or mapping, or as forcing a different target.
Disable the real forget action in dry run.

## PR and branch handling

- Steps 1 and 2 are merged; do not reopen their PRs for this change.
- The existing Step 3 [fork PR #28](https://github.com/Snuffy2/audiobookshelf-hardcover-sync/pull/28)
  and [upstream PR #198](https://github.com/drallgood/audiobookshelf-hardcover-sync/pull/198)
  describe the old draft contract. Rework their code and descriptions before
  they are considered ready. If the
  revised Step 3 cannot be reviewed cleanly as an amendment, supersede that
  PR with a focused replacement only on the owner's explicit command.
- The old Step 5 branch is source material for new Step 4. Rebase and audit its
  diff against current `develop` rather than merging the old stacked branch.
- Do not merge the old Step 4 create branch as-is. Restack and split its useful
  code after the matching work, preserving the create endpoint's authorization,
  dry-run, timeout, shutdown, and error-handling safeguards.
- Each PR targets `develop`, has one release-facing CHANGELOG bullet, updates
  its affected README/OpenAPI/crosswalk contract, and passes the affected Go
  tests, `make test`, `make lint`, relevant builds, and web tests if touched.
  The PR description uses the repository template. Do not push or open a PR
  merely because this plan names one.

## PR description block

Every fork and upstream PR uses the repository's
[pull request template](../../.github/pull_request_template.md). Put the
following `Multi-Step Project` block after **Summary of Changes** and before
**Testing Instructions**. Copy it into the PR for that step, move `(this PR)`
to the delivered line, and keep merged steps struck through without changing
their historical wording. Keep each line to one or two sentences. Answer every
template checklist item exactly as written; put qualifications in Testing
Instructions. Do not add issue links to fork PRs or upstream PRs targeting
non-default `develop`.

```markdown
## Multi-Step Project

1. ~~ISBN and export foundations: Add shared ISBN normalization and reading-format helpers, and fix the mismatch export (hyphenated ISBNs are kept, no default publisher, ebook items export as ebook editions, an abridged audiobook exports as Abridged).~~

2. ~~Edition creator hardening: Make the edition creator reuse an existing edition of the same book by ASIN or ISBN and refuse another book's, honor the requested edition format, send the Audiobookshelf token only to its own server, keep the cover upload code but switched off, and create ebook editions.~~

3. Edition draft endpoint (this PR): Preview ABS audiobook and ebook metadata without a Hardcover call. Treat an audiobook ASIN as a source Audible identifier, discover its region with bounded Audnex lookup, and use the matched region's release date.

4. ISBN counterpart matching: Find ISBN-10 and ISBN-13 counterparts during sync while preserving reading-format checks.

5. Durable Audible resolution: Store confirmed ABS-to-Hardcover associations, offer a forget-match API, recover from confirmed edition deletion, and match exact regional Audible identifiers with bounded discovery.

6. Missing regional mapping: Resolve a known candidate with `upsert_book`, retain validated `loaded` or `created` IDs locally, and stop matching audiobooks by `editions.asin`.

7. Create capability: Report whether the profile can perform `upsert_book` imports before offering the create action.

8. Edition create endpoint: Import by ISBN or regional Audible identifier with `upsert_book`, validate book and format, and save a confirmed local association.

9. Immediate read-status resync: Optionally sync the created edition's one ABS item's read status without overlapping a full sync.

10. Sync Status UI: Preview and confirm edition creation for eligible needs-review items, offer optional resync, and let users forget a stored match for a future retry.

[Full Plan Document](https://github.com/Snuffy2/audiobookshelf-hardcover-sync/blob/docs/needs-review-edition-plan/docs/implementations/needs-review-edition-creation.md)
```

Update this block here first when a step's actual contract changes, then
update any open PR descriptions. When an upstream PR merges, strike its line
through and update the delivery table and checklist in the same plan change.
The example marks Step 3 as the current PR; no Step 4–10 PR is implied to
exist.

## Changelog by step

Each implementation PR adds exactly one bullet under the fitting
`[Unreleased]` section of `CHANGELOG.md`. Fold related user-visible changes
into that bullet. Preserve earlier steps' bullets; add the upstream PR number
only after that PR exists. Steps 1 and 2 are merged and their entries are
already recorded; do not rewrite them for this plan revision. The following
wording is a starting point to update against the final diff and live API
evidence:

- **Step 3 — Added:** `**Edition draft for needs-review items**: Preview ABS
  audiobook and ebook metadata without a Hardcover request, distinguishing a
  source Audible ASIN from an edition field, discovering its region with
  bounded Audnex lookup, and using that region's release date when available.
  By @Snuffy2 (#198)` if the existing upstream PR continues. Its
  current bullet claims a later step must put Audible identifiers directly in
  `book_mappings`; replace that claim. If the PR is superseded, use the new
  upstream number when it exists.
- **Step 4 — Fixed:** `**ISBN counterpart matching**: Match an ABS ISBN-10 to
  its valid ISBN-13 counterpart, and the reverse, without changing
  reading-format separation. By @Snuffy2`.
- **Step 5 — Added:** `**Durable Audible matches**: Persist verified ABS item
  to Hardcover book and edition resolutions, offer a per-item forget-match
  API, recover from confirmed edition deletion, and search exact regional
  Audible mappings while retiring the expiring ASIN cache. By @Snuffy2`.
- **Step 6 — Changed:** `**Audible identifier resolution**: Resolve a missing
  regional mapping through Hardcover import when permitted, retain confirmed
  results locally, and stop treating edition ASINs as Audible matches; items
  without a verified resolution remain reviewable. By @Snuffy2`.
- **Step 7 — Added:** `**Edition creation capability**: Report the profile's
  `upsert_book` import capability without promising permission from an
  unrelated scope check. By @Snuffy2`.
- **Step 8 — Added:** `**Create editions from needs-review items (API)**:
  Create or reuse an edition after confirmation through ISBN or regional
  Audible `upsert_book`, validate book and format, and retain its confirmed
  local resolution; dry runs make no external change. By @Snuffy2`.
- **Step 9 — Added:** `**Immediate one-book resync**: Optionally sync read
  status after edition creation while excluding an overlapping full sync. By
  @Snuffy2`.
- **Step 10 — Added:** `**Edition creation in Sync Status**: Preview and
  confirm an eligible needs-review edition in the UI, show capability and
  region uncertainty, optionally resync its read status, and forget a stored
  match for future rematching. By @Snuffy2`.

Step 3 owns the draft README/OpenAPI description; Steps 4–6 document matching
and any new persistence or permission behavior; Step 7 documents its
capability route; Step 8 documents the create route and its exact
identifier corrections; Step 9 documents the `resync` request/response; Step 10 documents the
user flow. Update the field crosswalk as the relevant step lands, rather than
leaving its old R4 destination as an implementation contract.

## Step checklists

These are the follow-up lists for the revised sequence. A checked item means
the work or verified pre-existing part is complete, not merely planned.
Before marking any implementation step done, run its focused behavioral tests,
`gofmt`, relevant builds and `go vet`, `make test`, and `make lint`; run
`node --test web/app.test.js` when web code changes. Update README, OpenAPI,
crosswalk, and the one changelog bullet where that step affects them. Review
the branch diff against its base so an old stacked branch does not carry
another step's work. Follow the PR template and block above, check CI, and
address valid review feedback. A live API test is recorded as live evidence,
not substituted for automated interface tests. PR creation and pushes still
require the owner's instruction.

### Step 1 — ISBN and export foundations (merged)

- [x] Normalize and classify source ISBNs, retain invalid-checksum values with
  validity flags, and derive a counterpart only when valid and possible.
- [x] Correct publisher and ebook mismatch exports and abridged audiobook
  information; preserve the other audiobook export behavior.
- [x] Merge upstream PR #195 into `develop`; retain its existing CHANGELOG
  bullet and completed review record in the legacy plan.

### Step 2 — edition creator hardening (merged)

- [x] Harden same-book reuse and cross-book refusal, edition format handling,
  ebook creation, token scoping, redirects, and the disabled cover path.
- [x] Merge upstream PR #197 into `develop`; retain its existing CHANGELOG
  bullet and completed review record in the legacy plan.

### Step 3 — source draft (existing PR needs revision)

- [x] The existing branch has an ABS/Audnex draft endpoint with no Hardcover
  call; its useful decoding and local metadata tests can be retained.
- [x] Establish ordinary-token Audible import/update behavior: import
  succeeded, but attempted title and subtitle edits did not persist. Limit
  audiobook correction to the submitted regional identifier.
- [ ] Rework the draft schema and crosswalk R4 so it preserves the bare ABS
  source ASIN, optional established region, and uncertainty without implying
  an `edition.asin` write or silently choosing US.
- [ ] Try the configured Audnex region first (US when unset); only on an ASIN
  miss, sweep the other verified supported regions in a fixed bounded order
  and stop at the first response for that ASIN. Keep this lookup reusable by
  sync; do not require close title/author/narrator/edition metadata agreement.
- [ ] Populate the draft release date from that successful region's Audnex
  `releaseDate`, falling back to the ABS published date/year if it is absent
  or no region resolves. Test preferred hit, fallback-region hit, no result,
  transient error (including 429 and a retryable draft warning), and malformed
  or mismatched returned ASIN.
- [ ] Expand six-region config validation to the verified ten-region Audnex
  lookup set and normalize the preferred region; do not include a
  Hardcover-only mapping region without Audnex support.
- [ ] Verify audiobook/ebook metadata, ISBN flags, author/narrator names,
  language and date warnings, eligibility, cancellation, and zero Hardcover
  requests at the HTTP boundary.
- [ ] Correct README, OpenAPI, the single CHANGELOG bullet, and the fork and
  upstream PR descriptions; rerun the shared validation and PR gates.

### Step 4 — ISBN counterpart matching

- [x] A prior Step 5 branch contains an ISBN matching implementation to audit
  and restack; that branch is not merged.
- [ ] Rebase onto current `develop` and keep this PR's diff to ISBN
  normalization, counterpart searches, and same-format behavior.
- [ ] Test ISBN-10 only, ISBN-13 only, valid counterpart, 979/no counterpart,
  invalid checksum, normalized separators, audiobook, and ebook at the sync
  and GraphQL boundaries.
- [ ] Update the matching documentation and one CHANGELOG bullet, complete
  the shared validation, and publish only on instruction.

### Step 5 — durable association and exact Audible mapping

- [x] Choose the existing versioned sync state file for CLI and per-profile
  web persistence; require a version bump, coordinated atomic association
  and checkpoint updates, and safe concurrent writes in Step 5.
- [ ] Read a valid local association first; invalidate it after a changed
  source identifier or definitively unavailable target. Do not make a missing
  regional mapping invalidate an otherwise confirmed association.
- [ ] Add an authenticated, profile-scoped, per-ABS-item forget-match API.
  Remove the association and incremental checkpoint together, prevent an
  in-flight sync from restoring it, and make repeat deletion a safe no-op.
  Return the previous target and document that the next sync may rematch the
  same edition under the normal priority order; dry run makes no deletion.
- [ ] On an edition-specific failure during sync, bypass the edition cache to
  confirm the target edition is absent before invalidating the association
  and checkpoint. Keep user-book/read not-found and transient errors distinct;
  mark a confirmed deletion retryable without applying progress elsewhere.
- [ ] Discover or enumerate supported regions and query Audible
  `book_mappings` exactly; accept agreeing results of the correct format and
  leave conflicts or unknown regions reviewable. Reuse Step 3's first-ASIN-hit
  Audnex discovery on a mapping miss without a strict metadata gate.
- [ ] On Audnex 429 or another transient discovery failure, stop the sweep.
  Preserve independently verified matches; otherwise record a retryable
  per-book `failed` outcome without a durable association or incremental
  checkpoint that would suppress the next attempt. Genuine completed misses
  remain `needs_review`, and the rest of the run continues.
- [ ] Retire the in-memory and 24-hour positive ASIN caches without trusting
  or migrating old hits; do not persist the temporary legacy fallback.
- [ ] Test restart, concurrent save, storage failure, source change, mapping
  conflict with independent Hardcover evidence, verified-match-plus-429,
  discovery-blocking-429, all-regions-miss, forget/rematch-to-same-or-new,
  concurrent forget, confirmed edition deletion versus cached edition or
  unrelated not-found, no-op, and dry-run paths through real persistence and
  client boundaries.
- [ ] Document the persistence and read-only matching behavior, add one
  CHANGELOG bullet, and complete the shared validation and PR gates.

### Step 6 — regional import and matching switchover

- [x] Verify the published `upsert_book` scope (`write:catalog:append` or
  broader catalogue-write scope) and a successful ordinary-token live import.
  Keep normal library sync usable without catalogue-write scope.
- [ ] Require an unambiguous candidate book and established region; bound
  polling, handle `failed`/`loaded`/`created`, and verify returned book,
  edition, and reading format before saving.
- [ ] Persist confirmed `loaded` and `created` resolutions even when Hardcover
  does not retain the submitted regional alias.
- [ ] Remove audiobook `editions.asin` matching and duplicate checks from the
  sync path in this same PR; never call `insert_book_mapping`.
- [ ] Test missing-scope, timeout, conflicting-ID, import failure, no-op,
  restart, and dry-run paths without partial local success or a failed whole
  sync.
- [ ] Document the matching change and accepted legacy-record coexistence,
  add one CHANGELOG bullet, and complete the shared validation and PR gates.

### Step 7 — create capability

- [ ] Split the existing create branch so capability reporting is a standalone
  route, with authorization matching the eventual create operation.
- [ ] Report `upsert_book` capability for ISBN and regional Audible imports;
  never infer metadata editability from a successful `update_edition` response
  without a read-back. Report unknown or failed probes as unverified.
- [ ] Test no-mutation probes, scope denial, token change, transient errors,
  profile access, and dry run at the HTTP/client boundary.
- [ ] Document the route and its truthful limits in README/OpenAPI, add one
  CHANGELOG bullet, and complete the shared validation and PR gates.

### Step 8 — create endpoint

- [ ] Accept only a corrected regional Audible identifier for audiobook
  imports or a corrected ISBN for ebook imports; reject edition metadata edits
  rather than accepting and dropping them. Require a usable import identifier.
- [ ] Refetch the ABS item, use the run record's book ID for an unresolved
  identifier, and submit only platform ID, external ID, and that book ID to
  `upsert_book`. Verify the returned book ID and reading format.
- [ ] Route Audible imports through the regional resolver without an
  `edition.asin` write or duplicate guard; import ebooks by ISBN platform 8.
  Retain the original ABS and submitted identifiers in the durable
  association. Derive the audiobook preview date from the response for the
  region that resolves the submitted ASIN, with the Step 3 ABS date fallback.
  Reject a physical-format or wrong-book ISBN result as reviewable.
- [ ] Make successful create immediately findable by normal sync. Distinguish
  a remote success followed by local-store failure so a retry is safe.
- [ ] Test unauthorized/foreign profiles, wrong book, wrong reading format,
  missing or invalid identifiers, missing scope, timeouts, double submit,
  shutdown, existing edition, ISBN status uncertainty, and dry run at the
  HTTP boundary.
- [ ] Update README/OpenAPI/crosswalk, add one CHANGELOG bullet, and complete
  the shared validation and PR gates before offering the API for use.

### Step 9 — immediate one-book resync

- [ ] Add `SyncBook` through existing per-book progress, ownership,
  finished-state, checkpoint, and mutation boundaries.
- [ ] Make `resync` opt-in and exclude overlap with a full sync for the same
  profile before any create-with-resync mutation begins.
- [ ] Report resync failure separately after a successful create; attempt no
  resync or persistent state write in dry run.
- [ ] Test synced, already-current, skipped, failed, cancellation, full-sync
  contention, and no-op paths, including the race detector.
- [ ] Document request/response changes, add one CHANGELOG bullet, and
  complete the shared validation and PR gates.

### Step 10 — Sync Status UI

- [ ] Show the action only for eligible needs-review records with the
  applicable verified capability and permitted profile access.
- [ ] Separately show the current stored Hardcover target and a confirmed
  forget-match action for matched items. Explain that the next sync uses
  normal matching priority and may find the same edition again; do not imply
  any Hardcover record is deleted or another edition is forced.
- [ ] Render the draft's source identifier, established/uncertain region,
  warnings, and only supported identifier corrections (regional Audible
  identifier or ebook ISBN); escape ABS-provided strings.
- [ ] Confirm through the create POST, display reused/created and error
  outcomes, and make resync an explicit option except in dry run.
- [ ] Test capability denial, preview, edits, confirmation, transient errors,
  status polling, forget-match authorization/confirmation/same-result, and
  resync results at the web boundary.
- [ ] Update the user-facing README, add one CHANGELOG bullet, and complete
  the shared validation and PR gates.

## Resolved decisions and evidence

1. **Audible edit capability:** the ordinary full API token imported an
   Outland edition with `B07NHP9F58:uk` as `created`. A populated title and
   empty subtitle each produced a successful `update_edition` response but
   did not change on a fresh read. The original title and null subtitle
   remain intact. Audiobook metadata is therefore preview-only in Steps 3
   and 8; only the submitted regional Audible identifier is correctable.
2. **Region discovery:** Audnex's published `/books/{ASIN}` schema accepts
   AU, CA, DE, ES, FR, IN, IT, JP, US, and UK. Use the preferred region
   first, then US, CA, UK, AU, DE, FR, ES, IN, IT, JP without repeating it.
   A first matching response wins; a complete miss leaves the region unknown,
   and independent Hardcover identity conflicts remain reviewable. Use its
   `releaseDate` or the existing ABS date fallback.
3. **Durable store:** extend the versioned `sync.state_file` JSON store.
   The CLI uses it directly; web profiles each get a separate path. Step 5
   must serialize, reload, and atomically save association plus checkpoint
   under a per-file lock, and coordinate with active syncs.
4. **Catalogue-write scope:** Hardcover's published capabilities permit
   `upsert_book` with `write:catalog:append` or broader catalogue-write
   scopes; the ordinary full token completed a live import. This is separate
   from `write:catalog:map`. Tokens without catalogue-write capability
   retain ordinary library-progress sync and receive a reviewable unresolved
   item when import is required.
5. **Import-only create:** staff guidance identifies virtual platform 8 for
   ISBN imports. Live no-`book_id` requests reused Outland's ebook edition
   for ISBN `9781507000885` and audiobook edition for regional Audible ASIN
   `B07NHP9F58:uk`. A no-`book_id` request for ISBN `9781680681420`
   instead reused a physical edition on another Outland book and created no
   ebook. ISBN `9781680680065`, absent from a direct ISBN lookup, loaded an
   existing **Flybot** audiobook after asynchronous import and created no
   ebook. Step 8 uses `upsert_book` for both formats, without edition metadata
   fields. An unresolved identifier is imported with the
   confirmed book ID; every returned book and format is checked. A physical
   ISBN result or missing ISBN remains reviewable.

Sources: [Audnex API schema](https://github.com/laxamentumtech/audnexus/blob/develop/docs/index.html),
[Hardcover capability map](https://github.com/hardcoverapp/hardcover-docs/blob/main/capability-scopes.json),
and the [Audible mapping findings](../hardcover-audible-mapping-findings.md).
