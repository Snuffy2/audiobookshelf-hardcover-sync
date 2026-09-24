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
- For Audnex discovery, try the configured preferred region first (US when no
  preference is set), then make a bounded sweep of other Audnex-supported
  regions only on a true miss. Stop at the first response for the requested
  ASIN. Use that response's region for the regional identifier and its
  `releaseDate` for edition metadata; the preference is not an assertion that
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
| 3 | Rework the read-only ABS/Audnex edition draft. Its audiobook ASIN is a source Audible identifier, never an instruction to write `edition.asin`. Discover its region with preferred-first, bounded Audnex lookup and take `releaseDate` from the region that resolves it. Show a region only when established and distinguish unknown/ambiguous region. Expose only edit promises the eventual create path can honor. The endpoint still makes no Hardcover request and works by itself. | 2 | Existing draft branch and PR need revision; full rework is allowed |
| 4 | Land the existing ISBN-10/ISBN-13 counterpart matching as its own PR. Keep its diff confined to ISBN behavior and format protection. | 2 | Existing `step_5_needs_review_add_edition` work is reusable after restacking |
| 5 | Add the durable local association, read-first lookup, shared preferred-first Audnex region discovery, and exact Audible `book_mappings` query. Replace the positive 24-hour cache; persist only verified matches. Keep the current legacy fallback for misses during this additive step, without saving its result as a verified association. | 3, 4 | New work |
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

The configured Audnex region is the first lookup preference, defaulting to US
when unset. If the requested ASIN is absent there, probe the other supported
Audnex regions in a fixed, bounded order and stop at the first response for
that ASIN. Do not make title/author/narrator/ISBN/runtime similarity a hard
gate for region discovery; these are not the Hardcover identity checks. Use
`releaseDate` from the successful response, regardless of whether it came
from the preferred or a fallback region. If that response has no release date,
fall back to the existing ABS published date/year precedence. If no supported
region resolves the ASIN, keep the region unknown and use the same ABS date
fallback; do not automatically import a regional Audible identifier. A
transport error or rate limit is not evidence that the ASIN is absent; report
the lookup as unresolved rather than turning that failure into a guessed
region. Align config validation with the verified Audnex-supported region
list before enabling the sweep; a region seen only in Hardcover mappings is
not thereby a supported Audnex lookup region.

The existing draft PR is not protected from redesign. Reuse its ABS decoding,
metadata preparation, eligibility, and tests where they still fit. Remove or
change fields and assertions that present an audiobook ASIN as a future
`BookDtoInput.asin`. A draft may offer an editable Audible identifier only if
Step 8 can apply that edit without silently discarding it. Existing ISBN,
reading-format, date, author/narrator, language-warning, and dry-run behavior
remain within the draft's read-only scope.

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
a region for a bare ASIN when the mapping is absent, using Step 3's
preferred-first, bounded, first-ASIN-hit lookup. Do not impose a strict
Audnex-to-ABS metadata comparison on that lookup. An absence or conflict with
independent Hardcover evidence is not proof that the book is absent. Continue
to ISBN and title/author candidate discovery as appropriate.
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
create-time lookup of that submitted ASIN uses the same preferred-first
discovery rule; when release date is server-derived, its baseline comes from
the response for the region that actually resolves that ASIN, not a separate
lookup forced to the configured region. A supported, explicit user date edit
still takes precedence over that baseline. A
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

5. Durable Audible resolution: Store confirmed ABS-to-Hardcover book/edition associations, reuse preferred-first Audnex region discovery, read exact regional Audible mappings, and retire the 24-hour positive ASIN cache.

6. Missing regional mapping: Resolve a known candidate with `upsert_book`, retain validated `loaded` or `created` IDs locally, and stop matching audiobooks by `editions.asin`.

7. Create capability: Report whether the profile can perform the applicable edition insertion or Audible import before offering the create action.

8. Edition create endpoint: Add or reuse a Hardcover edition for a needs-review item, using regional Audible import for audiobooks and saving a confirmed local association.

9. Immediate read-status resync: Optionally sync the created edition's one ABS item's read status without overlapping a full sync.

10. Sync Status UI: Preview, confirm, and report edition creation for an eligible needs-review item, with an optional resync.

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
  to Hardcover book and edition resolutions, search exact regional Audible
  mappings using preferred-first region discovery, and retire the expiring
  positive ASIN cache. By @Snuffy2`.
- **Step 6 — Changed:** `**Audible identifier resolution**: Resolve a missing
  regional mapping through Hardcover import when permitted, retain confirmed
  results locally, and stop treating edition ASINs as Audible matches; items
  without a verified resolution remain reviewable. By @Snuffy2`.
- **Step 7 — Added:** `**Edition creation capability**: Report the profile's
  applicable insertion and Audible import capabilities without promising
  permission from an unrelated scope check. By @Snuffy2`.
- **Step 8 — Added:** `**Create editions from needs-review items (API)**:
  Create or reuse an edition after confirmation, import Audible identifiers
  through the regional path, and retain its confirmed local resolution; dry
  runs make no external change. By @Snuffy2`.
- **Step 9 — Added:** `**Immediate one-book resync**: Optionally sync read
  status after edition creation while excluding an overlapping full sync. By
  @Snuffy2`.
- **Step 10 — Added:** `**Edition creation in Sync Status**: Preview and
  confirm an eligible needs-review edition in the UI, show capability and
  region uncertainty, and optionally resync its read status. By @Snuffy2`.

Step 3 owns the draft README/OpenAPI description; Steps 4–6 document matching
and any new persistence or permission behavior; Step 7 documents its
capability route; Step 8 documents the create route and its exact editable
fields; Step 9 documents the `resync` request/response; Step 10 documents the
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
- [ ] Establish the ordinary-token Audible import/update behavior needed to
  decide which audiobook fields the draft may truthfully call editable.
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
  transient error, and malformed or mismatched returned ASIN.
- [ ] Reconcile six-region config validation with the verified Audnex lookup
  set; do not include a Hardcover-only mapping region without Audnex support.
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

- [ ] Choose and document the profile-scoped durable store and versioned
  record, including ABS item/source ASIN, regional ID, Hardcover IDs, and
  provenance; cover single-user and multiuser persistence.
- [ ] Read a valid local association first; invalidate it after a changed
  source identifier or unavailable target, and provide a controlled rematch.
- [ ] Discover or enumerate supported regions and query Audible
  `book_mappings` exactly; accept agreeing results of the correct format and
  leave conflicts or unknown regions reviewable. Reuse Step 3's first-ASIN-hit
  Audnex discovery on a mapping miss without a strict metadata gate.
- [ ] Retire the in-memory and 24-hour positive ASIN caches without trusting
  or migrating old hits; do not persist the temporary legacy fallback.
- [ ] Test restart, concurrent save, storage failure, source change, mapping
  conflict with independent Hardcover evidence, no-op, and dry-run paths
  through real persistence and client boundaries.
- [ ] Document the persistence and read-only matching behavior, add one
  CHANGELOG bullet, and complete the shared validation and PR gates.

### Step 6 — regional import and matching switchover

- [ ] Verify the ordinary API-token scope for `upsert_book` and keep normal
  library sync usable without it.
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
- [ ] Distinguish proven `insert_edition` capability from verified Audible
  import and metadata-update capability; report unknown or failed probes as
  unverified rather than allowed.
- [ ] Test no-mutation probes, scope denial, token change, transient errors,
  profile access, and dry run at the HTTP/client boundary.
- [ ] Document the route and its truthful limits in README/OpenAPI, add one
  CHANGELOG bullet, and complete the shared validation and PR gates.

### Step 8 — create endpoint

- [ ] Set the request's editable fields from the Step 3 live capability result;
  reject unsupported edits rather than accepting and dropping them.
- [ ] Refetch the ABS item, use the run record's book ID, derive non-editable
  fields server-side, and resolve required/optional people and publisher
  metadata only after confirmation.
- [ ] Route Audible imports through the regional resolver without an
  `edition.asin` write or duplicate guard; retain the original ABS and
  submitted identifiers in the durable association. Derive an unedited
  release date from the response for the region that resolves the submitted
  ASIN, with the Step 3 ABS date fallback. Preserve ISBN/ebook insertion and
  same-book checks.
- [ ] Make successful create immediately findable by normal sync. Distinguish
  a remote success followed by local-store failure so a retry is safe.
- [ ] Test unauthorized/foreign profiles, wrong book, invalid fields,
  missing author, optional misses, missing scope, timeouts, double submit,
  shutdown, existing edition, and dry run at the HTTP boundary.
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
- [ ] Render the draft's source identifier, established/uncertain region,
  warnings, and only supported editable fields; escape ABS-provided strings.
- [ ] Confirm through the create POST, display reused/created and error
  outcomes, and make resync an explicit option except in dry run.
- [ ] Test capability denial, preview, edits, confirmation, transient errors,
  status polling, and resync results at the web boundary.
- [ ] Update the user-facing README, add one CHANGELOG bullet, and complete
  the shared validation and PR gates.

## Remaining decisions and evidence gates

1. **Audible edit capability:** test `upsert_book` permission and whether
   ordinary credentials can update and read back metadata on an imported
   edition. Set the Step 3 and Step 8 editable fields from that result.
2. **Region discovery implementation:** verify the Audnex-supported
   marketplace set, reconcile the current six-region config validation, and
   define a fixed bounded sweep order after the configured preference (US
   when unset). Stop at the first response for the requested ASIN; do not
   continue probing just to compare metadata across regions. A true miss
   leaves the region unknown, while conflict with independent Hardcover
   evidence remains reviewable. The matched response supplies `releaseDate`
   when present, otherwise use the ABS date fallback.
3. **Durable-store location:** select the existing persistence boundary that
   works in single-user and multiuser modes, survives restarts/backups, and
   permits profile-scoped correction and safe concurrent writes.
4. **Catalogue-write scope:** verify the `upsert_book` authorization needed by
   ordinary API credentials. The absence of that scope must not become a new
   requirement for normal library-progress sync.

These gates may refine fields and PR boundaries. They do not change the
confirmed separation between Audible mappings, local resolution, and the
legacy `editions.asin` field.
