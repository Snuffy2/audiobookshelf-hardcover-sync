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
The application must retain a confirmed resolution locally so subsequent
syncs find the same edition after restarts. A successful resolution has no
routine expiry.

**Sync never writes to the Hardcover catalogue.** Normal sync (scheduled,
manual, CLI, or web) performs only catalogue reads plus the existing
library-progress, read-status, and ownership writes. It never calls
`upsert_book`, `insert_edition`, `update_edition`, or `insert_book_mapping`.
A catalogue write happens only when the user explicitly calls the create API,
runs the standalone `edition create` command, or clicks the UI action that
calls the create API. An audiobook sync cannot resolve through an exact
mapping, a local association, or another independent match remains
`needs_review` with enough detail for the user to add the edition.

The user-approved identifier rules for the completed flow (after Step 10) are:

- Use a durable, profile-scoped association from the ABS item and its source
  identifiers (ASIN, ISBN-10, and ISBN-13, whichever the item has) to the
  resolved Hardcover `book_id` and `edition_id`. Record the
  region-qualified identifier used for resolution, when there is one, and how
  the IDs were confirmed. Invalidate or re-evaluate the association when any
  recorded ABS source identifier changes on a processed sync, the target
  edition is definitively unavailable, or the user explicitly forgets the
  match. It is correctable, not a permanent
  assertion about mutable catalogues.
- For an audiobook, sync prefers an existing confirmed local association,
  then an exact Audible `book_mappings` match, then ISBN and title/author
  discovery. The exact mapping lookup queries every Hardcover Audible region
  form of the bare ASIN in one read (see [Regions](#regions)); it needs no
  Audnex region discovery. Agreeing hits on one edition with audiobook format
  are a match; hits on different editions or books remain reviewable.
- For an ebook, sync prefers an existing confirmed local association, then an
  `editions.asin` match on an ebook-format edition, then ISBN and
  title/author discovery. An ebook's ASIN is an Amazon retail (Kindle)
  identifier, which is what `editions.asin` holds, so it stays a match and
  ranks above ISBN, as it does today. Audible mappings are not queried for
  ebooks.
- Do not use `editions.asin` to match an ABS audiobook to Hardcover, to
  choose a candidate, or as a duplicate guard for an audiobook import. Step 10
  removes the current audiobook `editions.asin` matching once the
  user-initiated create flow and UI can resolve the items this exposes. An
  older edition that contains an Audible ASIN only in that field may coexist
  with a newly imported edition that has a proper Audible mapping. Ebook
  `editions.asin` matching, the ebook creator's ASIN duplicate check, and
  writing an ebook's ASIN to `edition.asin` on ebook insertion are kept.
- The user-initiated audiobook create path discovers the source ASIN's region
  before a regional import; it does not assume that an unqualified ASIN is US.
  It submits the regional external ID through `upsert_book` with the run
  record's confirmed book ID. A validated `loaded` result is a successful
  resolution even if Hardcover never stores that alias; a validated `created`
  result identifies the new edition. The create API, and `edition create`
  when given an ABS item ID, persist either result locally only after its
  book and edition IDs are confirmed.
- Do not call `insert_book_mapping`. Ordinary API credentials cannot be
  assumed to have `write:catalog:map`, even though ordinary web users may add
  Audible identifiers through Hardcover's webpage.
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
Dry run may perform reads but neither mutates Hardcover nor persists an
association that would skip a later real sync. No step may rely on a
subsequent PR to make its newly shipped behavior safe.

Adding an edition requires an ASIN or ISBN. An ABS item with neither is not
eligible for the draft/create API or UI action, and standalone `edition create`
rejects input with neither. The final submitted input must retain at least one
identifier after normalization; an ABS item ID is not a substitute. Audiobook
imports additionally require a regional Audible ASIN; an ISBN alone cannot
drive that importer. Ebook insertion accepts a Kindle ASIN, an ISBN, or both.

Keep the existing Hardcover `Owned`-list ownership checks and Audiobookshelf
finished-state behavior throughout the matching and resync changes.

### Regions

Two region lists are used, for different purposes:

- **Hardcover Audible mapping regions (11):** `us`, `ca`, `uk`, `au`, `de`,
  `fr`, `es`, `in`, `it`, `jp`, `br`. Each has been observed as the suffix of
  an Audible `book_mappings.external_id` on a real Hardcover book. Sync's
  exact mapping lookup and create-time validation use this list.
- **Audnex lookup regions (10):** US, CA, UK, AU, DE, FR, ES, IN, IT, JP, the
  regions in Audnex's published `/books/{ASIN}` API schema. Region discovery
  and the `audnexus_region` setting use this list. `br` (Brazil) is an
  Audible region but not an Audnexus region, so it cannot be discovered
  through Audnex; a Brazilian identifier can still match an existing
  `ASIN:br` mapping or be supplied explicitly by the user at create time.

### Audnex region discovery

Region discovery is a shared helper used by the Step 3 draft and the Step 7
create API and CLI. Sync does not use it for matching. Sync's existing mismatch
export enrichment (the configured region, then US) is unchanged by this plan.

- Try the configured preferred region first (US when none is set), then sweep
  the remaining Audnex regions once in this fixed order: US, CA, UK, AU, DE,
  FR, ES, IN, IT, JP, skipping the preferred region, so there are at most ten
  lookups. Bound the whole sweep with one deadline that includes the client's
  retries.
- Stop at the first response whose returned ASIN equals the requested ASIN.
  A response for a different or missing ASIN is treated as a miss for that
  region and the sweep continues. Use the matching response's region for the
  regional identifier and its `releaseDate` for edition metadata; the
  preference is not an assertion that every ABS ASIN belongs to that
  marketplace. Do not reject an ASIN hit merely because Audnex title, author,
  narrator, or edition details differ from ABS. Keep Hardcover book/edition
  identity checks separate from this lookup.
- The Audnex client must report typed outcomes: not found (404), rate limited
  (429), and transient (network, timeout, 5xx after retries). Today every 4xx
  becomes the same untyped error and is logged at Error level. A per-region
  miss is expected during a sweep and is not logged as an error.
- A 429 or transient error stops the sweep. It is not evidence that the ASIN
  is absent: report the region as temporarily unavailable, never as a guessed
  or unknown region. A completed sweep with only misses leaves the region
  unknown. Neither outcome triggers an import. The draft shows a retryable
  warning; the create API and CLI refuse the import with a retryable error
  unless the user supplied an explicit regional identifier.

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
| 3 | Read-only ABS/Audnex edition draft. Its audiobook ASIN is a source Audible identifier, never an instruction to write `edition.asin`. Add the shared Audnex region discovery with typed client errors and take `releaseDate` from the region that resolves it. Show a region only when established and distinguish unknown and temporarily unavailable. Audiobook metadata is preview-only; the submitted Audible identifier may be corrected. The endpoint makes no Hardcover request and works by itself. | 2 | Rewrite from current `develop`; the existing branch follows the old plan and is not a source |
| 4 | Land the existing ISBN-10/ISBN-13 counterpart matching as its own PR. Keep its diff confined to ISBN behavior and format protection. | 2 | Existing `step_5_needs_review_add_edition` work is reusable after restacking |
| 5 | Add the durable local association, a profile-scoped forget-match API, read-first lookup, and the exact 11-region Audible `book_mappings` lookup. Invalidate on a freshly confirmed deleted edition and clear the item's incremental checkpoint. Replace the positive 24-hour cache; persist only verified matches. Add the cross-process state-file lock for sync and forget-match. Keep the existing `editions.asin` fallback, unpersisted, until Step 10. No catalogue writes. | 4 | New work |
| 6 | Report create capability for ebook `insert_edition` and regional Audible `upsert_book`. Keep the route independently useful without exposing a create action that is not implemented. | 3, 5 | Extract from the old Step 4 branch and revise |
| 7 | Add the user-initiated create POST: format-aware `insert_edition` for ebooks and a bounded regional `upsert_book` resolver for audiobooks. Migrate standalone `edition create` to the same resolver and remove its audiobook `insert_edition` route. Validate returned book and format; the API, and the CLI when given an ABS item ID, save a local association before reporting success. Reuse Step 5's state-file lock. | 3, 5, 6 | Rebuild from the old Step 4 branch |
| 8 | Add opt-in single-book read-status resync after creation, sharing the normal sync path and excluding overlapping full syncs. | 7 | Not started |
| 9 | Add the Sync Status preview, confirmation, capability, optional resync, and forget-match UI. | 8 | Not started |
| 10 | Stop matching ABS books to Hardcover by `editions.asin`: remove the sync fallback and the audiobook `editions.asin` duplicate guard. Items that relied on it become `needs_review`, which the Step 7–9 create flow resolves. | 9 | Not started |

Step 10 is last so that every user has the create API, CLI, and UI before
matches that depended on `editions.asin` become reviewable.

### Step 3: source draft

Rewrite this step from current `develop`. The existing Step 3 branch and PR
implement the old plan and are not a source for this design.

The draft fetches the expanded ABS item and may make bounded Audnex reads
through the shared [region discovery](#audnex-region-discovery) helper. It
keeps the original bare ABS ASIN separate from an optional confirmed
marketplace and from any user-submitted correction. It does not use Hardcover
to decide whether an edition exists. An unknown or temporarily unavailable
region is visible to the caller; it is not silently changed to US.

Use `releaseDate` from the successful Audnex response, regardless of whether
it came from the preferred or a fallback region. If that response has no
release date, or no region resolves, fall back to the existing ABS published
date/year precedence. In the read-only draft, a rate limit or transient error
is a retryable warning with the ABS date fallback, not a sync outcome.

Expand `audnexus_region` validation from six to the ten Audnex regions,
normalize the value to lowercase, and warn and use US for an unsupported
value. Update environment handling and README to match. `br` is not an
accepted Audnex preference.

Add the Audnex client's typed not-found, rate-limited, and transient errors in
this step, and stop logging an expected per-region miss as an error. Sync's
mismatch export enrichment keeps its current region behavior; if the draft
reuses the mismatch enrichment code, the region sweep must be injected only
for the draft so sync makes no additional Audnex requests.

For audiobooks, offer a correction to the submitted regional Audible
identifier (ASIN and region, including `br`), which Step 7 passes to
`upsert_book`; show title, subtitle, date, edition information, ISBNs, and
other imported metadata as read-only preview. Do not accept audiobook metadata
edits that the import cannot save. For ebooks, retain candidate edition fields
in the draft, including an optional corrected ISBN; Step 7 may accept only
fields its format-aware insertion has verified to persist. An ISBN is optional
only when a usable ASIN is present; an item with neither cannot add an edition.
Reading-format, date, author/narrator, language-warning, and dry-run behavior
remain within the draft's read-only scope.

Acceptance: the draft works with a bare ABS ASIN, reports a discovered region
when one is supported by evidence, reports unknown or temporarily unavailable
otherwise, and issues zero Hardcover requests. Its release date comes from the
Audnex response for the discovered region, with ABS date fallback when that
response has no date or no region resolves. Its API description distinguishes
source identifiers from Hardcover edition fields. A sync run makes the same
Audnex requests as before this step.

### Step 4: ISBN counterpart matching

The previous Step 5 implementation can supply this PR, but it must be rebased
onto current `develop` and narrowed to ISBN normalization, counterpart search,
and reading-format behavior. It must not reintroduce an ASIN fallback or
silently modify the later Audible matching. An ebook's `editions.asin`
match stays ahead of its ISBN match. The PR is useful immediately for
ISBN-only and ebook items; no later step is needed for those matches to work.

### Step 5: durable association and read-only Audible matching

Extend the existing versioned `sync.state_file` JSON store, rather than the
cache directory or the web-only database. The one-time CLI already uses this
file; the web service derives a separate state path per profile, and deployment
examples mount it in persistent `/data` or `/app/data`. Keep the association
and incremental checkpoint in the same state-file record. The record needs the
ABS item identity, the item's source identifiers as ABS reported them when the
match was confirmed (ASIN, ISBN-10, and ISBN-13, each only when present), any
user-submitted correction, the resolved regional external ID when there is
one, the Hardcover book and edition IDs, the reading format, and resolution
provenance; the profile is identified by the state-file path. Audiobook and
ebook associations use the same record. Compare identifiers after the
existing normalization (trimmed ASIN, ISBN without separators), so formatting
alone is not a change. On a processed sync, if any recorded identifier later
differs, is removed, or a previously absent identifier appears, the association
is not reused and the item is matched normally. Keep the existing incremental
progress/status filters: an identifier-only change does not force a sync or
matching. Check the association when the item next needs processing, or after
the user explicitly forgets the match. An item with no identifiers cannot use
the add-edition flow.

Bump the schema version and read existing v3 checkpoint files without losing
their books. `LoadState` currently rejects any unknown version, so a release
before this step cannot read the new file: document the downgrade limit in
`MIGRATION.md`. Retain the existing atomic replace/directory-sync durability.

Each sync run currently loads the state once and rewrites the whole file from
its in-memory copy after every book and at the end. An out-of-band write to
the same file, such as forget-match or the Step 7 create API saving an
association, would be overwritten by the running sync's next checkpoint.
Forget-match and the Step 7 create API are therefore refused with HTTP 409
while that profile has an active sync run; the multi-user service already
tracks active runs. The check happens before any state change or Hardcover
mutation, so a refused create has written nothing. While a forget or create
request is in progress it holds the profile's run guard, so a sync cannot
start until it finishes and a sync start during that window is refused or
waits as an ordinary overlapping run would. The UI disables these actions
during a sync and explains why. Queuing, writing into the running sync's
memory, and merge-on-save were considered and not chosen.

The in-process guard cannot protect against a separate CLI sync or web
process sharing the state path. Add the cross-process state-file lock in this
step: every sync run acquires it before loading state and holds it through
its final save, and forget-match holds it across the entire load/change/save
operation. Refuse forget-match with 409 when the file is busy, before changing
state. Competing sync runs must not proceed with an unlocked state snapshot.
The lock must work on every platform the state package supports and recover
from a stale lock left by a crashed process. Step 7's create writers reuse
this lock before any Hardcover mutation and through the local save.

Add an authenticated, profile-authorized API action to forget one ABS item's
association, clear its incremental checkpoint, and make the next sync perform
ordinary matching again. Return the previous resolution and whether an
association was removed; an absent association is a safe no-op. The action
changes no Hardcover catalogue or library record. Dry run does not delete the
association or checkpoint; the API reports that no persistent change was
made. It does not promise a different result: unchanged Hardcover identifiers
may resolve to the same edition; a changed, removed, or newly higher-priority
candidate may resolve differently. A changed source identifier cannot reuse a
stale association. Failures must not masquerade as a saved match.

An unchanged item may be skipped by incremental sync before any Hardcover
read, so this plan does not promise continuous deletion detection. Identify
the specific Hardcover operations and error shapes that indicate the mapped
edition may be gone, for example a user-book or read mutation that rejects the
edition ID, and cover each with a test; a generic error is never a trigger.
When one occurs, bypass the existing `GetEdition` cache and check that edition
ID directly. Only a definitive absent edition invalidates the association and
checkpoint. Do not infer edition deletion from a missing user book, missing
read, GraphQL transport failure, permission error, or absent regional
`book_mappings` row. Record the current item as retryable `failed` without
applying progress to another edition; let the next sync rematch. Clearing an
association must not erase historical Hardcover reads or ownership.

On a local miss, query exact Audible mappings for the bare ASIN combined with
each of the 11 Hardcover mapping regions in one read. Hardcover stores no
Audible mapping without a region, so the bare ASIN alone is not queried as a
mapping `external_id`. Confirm that any multiple hits agree on the same book
and edition and have audiobook format; disagreement remains `needs_review`.
Rework the existing `SearchBookByASIN` query rather than adding a parallel
one: it currently ORs `editions.asin`, a bare-ASIN mapping, and one
configured-region mapping with `limit: 1`, so callers cannot tell which
clause matched. Split it so a mapping match is distinguishable from the
`editions.asin` fallback. A verified mapping match is saved as an
association. An absence is not proof that the book is absent; continue to
ISBN and title/author candidate discovery as appropriate.

The Audible mapping lookup applies only to audiobooks. For an ebook, the
split keeps today's order and results: an `editions.asin` match on an
ebook-format edition is tried before ISBN and is a real match, not a legacy
fallback. It is not saved as an association in this step, like an ISBN
match; it is found again on each processed sync.

`SearchBookByASIN` has callers besides sync matching. `GetEditionByASIN`
wraps it, and the edition creator's duplicate check calls that. The mismatch
export calls it in `AddWithMetadata` to attach Hardcover book details to the
exported JSON. Audit every production caller when splitting the query, keep
each caller's current results in this step, and cover them with tests. Their
`editions.asin` use is removed in Step 10.

Until Step 10, preserve the existing `editions.asin` fallback for an
unresolved audiobook so behavior does not regress before the create flow
exists. Never write that fallback into the durable association. Retire the old
in-memory and 24-hour positive ASIN caches without importing them into the new
store.

Acceptance: exact mapping and already-confirmed local association produce the
correct IDs after a restart; old unverified cache entries do not; the forget
API forces the next sync to re-evaluate; a freshly confirmed deleted edition
invalidates only its local association; dry run writes no association; sync
makes no catalogue write.

### Step 6: create capability

Split the capability route, authorization, and tests from the old Step 4
create branch. Report ebook `insert_edition` and Audible `upsert_book`
capability separately. Hardcover's published capability map permits both
mutations under `write:catalog:append` (or the broader `write:catalog:edit`
and `write:catalog` scopes); `write:catalog:map` is separate. Identify a
read-only signal that reveals a token's catalogue scopes before relying on
one. If none exists, the route reports the capability as unverified. The UI
still offers creation to otherwise eligible, authorized users with a warning
that catalogue permission is unverified; the create action attempts the
operation and reports a permission failure clearly. A known denial prevents
the action. Never infer editability from a successful `update_edition` response
without a read-back.

### Step 7: create API and standalone CLI

This is the only path that writes to the Hardcover catalogue, and it runs only
on an explicit user request. The current create branch assumes
`insert_edition` plus a duplicate search in `edition.asin` can handle an
Audible ASIN. That behavior cannot merge unchanged; rebuild the create-time
name resolution, request validation, in-flight guard, and tests from it.

The application create endpoint uses a bounded regional `upsert_book` resolver
(platform 32) for audiobooks and `insert_edition` for ebooks. There is no
audiobook `insert_edition` fallback after a missing region, permission denial,
import failure, timeout, or identity conflict: the item remains reviewable.
The resolver supplies the run record's confirmed book ID; the no-`book_id`
form has been tested only with identifiers already present in Hardcover,
including one that resolved to a different book. Bound asynchronous polling,
preserve the client's rate limiting and retry behavior, and distinguish
`failed`, `loaded`, and `created`. Confirm returned book and edition IDs, the
expected book, and audiobook reading format before reporting success. If the
returned identity conflicts, report it and save nothing. Do not use
`editions.asin` as a duplicate guard for an audiobook import.

The ebook insertion uses Step 2's format-aware `edition.Creator` path, which
already sends `reading_format_id` from the requested reading format (`4` for
ebook, `2` for audiobook). An ordinary-token ebook insertion and read-back
remain a Step 7 verification gate before the ebook path is advertised.

The standalone `cmd/edition` CLI currently sends audiobooks to
`insert_edition` through `edition.Creator`. Migrate its `create` command to the
shared regional `upsert_book` resolver and remove audiobook `insert_edition`
from that CLI. The mismatch export writes JSON for this command that includes
title, author and narrator IDs, publisher, dates, and a bare `asin`. For an
audiobook import, the CLI reads the book ID, ASIN, optional region, and
reading format, and ignores the other fields so existing export files keep
working; it documents which fields an audiobook import uses. Ebook creation
keeps using the supplied metadata. Keep book and reading-format checks for
both formats.

The CLI can also save the local association, so a CLI-only user whose
audiobook import returns `loaded` without a stored mapping is not left in
`needs_review`. `edition create` accepts an ABS item ID from a flag or from an
`abs_item_id` field that the mismatch export adds (an additive field; older
export files without it still work but save nothing). When an item ID is
given, the CLI:

- fetches that item from the configured Audiobookshelf server before any
  Hardcover mutation, refuses if it is missing or its format differs from the
  request, and records the item's own source identifiers; a different
  submitted ASIN is recorded as the user's correction, as in the API;
- writes to the configured `sync.state_file`, or to an explicit
  `--state-file` path, using the same Step 5 record, version, and atomic save;
- saves the association, for both `loaded` and `created` results and for
  ebooks, only after the returned book, edition, and reading format are
  verified;
- refuses before any Hardcover mutation, with a clear non-zero exit, while a
  sync holds that state file, and holds the state-file lock itself until the
  association is saved;
- reports a remote success followed by a failed local save as a distinct
  outcome, so a retry is safe;
- saves nothing in dry run.

Without an item ID, the CLI reports the verified IDs and status and saves
nothing.

Reuse Step 5's cross-process state-file lock for the create API and for
`edition create` when given an ABS item ID. Acquire it before loading state
or making any Hardcover mutation, refuse when busy, and hold it until the
association is saved. Keep the API's profile run guard as well.

The live test with an ordinary full API token successfully called `upsert_book`
for a known book and regional Audible ASIN; import status became `created`
and a subsequent read found the new edition and mapping. The same token's
`update_edition` calls returned an ID and no errors for both a populated
title and an empty subtitle, but fresh reads retained the original title and
null subtitle. A success response is therefore not proof that submitted
metadata persisted. Steps 3 and 7 must not promise audiobook metadata
edits. The supported correction is the regional Audible identifier submitted
to `upsert_book`; returned audiobook metadata is preview-only. Ebook insertion
may accept only fields the format-aware create path verifies it can persist.
Runtime authorization and read-back remain the source of truth for a given
token.

Hardcover staff also describe an ISBN import variant of `upsert_book` with
`platform_id: 8` and the ISBN as `external_id`. Platform 8 is not returned
by the live `platforms` catalogue, so treat it as an undocumented import
source rather than a discoverable mapping platform. A live call with an
existing Outland ISBN and the known book ID returned the existing physical
edition immediately; `book_import_statuses` still reported `not_found` for
that platform and ISBN. This confirms ISBN reuse, not creation of a new
edition or reliable status polling for that source. A second live request
omitted `book_id` and returned Outland's existing ebook edition for ISBN
`9781507000885` (format 4); the same no-`book_id` form returned its existing audiobook for `B07NHP9F58:uk`
(format 2). Omitting `book_id` is valid for these existing identifiers, but
these calls do not establish where an absent identifier will be imported.
A requested no-`book_id` import of ISBN `9781680681420` returned existing
physical edition `33271167` (format 1) on a different Outland book
(`2933828`), not a new ebook on the intended book (`461193`). A fresh read
confirmed no ebook with that ISBN was added. Another ISBN,
`9781680680065`, had no direct edition match before import. Its no-`book_id`
upsert returned null IDs, then import status `loaded` resolved to **Flybot**
book `1931466`, audiobook edition `32083414` (format 2); no ebook was added.
A later edition read showed no persisted ISBN despite the `loaded` import
status. A test of `update_edition` on the authorized Outland audiobook returned
no error when asked to change format 2 to 4, but read-back stayed at 2; a
restore request and final read also showed 2. These results rule out ISBN
`upsert_book` plus a format update as a reliable ebook creation path. Step 7
uses `insert_edition` for ebooks, with same-book and format-aware duplicate
checks. Do not treat an ISBN upsert ID or `not_found` import status alone as
proof of a suitable ebook edition.

The API create path refetches the ABS item and uses the run record's
Hardcover book ID for both ebook insertion and audiobook import, never a
client-supplied book ID. Before any catalogue mutation, compare the current
ABS item's normalized source identifiers and reading format with the snapshot
recorded by the sync that selected that Hardcover book. A changed, removed,
or newly present source identifier, or a changed format, makes the run record
stale: return 409 and require a fresh sync/review. Record the source snapshot
in run outcomes; older records without enough data to establish agreement
also require a fresh sync. Formatting-only identifier changes are accepted.
This create-time check does not change incremental sync's skip behavior.

Compare the refetched ABS source with the run snapshot before applying request
edits. A deliberately submitted Audible identifier correction remains allowed
when that source snapshot agrees; it is not an ABS source change. Retain both
the original ABS source value and the regional identifier submitted for import
in the local association. A
create-time lookup of an unqualified submitted ASIN uses the shared
[region discovery](#audnex-region-discovery); when release date is
server-derived, its baseline comes from the response for the region that
actually resolves that ASIN. Audiobook date is not an editable request field.
A create response must distinguish an edition already created remotely from a
failure to save the local association, so retry cannot quietly create an
unrelated second edition. Dry run makes no Hardcover mutation and saves no
association. The POST must be usable with curl, and its result must be
findable by the next normal sync before Step 8 exists.

### Step 8: optional immediate resync

Add `Service.SyncBook` through the existing per-book processing path. Both
ordinary create and opt-in create-with-resync are refused with 409 during a
full sync for the same profile, before any catalogue mutation. Keep the
profile guard and state-file lock through the optional resync. A resync failure
is reported separately because the edition may already exist. Dry run attempts
no resync and persists no progress or association. Test success, no-op,
failure, cancellation, and concurrent full-sync paths with the race detector.

### Step 9: UI

Show the create action only for authorized profiles and eligible run records
with an ASIN or ISBN. A confirmed capability or an unverified capability
permits the action; show the unverified-permission warning and report any
permission failure from the POST clearly. A known denial prevents creation.
Preview the ABS source identifier and any established region without implying
that `edition.asin` is the Audible destination. Show
only edits that Step 7 can honor, escape ABS-provided strings, and display
unknown or temporarily unavailable region and unresolved candidate results as
review states. Confirmation uses the create POST; the resync checkbox is
opt-in. Dry run is clearly identified and does not offer a real resync.

Also expose the Step 5 forget-match action for already matched items, not only
`needs_review` items. Show the current Hardcover book/edition target and ask
for confirmation. Explain that the next sync reruns normal matching and may
select the same edition if Hardcover has not changed. Do not describe this as
deleting a Hardcover edition or mapping, or as forcing a different target.
Disable the real forget action in dry run.

### Step 10: stop matching by `editions.asin`

Remove `editions.asin` from every production audiobook path that chooses a
Hardcover book or edition:

- sync's audiobook matching;
- the mismatch export's Hardcover book lookup in `AddWithMetadata`, which
  must not attach an `editions.asin`-only book to the exported JSON;
- `GetEditionByASIN` and the edition creator's duplicate check, for
  audiobooks.

Ebook `editions.asin` matching stays, ahead of ISBN, and so does the ebook
creator's ASIN duplicate check. Re-audit the `SearchBookByASIN` and
`GetEditionByASIN` callers so none still reaches `editions.asin` for an
audiobook and every ebook caller still does.

Items whose only link was `editions.asin` become reviewable on their next
processed sync. When title/author search finds the book, they are
`needs_review` and the user resolves them through the create flow, where a
`loaded` result saves an association. When title/author search also fails,
they are `not_found` with no Hardcover book ID, and the in-app create flow
cannot help; the user fixes them in Hardcover or with the CLI and a
hand-entered book ID. This is accepted. A title already represented only by a
legacy `editions.asin` may receive another edition with a correct Audible
mapping; that is also accepted.

Incremental sync skips an item whose progress and status have not changed
since its last checkpoint, before any matching. A book already synced through
`editions.asin` therefore keeps its existing Hardcover edition, reads, and
ownership until its ABS progress or status changes; only then is it
rematched. This step does not clear checkpoints to force that rematch: the
existing Hardcover records remain valid, and forcing a rematch would move
unchanged books to review for no user benefit. Document this in
`MIGRATION.md`, with the forget-match action as the way to rematch one book
now. Existing associations and exact mapping matches are unaffected.

Acceptance: a processed audiobook whose only Hardcover link is `editions.asin`
is `needs_review` or `not_found`, not matched; the mismatch export attaches no
`editions.asin`-only book; exact mapping, association, and ISBN matches are
unchanged; an ebook with a matching `editions.asin` still matches by ASIN
before ISBN.

## PR and branch handling

- Steps 1 and 2 are merged; do not reopen their PRs for this change.
- The existing Step 3 [fork PR #28](https://github.com/Snuffy2/audiobookshelf-hardcover-sync/pull/28)
  and [upstream PR #198](https://github.com/drallgood/audiobookshelf-hardcover-sync/pull/198)
  implement the old plan. Step 3 is rewritten from current `develop`; the
  rewrite can replace that branch's contents or supersede those PRs only on
  the owner's explicit command.
- The old Step 5 branch is source material for new Step 4. Rebase and audit its
  diff against current `develop` rather than merging the old stacked branch.
- Do not merge the old Step 4 create branch as-is. Restack and split its useful
  code into Steps 6 and 7, preserving the create endpoint's authorization,
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

5. Durable Audible matching: Store confirmed ABS-to-Hardcover associations, offer a forget-match API, recover from confirmed edition deletion, and match exact regional Audible mappings without catalogue writes. Protect sync and forget-match with a cross-process state-file lock while preserving incremental skip behavior.

6. Create capability: Report whether the profile can insert ebook editions or import regional Audible identifiers. Allow an otherwise eligible create attempt when permission is unverified, with a warning.

7. Edition create endpoint and CLI: Require an ASIN or ISBN, reject stale API run records, and insert format-aware ebooks or import regional Audible identifiers on request. Validate book and format, then save the association under Step 5's lock from the API or CLI with an ABS item ID.

8. Immediate read-status resync: Optionally sync the created edition's one ABS item's read status. Both ordinary create and create-with-resync refuse overlap with a full sync.

9. Sync Status UI: Preview and confirm edition creation for eligible needs-review items, offer optional resync, and let users forget a stored match for a future retry.

10. Stop matching by edition ASIN: Match audiobooks only by association, exact Audible mapping, ISBN, or title and author; items that relied on `editions.asin` become reviewable.

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
  By @Snuffy2`. Add the upstream PR number once the rewritten PR exists. Any
  existing bullet that claims a later step must put Audible identifiers
  directly in `book_mappings` must be replaced.
- **Step 4 — Fixed:** `**ISBN counterpart matching**: Match an ABS ISBN-10 to
  its valid ISBN-13 counterpart, and the reverse, without changing
  reading-format separation. By @Snuffy2`.
- **Step 5 — Added:** `**Durable Audible matches**: Persist verified ABS item
  to Hardcover book and edition resolutions, offer a per-item forget-match
  API, recover from confirmed edition deletion, and match exact Audible
  mappings in every Hardcover region while retiring the expiring ASIN cache.
  By @Snuffy2`.
- **Step 6 — Added:** `**Edition creation capability**: Report the profile's
  applicable ebook insertion and Audible import capabilities without
  promising permission from an unrelated scope check. By @Snuffy2`.
- **Step 7 — Added:** `**Create editions from needs-review items (API and CLI)**:
  On request, create or reuse an edition through format-aware ebook insertion
  or regional Audible import, migrate the standalone audiobook CLI to the
  regional importer, let the CLI save the match for an ABS item, and validate
  book and format; dry runs make no external change. By @Snuffy2`.
- **Step 8 — Added:** `**Immediate one-book resync**: Optionally sync read
  status after edition creation while excluding an overlapping full sync. By
  @Snuffy2`.
- **Step 9 — Added:** `**Edition creation in Sync Status**: Preview and
  confirm an eligible needs-review edition in the UI, show capability and
  region uncertainty, optionally resync its read status, and forget a stored
  match for future rematching. By @Snuffy2`.
- **Step 10 — Changed:** `**Audible matching no longer uses edition ASINs**:
  Audiobooks whose only Hardcover link was an edition's ASIN field are no
  longer matched through it; those found by title and author can be resolved
  with the add-edition action. By @Snuffy2`.

Step 3 owns the draft README/OpenAPI description and the Audnex region
setting; Steps 4 and 5 document matching and the new persistence; Step 6
documents its capability route; Step 7 documents the create route, standalone
CLI contract, the fields an audiobook import reads, and exact identifier
corrections; Step 8 documents the `resync` request/response; Step 9 documents
the user flow; Step 10 documents the matching change and its migration note.
Update the field crosswalk as the relevant step lands, rather than leaving its
old R4 destination as an implementation contract.

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

### Step 3 — source draft (rewrite)

- [x] Establish ordinary-token Audible import/update behavior: import
  succeeded, but attempted title and subtitle edits did not persist. Limit
  audiobook correction to the submitted regional identifier.
- [ ] Start from current `develop`; do not carry the old Step 3 branch.
- [ ] Define the draft schema and crosswalk R4 so they preserve the bare ABS
  source ASIN, optional established region, and unknown or temporarily
  unavailable state without implying an `edition.asin` write or silently
  choosing US.
- [ ] Add typed Audnex not-found, rate-limited, and transient errors; log an
  expected per-region miss below Error level.
- [ ] Add the shared region discovery: preferred region first (US when
  unset), then the fixed ten-region sweep, stopping at the first response for
  the requested ASIN, under one overall deadline. Do not require close
  title/author/narrator/edition agreement.
- [ ] Populate the draft release date from the successful region's Audnex
  `releaseDate`, falling back to the ABS published date/year if it is absent
  or no region resolves. Test preferred hit, fallback-region hit, no result,
  429 and transient error with a retryable warning, and a response for a
  different ASIN treated as a miss.
- [ ] Expand six-region config validation to the ten Audnex regions and
  normalize the preferred region; keep `br` out of the Audnex setting.
- [ ] Verify sync's mismatch export makes no additional Audnex requests.
- [ ] Verify audiobook/ebook metadata, ISBN flags, author/narrator names,
  language and date warnings, ASIN-or-ISBN eligibility (including neither
  present), cancellation, and zero Hardcover requests at the HTTP boundary.
- [ ] Update README, OpenAPI, the single CHANGELOG bullet, and the PR
  description; run the shared validation and PR gates.

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
  web persistence.
- [x] Decide how forget-match and create-API writes coordinate with a running
  sync: refuse with HTTP 409 while the profile is syncing.
- [ ] Refuse forget-match with 409 during an active run before any state
  change, and hold the profile's run guard while it runs so a sync cannot
  start mid-write.
- [ ] Add the cross-process state-file lock for every sync run and forget-match
  operation. Acquire before loading state, hold through the final save, refuse
  competing access, recover stale locks, and support all state-package
  platforms. Test a separate CLI sync holding the file against forget-match.
- [ ] Bump the state version, read v3 files without loss, and document the
  downgrade limit in `MIGRATION.md`.
- [ ] Define one association record for audiobooks and ebooks that stores
  whichever of ASIN, ISBN-10, and ISBN-13 the item had, compared after
  normalization. A changed, removed, or newly present identifier prevents
  reuse on a processed sync. Preserve incremental skipping when progress and
  status are unchanged; identifier changes alone do not force matching.
- [ ] Read a valid local association first; invalidate it after a changed
  source identifier or definitively unavailable target. Do not make a missing
  regional mapping invalidate an otherwise confirmed association.
- [ ] Add an authenticated, profile-scoped, per-ABS-item forget-match API.
  Remove the association and incremental checkpoint together, and make repeat
  deletion a safe no-op. Return the previous target and document that the
  next sync may rematch the same edition under the normal priority order; dry
  run makes no deletion.
- [ ] Name the Hardcover operations and errors that suggest a deleted edition.
  On one, bypass the edition cache to confirm the target edition is absent
  before invalidating the association and checkpoint. Keep user-book/read
  not-found and transient errors distinct; mark a confirmed deletion retryable
  without applying progress elsewhere.
- [ ] Split `SearchBookByASIN` so exact Audible mappings for all 11 Hardcover
  regions are queried in one read and are distinguishable from the
  `editions.asin` fallback. Accept agreeing results of audiobook format;
  leave conflicts reviewable. Do not query a bare-ASIN mapping. Save verified
  mapping matches as associations.
- [ ] Keep ebook matching by `editions.asin` on an ebook-format edition ahead
  of ISBN; query Audible mappings only for audiobooks. Test an ebook whose
  ASIN and ISBN point to different editions.
- [ ] Audit every production `SearchBookByASIN` caller, including
  `GetEditionByASIN`, the edition creator's duplicate check, and the mismatch
  export's book lookup; keep their current results and test them.
- [ ] Retire the in-memory and 24-hour positive ASIN caches without trusting
  or migrating old hits; do not persist the temporary `editions.asin`
  fallback.
- [ ] Test restart, concurrent save, storage failure, ASIN and ISBN source
  changes on processed syncs (including a formatting-only change that is not
  a change), identifier-only changes skipped by incremental sync, mapping
  conflict, forget/rematch-to-same-or-new, forget during an active sync,
  confirmed edition deletion versus cached edition or unrelated not-found,
  no-op, dry run, and zero catalogue mutations through real persistence and
  client boundaries.
- [ ] Document the persistence and read-only matching behavior, add one
  CHANGELOG bullet, and complete the shared validation and PR gates.

### Step 6 — create capability

- [ ] Split the existing create branch so capability reporting is a standalone
  route, with authorization matching the eventual create operation.
- [ ] Find a read-only way to learn a token's catalogue scopes, or report the
  capability as unverified when none exists. Unverified permits an otherwise
  eligible create attempt with a warning; a known denial prevents it.
- [ ] Distinguish ebook `insert_edition` capability from Audible
  `upsert_book` capability; never infer editability from a successful
  `update_edition` response without a read-back.
- [ ] Test no-mutation probes, scope denial, token change, transient errors,
  profile access, and dry run at the HTTP/client boundary.
- [ ] Document the route and its truthful limits in README/OpenAPI, add one
  CHANGELOG bullet, and complete the shared validation and PR gates.

### Step 7 — create endpoint and standalone CLI

- [x] Verify the published `upsert_book` scope (`write:catalog:append` or
  broader catalogue-write scope) and a successful ordinary-token live import.
- [ ] Add the bounded regional `upsert_book` resolver: require the run
  record's book ID and an established or user-supplied region, bound polling,
  handle `failed`/`loaded`/`created`, and verify returned book, edition, and
  reading format. Callable by the API and CLI; each saves an association
  only when it has an ABS item.
- [ ] Accept only a corrected regional Audible identifier for audiobook
  imports through the API; reject audiobook metadata edits rather than
  accepting and dropping them. Keep ebook edits tied to fields the
  format-aware insertion honors.
- [ ] Refetch the ABS item and use the run record's book ID, never a client
  book ID. Compare its normalized source identifiers and format with the
  run-record snapshot before applying user corrections or making mutations.
  Return 409 for changed source identity or a missing snapshot and require a
  fresh sync/review; accept formatting-only changes. Resolve required and
  optional people and publisher metadata for ebook insertion only after
  confirmation. Verify that an ordinary token can
  insert and read back an ebook through Step 2's format-aware path before
  advertising it.
- [ ] Record source identifiers and reading format in sync run outcomes so
  create can verify the source snapshot. Older outcomes lacking sufficient
  data require a fresh sync/review before creation.
- [ ] Route Audible imports through the regional resolver without an
  `edition.asin` write, `editions.asin` duplicate guard, or `insert_edition`
  fallback. Retain the original ABS and submitted Audible identifiers in the
  durable association. Derive the audiobook preview date from the response
  for the region that resolves the submitted ASIN, with the Step 3 ABS date
  fallback. Verify the returned book and format and guard against same-book
  and cross-book ebook duplicates.
- [ ] Migrate standalone `edition create` from audiobook `insert_edition` to
  the regional resolver. Require a confirmed regional Audible ID, either
  supplied explicitly or found by the shared Audnex discovery; never infer
  US. Read the book ID, ASIN, optional region, and reading format from the
  input and ignore the informational fields a mismatch export includes. An
  unknown region, missing permission, failed import, timeout, or identity
  conflict exits without `insert_edition` or a claimed success. Preserve
  dry-run's no-mutation behavior. Remove or restrict the old `edition.Creator`
  audiobook insertion so no production caller can still use `insert_edition`
  for an audiobook.
- [ ] Update `edition prepopulate`, help, and `cmd/edition/README.md` to
  describe which fields an audiobook import reads, the ABS item ID option,
  and the state-file flag. Document that without an item ID a `loaded`
  result without a stored mapping stays unresolved for sync.
- [x] Decide how a CLI-only user saves a `loaded` result: `edition create`
  takes an ABS item ID and saves the association to the state file.
- [ ] Accept an ABS item ID by flag or the export's new `abs_item_id` field.
  Fetch and verify the item before any mutation, then save the verified
  association to `sync.state_file` or `--state-file` for `loaded` and
  `created` audiobooks and inserted ebooks; distinguish a failed local save
  after remote success; save nothing in dry run or without an item ID.
- [ ] Reuse Step 5's state-file lock in the create API and `edition create`
  with an ABS item ID. Acquire before loading state or any Hardcover mutation,
  refuse when busy, and hold through the association save.
- [ ] Refuse the create POST with 409 while the profile is syncing, before any
  Hardcover mutation, and hold the profile's run guard until the association
  is saved.
- [ ] Make successful API create immediately findable by normal sync.
  Distinguish a remote success followed by local-store failure so a retry is
  safe.
- [ ] Require an ASIN or ISBN for add-edition eligibility and final create
  input, including standalone CLI input without an ABS item ID. Reject
  missing or whitespace-only identifiers before any catalogue mutation; an
  audiobook import specifically requires a regional Audible ASIN. Test no
  identifiers, ASIN-only ebook, ISBN-only ebook, and corrections that remove
  the last identifier.
- [ ] Test stale run records (changed, added, or removed ABS identifiers or
  changed format), missing source snapshots, formatting-only changes, and
  deliberate submitted corrections; rejected stale requests make no catalogue
  mutation.
- [ ] Test unauthorized/foreign profiles, wrong book, wrong reading format,
  invalid fields, missing author, optional misses, missing scope, timeouts,
  double submit, shutdown, existing edition, ISBN conflicts, and dry run at
  the HTTP boundary. Test CLI `loaded`/`created`, unknown region, an existing
  mismatch-export file with and without `abs_item_id`, a missing or
  mismatched ABS item, a state file locked by a running sync, a failed local
  save, a saved association found by the next sync, wrong book/format,
  missing scope, import failure, timeout, prepopulate, and dry run at its
  command boundary; assert no
  audiobook `insert_edition` call and audit remaining production callers.
- [ ] Update README/OpenAPI/crosswalk and the CLI documentation, add one
  CHANGELOG bullet, and complete the shared validation and PR gates before
  offering either create path for use.

### Step 8 — immediate one-book resync

- [ ] Add `SyncBook` through existing per-book progress, ownership,
  finished-state, checkpoint, and mutation boundaries.
- [ ] Make `resync` opt-in. Refuse both ordinary create and create-with-resync
  during a full sync before any catalogue mutation; hold the profile guard
  and state-file lock through the optional resync.
- [ ] Report resync failure separately after a successful create; attempt no
  resync or persistent state write in dry run.
- [ ] Test synced, already-current, skipped, failed, cancellation, full-sync
  contention, and no-op paths, including the race detector.
- [ ] Document request/response changes, add one CHANGELOG bullet, and
  complete the shared validation and PR gates.

### Step 9 — Sync Status UI

- [ ] Show the action only for eligible needs-review records with an ASIN or
  ISBN and permitted profile access. Allow confirmed or unverified capability,
  warn for unverified permission, and prevent creation on a known denial.
- [ ] Separately show the current stored Hardcover target and a confirmed
  forget-match action for matched items. Explain that the next sync uses
  normal matching priority and may find the same edition again; do not imply
  any Hardcover record is deleted or another edition is forced.
- [ ] Render the draft's source identifier, established, unknown, or
  temporarily unavailable region, warnings, and only supported editable
  fields (regional Audible identifier correction for audiobooks;
  format-aware insertion fields for ebooks); escape ABS-provided strings.
- [ ] Confirm through the create POST, display reused/created and error
  outcomes, and make resync an explicit option except in dry run.
- [ ] Disable create and forget-match while the profile is syncing, explain
  why, and handle a 409 from a sync that started after the page loaded.
- [ ] Test capability denial, unverified capability with successful creation
  or permission failure, missing-identifier ineligibility, preview, edits,
  confirmation, stale-record 409, transient errors,
  status polling, forget-match authorization/confirmation/same-result, and
  resync results at the web boundary.
- [ ] Update the user-facing README, add one CHANGELOG bullet, and complete
  the shared validation and PR gates.

### Step 10 — stop matching by `editions.asin`

- [ ] Remove `editions.asin` from sync's audiobook matching, the mismatch
  export's book lookup, and the audiobook path of `GetEditionByASIN` and the
  creator's duplicate check; re-audit every caller.
- [ ] Test an `editions.asin`-only audiobook that title/author finds
  (`needs_review`) and one it does not (`not_found`), the mismatch export for
  such a book, an unchanged already-synced book that incremental sync skips,
  exact mapping, association, ISBN, and an ebook that matches by
  `editions.asin` ahead of a conflicting ISBN.
- [ ] Document the change, both resolution paths, the unchanged-book skip,
  and forget-match as the way to rematch now in README and `MIGRATION.md`;
  add one CHANGELOG bullet, and complete the shared validation and PR gates.

## Resolved decisions and evidence

1. **No automatic catalogue writes:** sync never calls a Hardcover catalogue
   mutation. Imports and insertions happen only through the user-initiated
   create API, CLI, or UI action. Unresolved audiobooks remain `needs_review`.
2. **No `editions.asin` matching:** ABS books are not matched to Hardcover by
   `editions.asin`. The existing fallback remains only until Step 10, after
   the create flow and UI exist. Step 10 removes it from sync, the mismatch
   export, and the audiobook duplicate check. A book whose only link was
   `editions.asin` and that title/author search cannot find becomes
   `not_found` without an in-app fix; this is accepted. Hardcover stores no
   Audible mapping without a region, so exact lookups use only the 11
   regional forms.
3. **Audible edit capability:** the ordinary full API token imported an
   Outland edition with `B07NHP9F58:uk` as `created`. A populated title and
   empty subtitle each produced a successful `update_edition` response but
   did not change on a fresh read. The original title and null subtitle
   remain intact. Audiobook metadata is therefore preview-only in Steps 3
   and 7; only the submitted regional Audible identifier is correctable.
4. **Regions:** all 11 Hardcover Audible mapping regions (`us`, `ca`, `uk`,
   `au`, `de`, `fr`, `es`, `in`, `it`, `jp`, `br`) were observed on real
   Hardcover books. Audnex's published `/books/{ASIN}` schema accepts the
   first ten. Exact mapping lookup uses all 11; region discovery uses the
   Audnex ten, preferred region first, and the first matching response wins.
5. **Durable store:** extend the versioned `sync.state_file` JSON store.
   The CLI uses it directly; web profiles each get a separate path.
   Forget-match and create-API writes are refused with HTTP 409 while the
   profile is syncing, and hold the profile's run guard while they run.
   Step 5 adds the cross-process state-file lock for sync and forget-match;
   Step 7 reuses it for the create API and `edition create`.
6. **CLI association:** `edition create` accepts an ABS item ID and saves the
   verified match to the state file, so a CLI-only user's `loaded` import is
   found by later syncs.
7. **Catalogue-write scope:** Hardcover's published capabilities permit
   `upsert_book` with `write:catalog:append` or broader catalogue-write
   scopes; the ordinary full token completed a live import. This is separate
   from `write:catalog:map`. Tokens without catalogue-write capability
   retain ordinary library-progress sync; only the create action fails.
8. **ISBN import is not the ebook create path:** staff guidance identifies
   virtual platform 8 for ISBN imports. Live no-`book_id` requests reused
   Outland's ebook edition for ISBN `9781507000885` and audiobook edition for regional Audible ASIN
   `B07NHP9F58:uk`. A no-`book_id` request for ISBN `9781680681420`
   instead reused a physical edition on another Outland book and created no
   ebook. ISBN `9781680680065`, absent from a direct ISBN lookup, loaded an
   existing **Flybot** audiobook after asynchronous import and created no
   ebook; later reads showed no persisted ISBN on that edition. An ordinary
   token's `update_edition` format change also returned success without a
   persisted change. Step 7 uses Step 2's format-aware `insert_edition` for
   ebooks and regional `upsert_book` for audiobooks, migrates the standalone
   `edition create` command to regional `upsert_book`, and validates the
   returned book and reading format on both paths.
9. **Incremental matching:** unchanged progress/status may skip an item before
   matching even when its source identifiers changed. Revalidate the association
   when the item next needs processing or the user explicitly forgets it.
10. **Create eligibility:** an ASIN or ISBN is required; items with neither
    cannot add an edition. Audiobook imports require a regional Audible ASIN;
    ebooks accept a Kindle ASIN, ISBN, or both.
11. **Stale create requests:** compare the refetched ABS source identifiers and
    format with the run snapshot before mutation. Reject stale or unverifiable
    records with 409; intentional request corrections remain distinct from ABS
    source changes.

Sources: [Audnex API schema](https://github.com/laxamentumtech/audnexus/blob/develop/docs/index.html),
[Hardcover capability map](https://github.com/hardcoverapp/hardcover-docs/blob/main/capability-scopes.json),
and the [Audible mapping findings](../hardcover-audible-mapping-findings.md).
