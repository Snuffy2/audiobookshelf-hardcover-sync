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
  changes, the target edition is unavailable, or the user explicitly rematches
  it. It is correctable, not a permanent assertion about mutable catalogues.
- Prefer an existing confirmed local association, then an exact Audible
  `book_mappings` match. Discover the source ASIN's region before a regional
  import; do not assume that an unqualified ASIN is US. Conflicting or
  unresolvable regions remain reviewable.
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
  cache as sources of match identity. Do
  not migrate its entries wholesale: some came from the old `editions.asin`
  lookup and are not verified Audible mappings. A separate short-lived
  performance cache is not part of this plan.

Normal sync must continue for profiles without catalogue-write capability.
An `upsert_book` permission failure, timeout, ambiguous candidate, or unknown
region yields a reviewable unresolved item rather than a failed whole sync or
an unverified local association. Dry run may perform reads but neither
mutates Hardcover nor persists an association that would skip a later real
sync. No step may rely on a subsequent PR to make its newly shipped behavior
safe.
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
| 3 | Rework the read-only ABS/Audnex edition draft. Its audiobook ASIN is a source Audible identifier, never an instruction to write `edition.asin`. Show a region only when established and distinguish unknown/ambiguous region. Expose only edit promises the eventual create path can honor. The endpoint still makes no Hardcover request and works by itself. | 2 | Existing draft branch and PR need revision; full rework is allowed |
| 4 | Land the existing ISBN-10/ISBN-13 counterpart matching as its own PR. Keep its diff confined to ISBN behavior and format protection. | 2 | Existing `step_5_needs_review_add_edition` work is reusable after restacking |
| 5 | Add the durable local association, read-first lookup, region discovery, and exact Audible `book_mappings` query. Replace the positive 24-hour cache; persist only verified matches. Keep the current legacy fallback for misses during this additive step, without saving its result as a verified association. | 3, 4 | New work |
| 6 | Add the bounded `upsert_book` fallback for a missing regional mapping, validate `loaded`/`created` IDs, and persist the resolution. Remove audiobook `editions.asin` matching and its duplicate guard in this same PR. A failure or missing write scope remains `needs_review`; normal sync stays usable. | 5 | New work |
| 7 | Provide create eligibility and capability reporting, with separate truthful outcomes for edition insertion and Audible import where their permissions differ. Keep the route independently useful without exposing a create action that is not implemented. | 3, 6 | Extract from the old Step 4 branch and revise |
| 8 | Add the create POST using the resolved identifier contract. ISBN/ebook insertion uses the applicable creator behavior; Audible import uses the regional path and saves a local association before reporting success. It works through the API without resync or UI. | 3, 6, 7 | Rebuild from the old Step 4 branch |
| 9 | Add opt-in single-book read-status resync after creation, sharing the normal sync path and excluding overlapping full syncs. | 8 | Not started |
| 10 | Add the Sync Status preview, confirmation, capability, and optional resync UI. | 9 | Not started |

### Step 3: source draft

The draft fetches the expanded ABS item and may make bounded Audnex reads. It
keeps the original bare ABS ASIN separate from an optional confirmed
marketplace and from any user-submitted correction. It does not use Hardcover
to decide whether an edition exists. An unknown or ambiguous region is visible
to the caller; it is not silently changed to US. The region discovery logic
must be reusable by the Step 5 and Step 6 sync paths rather than reimplemented
inside the draft.

The existing draft PR is not protected from redesign. Reuse its ABS decoding,
metadata preparation, eligibility, and tests where they still fit. Remove or
change fields and assertions that present an audiobook ASIN as a future
`BookDtoInput.asin`. A draft may offer an editable Audible identifier only if
Step 8 can apply that edit without silently discarding it. Existing ISBN,
reading-format, date, author/narrator, language-warning, and dry-run behavior
remain within the draft's read-only scope.

Acceptance: the draft works with a bare ABS ASIN, reports a discovered region
when one is supported by evidence, reports uncertainty otherwise, and issues
zero Hardcover requests. Its API description distinguishes source identifiers
from Hardcover edition fields.

### Step 4: ISBN counterpart matching

The previous Step 5 implementation can supply this PR, but it must be rebased
onto current `develop` and narrowed to ISBN normalization, counterpart search,
and reading-format behavior. It must not reintroduce an ASIN fallback or
silently modify the later Audible resolver. The PR is useful immediately for
ISBN-only and ebook items; no later step is needed for those matches to work.

### Step 5: durable association and read-only Audible matching

Use the application's persistent profile data, rather than the cache
directory, for a versioned association. The record needs the profile and ABS
item identity, the ABS source ASIN, the resolved regional external ID, the
Hardcover book and edition IDs, and resolution provenance. A changed source
identifier cannot reuse a stale association. Define a controlled rematch path
and recover from a removed or merged Hardcover edition. Writes must be safe
across restarts and concurrent syncs; failures must not masquerade as a saved
match.

On a local miss, probe exact regional Audible mappings with supported
marketplace identifiers and confirm that any multiple hits agree on the same
book and edition and have the expected audiobook format. Audnex may establish
a region for a bare ASIN when the
mapping is absent; an absence or conflict is not proof that the book is
absent. Continue to ISBN and title/author candidate discovery as appropriate.
During this step only, preserve the existing `edition.asin` fallback on an
unresolved audiobook so behavior does not regress before Step 6. Never write
that fallback into the durable association. Retire the old 24-hour positive
ASIN cache without importing it into the new store.

Acceptance: exact mapping and already-confirmed local association produce the
correct IDs after a restart; old unverified cache entries do not; an
unresolved regional alias remains reviewable; dry run writes no association.

### Step 6: missing-mapping resolution and the matching switchover

Only a credible, unambiguous candidate book may be supplied to `upsert_book`.
The existing title/author result is a candidate, not by itself an edition
match. Do not depend on `upsert_book` without a `book_id`; that behavior has
not been established by live testing. Bound asynchronous polling, preserve
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
unchanged. Split its capability route, authorization, create-time name
resolution, request validation, in-flight guard, and tests into reviewable
Step 7 and Step 8 diffs. Existing Step 2 `edition.Creator` and its CLI callers
are not automatically changed by this plan; Step 8 must explicitly route the
new application audiobook path around any legacy `edition.asin` write or
duplicate check. ISBN and ebook behavior remains format-aware.

Before Step 3's edit promises and Step 8's POST contract are finalized, run a
focused live test on a disposable catalogue record with ordinary API
credentials: establish the required `upsert_book` scope and whether the token
can update a `loaded` or `created` edition with user-edited metadata, then
read it back. The existing `insert_edition` scope probe proves only insertion
capability; it must not be reported as proof of Audible import or update
capability. If edits cannot be applied, revise the audiobook draft and create
contract explicitly rather than accepting fields and ignoring them. This is
an open design gate, not an assumed API feature.

Create refetches the ABS item and uses the run record's Hardcover book ID,
never a client-supplied book ID. It handles a changed submitted Audible ASIN
as an explicit user correction: retain both the original ABS source value and
the regional identifier submitted for import in the local association. A
create response must distinguish an edition already created remotely from a
failure to save the local association, so retry cannot quietly create an
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

## Remaining decisions and evidence gates

1. **Audible edit capability:** test `upsert_book` permission and whether
   ordinary credentials can update and read back metadata on an imported
   edition. Set the Step 3 and Step 8 editable fields from that result.
2. **Region discovery policy:** define the supported marketplace set and a
   bounded search order. Distinguish one confirmed region, multiple
   equivalent regions, conflicting regions, and no result without guessing.
3. **Durable-store location:** select the existing persistence boundary that
   works in single-user and multiuser modes, survives restarts/backups, and
   permits profile-scoped correction and safe concurrent writes.
4. **Catalogue-write scope:** verify the `upsert_book` authorization needed by
   ordinary API credentials. The absence of that scope must not become a new
   requirement for normal library-progress sync.

These gates may refine fields and PR boundaries. They do not change the
confirmed separation between Audible mappings, local resolution, and the
legacy `editions.asin` field.
