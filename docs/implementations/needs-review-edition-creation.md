# Implementation Plan: Add an Edition to Hardcover from a `needs_review` Book

**Status: 🚧 IN PROGRESS**

The feature is delivered as **seven steps**, each its own **upstream** PR (to `drallgood/audiobookshelf-hardcover-sync`)
that leaves `develop` working and shippable. The former combined branch has been split: steps 1-4 exist as four stacked
branches. Steps 5, 6 and 7 follow, and steps 6 and 7 are not started.

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
| 1 | ISBN package, reading-format helpers, mismatch export fixes | `step_1_needs_review_add_edition` | [#24](https://github.com/Snuffy2/audiobookshelf-hardcover-sync/pull/24) | [#195](https://github.com/drallgood/audiobookshelf-hardcover-sync/pull/195) | `develop` | ~950 (15 files, ~585 of it tests), measured | Built and validated; under maintainer review (see its checklist) |
| 2 | Edition creator hardening (duplicate detection, cross-book guard, `edition_format`, token scoping, cover warning, ebook format) and the `edition` CLI field | `step_2_needs_review_add_edition` | — | — | 1 | ~912 (266 prod, 646 tests), measured | Built and validated. See its checklist under "Step checklists" |
| 3 | Read-only draft endpoint (also carries the response write-deadline mechanism, see below) | `step_3_needs_review_add_edition` | — | — | 1, 2 | ~2,147 (~1,078 prod and docs, ~1,069 tests), measured | Built and validated |
| 4 | Create endpoint, its guards, and docs | `step_4_needs_review_add_edition` | — | — | 3 | ~1,525 (~588 prod and docs, ~937 tests), measured | Built and validated |
| 5 | Sync identifier matching | `step_5_needs_review_add_edition` | — | — | 1 only | ~500 (~335 tests), measured | Implemented and validated; needs a rebase onto step 1 (see the Step 5 notes) |
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
matching (5) merged, and step 7 needs step 6. Step 6 also needs `develop` with #188 merged (it changed
`internal/sync/service.go`, which `SyncBook` will call into). After step 4 the feature is usable with curl; step 7 is the
first thing a user sees. Steps 6 and 7 build on the create response shape from step 4, so a change requested in step 4's
review carries into them. Each step's single CHANGELOG bullet carries its own upstream PR number as `(#NNN)`, added when that
upstream PR is created; fork PR numbers such as #20 are not that number.

## Crosswalk scope by step

[needs-review-edition-field-crosswalk.md](needs-review-edition-field-crosswalk.md) maps every Audiobookshelf field to its
Hardcover edition field. Its rows have stable IDs, R1 to R18. Each step **delivers** some rows (its code implements or
changes that behavior, and its tests pin it) and **verifies** others (it depends on behavior that already exists, so a test
must fail if that changes). This table is the summary; the crosswalk's
[section 5](needs-review-edition-field-crosswalk.md#5-what-each-step-must-deliver-from-the-crosswalk) has the edge cases each
step must cover, and if the two ever differ the crosswalk wins and this table is corrected. Each step's checklist below
carries the matching item, and its PR description names the rows it delivers.

| Step | Delivers | Verifies | Crosswalk findings to decide |
|------|----------|----------|------------------------------|
| 1 ISBN and export foundations | R5, R6 (ISBN split); R10 (unresolved publisher is 0); R11, R13, R14 for ebook exports; R14 `Abridged` for an audiobook; R12 (format helpers) | Audiobook export otherwise unchanged: R2-R4, R7-R9, R11, R13-R17 | 1 (decided and done) |
| 2 Edition creator | The Hardcover side of every row: R1-R16 (the `dto`, duplicate detection), R17 (cover chain, token scoping) | R5, R6 forms from step 1 | 5, 8 |
| 3 Draft endpoint | The ABS side: decode, then R1-R17 as they appear in the draft; identifier requirement | R5, R6, R10-R14 export behavior; R12 | 2, 3, 4, 7, 9 (6 is informational) |
| 4 Create endpoint | The editable set, validation and normalization: R1, R2, R4-R7, R9-R13, R15, R16; R12 and R17 derived on the server | R3, R8, R14 pass through unchanged | 8 (PR testing notes), 5 |
| 5 Sync matching | R4, R5, R6, R12 as matching: the sync finds what the crosswalk creates | none | none |
| 6 Resync | nothing new | R1, R4-R6, R12: the resync finds the edition just created | none |
| 7 UI | What is editable, shown, hidden and echoed: R2-R14, R17 | Escaping of every ABS-supplied string | 9 (UI part), 3 if language is shown |

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
- [ ] **CHANGELOG.md: exactly ONE bullet per PR.** The whole step goes into a single `[Unreleased]` bullet, in the one section
      (Added, Changed or Fixed) that fits it best, in the repo's style: `**Short title**: what it delivers and any behavior
      that changes. By @Snuffy2 (#NNN)`. Side effects that would otherwise be extra bullets (a changed default, a fixed
      bug) are folded into that bullet's text. Never edit or remove an earlier step's bullet.
- [ ] The CHANGELOG `(#NNN)` is the upstream PR number; add it only when that upstream PR exists.
- [ ] Nothing here has run against real Hardcover (image upload, `insert_edition`, its duplicate behavior for ISBN/ASIN, and
      the ebook reading format id 4 with the `Ebook` label) or live Audnex. Say so in each PR's testing notes for steps 2-4,
      and do a manual check against a real Hardcover account before the upstream PRs for steps 2-4 if the owner wants one.
      Also unchecked: that an ordinary Hardcover token may call `insert_edition` and `insert_image` (Hardcover's
      capabilities file lists them under `write:catalog*` scopes), and that `language_id` 1 and `country_id` 1 are English
      and the United States (see the crosswalk).
- [ ] **Crosswalk:** the PR description names the crosswalk rows (R-numbers) the step delivers and verifies, taken from the
      "Crosswalk scope by step" table above; the step's tests cover each delivered row's edge cases from the crosswalk's
      section 5, at a real interface (the GraphQL variables sent, the HTTP response, the export JSON), not implementation
      details; if the code behaves differently from a row, correct the crosswalk in the same commit as the code.

**Step 1** (fork PR #24, upstream PR #195)
- [x] Crosswalk scope ([section 5, Step 1](needs-review-edition-field-crosswalk.md#step-1-isbn-and-export-foundations)):
      deliver R5 and R6 (hyphenated ISBN-13 and ISBN-10 kept, lowercase `x`, other separators, a 979 or wrong-shape value,
      no derived counterpart in the export), R10 (unresolved publisher is 0, late-resolved ID exported), R12 (format helpers
      and their truth table), R11, R13 and R14 for an ebook export, and R14 for an audiobook (`Abridged`, see finding 1
      below); verify the audiobook export is otherwise unchanged (R2-R4, R7-R9, R11, R13-R17, including R8's date
      normalization and year fallback). Done: `internal/isbn`, `internal/models/reading_format_test.go`,
      `internal/mismatch/reading_format_test.go` and the `mismatch_test.go` cases pin each row at the export boundary.
- [x] Collapse this step's three CHANGELOG bullets (hyphenated ISBNs, unresolved publisher, ebook export) into one bullet, per
      the one-bullet-per-PR rule. The bullet carries `(#195)`, the upstream PR number.
- [x] Read the CodeRabbit feedback on #24 and address the valid items. F1 (the stale publisher ID in `ToEditionExport`) and
      both threads about the CHANGELOG ebook wording and the case-insensitive ebook placeholder filter are fixed.
- [x] The PR body says `isbn.Result.ISBN10()/ISBN13()/Counterpart` have their first production caller in step 2; keep that note.
- [x] Crosswalk finding 1: decided and done. ABS `metadata.abridged` is decoded, carried on the mismatch record, and an
      abridged audiobook exports `edition_information: "Abridged"`; every other audiobook still exports `Unabridged`. It is a
      behavior change to the audiobook export, so it is called out in the CHANGELOG bullet and in the PR body. (An earlier
      change had narrowed the audiobook rule without being asked; it was reverted because the audiobook export must otherwise
      stay unchanged.)
- [x] The PR description follows the template, names the crosswalk rows this step delivers and verifies, carries the "Multi-Step
      Project" block, and has no issue links (upstream's default branch is `main` and the base is `develop`).
- [x] Maintainer review on #195: every point verified against current code; dispositions:
  - **Q1, checksum-invalid ISBNs: decided.** Keep accepting by shape and keep the value in `isbn_10` / `isbn_13`, and report the
    checksum: `isbn.Result.Valid`, and `isbn_10_valid` / `isbn_13_valid` in the export (omitted when that ISBN is empty; the
    `edition` command ignores them). The policy is documented in the `isbn` package. Why: Hardcover's docs list
    `isbn_10_valid` / `isbn_13_valid` on `editions`, which suggests it stores and flags such ISBNs, and step 5 must still find
    an edition by the given value. **Not verified:** whether `insert_edition` accepts a bad checksum, whether a lookup by one
    matches, and whether Audiobookshelf routinely serves them; none of it has been run against the real API. The open part
    (warn or reject on a `false` flag when drafting or creating) is queued under steps 3 and 4.
  - **Q2, ebook with an audiobook label: fixed.** `ToEditionExport` now forces `Ebook` for any ebook record (R11 says
    `Ebook`), with a test over `""`, `Audiobook`, `Audible Audio` and `libro.fm`.
  - **ABS schema: fixed.** `abridged` added beside `explicit` in `bookMetadataBase` in
    `internal/api/audiobookshelf/audiobookshelf-openapi.json`. The suggested raw-response fixture change is declined:
    `audiobookshelf_raw_response.json` is a 129-byte debug dump written by the client at runtime, no test reads it, and it has no
    `isbn` or `explicit` either.
  - **Nits:** the saved-export test sorts the files (`filepath.Glob` output is already lexical and the names carry a
    `%03d` prefix, so the flake risk was low); the CHANGELOG bullet has `(#195)`; the PR body said "no audio length" but the
    export writes `audio_seconds: 0`; `isbn.Parse`, `Result`, `ISBN10()` and `ISBN13()` have no production caller until
    step 2, as the PR already says.
- [ ] Apply the PR body fixes to #24 and #195 (`audio_seconds: 0`; the `isbn_10_valid` / `isbn_13_valid` sentence; the test
      list; the "audiobook export otherwise unchanged" claim becomes "adds the two ISBN flags"), and reply to the maintainer
      with the dispositions above. Then read the new CodeRabbit feedback and address the valid items.
- [ ] One CodeRabbit thread (case-insensitive audiobook placeholder filter in `ToEditionExport`) was verified and skipped: the
      filter is `develop`'s own audiobook rule, kept unchanged on purpose, its only production writer is the constant
      `"Audiobookshelf"` it already rejects, and step 3 deletes it with `EditionInfo`. Reply to and resolve the thread when the
      owner says so; do not change the audiobook rule in step 1.
- [ ] Rebase steps 2-5 onto step 1 after its review fixes: they changed `internal/mismatch/types.go` (forced `Ebook`,
      `MarkEbook`, the ISBN flags), `internal/isbn/isbn.go`, the ABS OpenAPI schema and step 1's CHANGELOG bullet (now with
      `(#195)`), so expect conflicts in those files and in `CHANGELOG.md`; step 3's edit must still leave step 1's bullet
      untouched.

**Step 2** (`edition` CLI behavior changes)
- [ ] Crosswalk scope ([section 5, Step 2](needs-review-edition-field-crosswalk.md#step-2-edition-creator-hardening)): this
      step owns everything sent to Hardcover, so assert the exact `GraphQLMutation` variables for R1-R16 (omitted-when-empty,
      greater-than-0-only, format fallbacks, `reading_format_id` 2 or 4, narrators and audio dropped for an ebook, date must
      be `YYYY-MM-DD`), the duplicate detection for R4-R6 (order, format scoping, same-book reuse, cross-book refusal), and
      the R17 cover chain (token only to the configured ABS host, `ImageError`, extension from `Content-Type`); verify the
      ISBN forms from step 1. Findings 5 and 8.
- [ ] Collapse this step's CHANGELOG bullets into ONE bullet (one-bullet-per-PR rule) that also covers the Audiobookshelf token
      scoping (`SetAudiobookshelfBaseURL`) and the cover-upload failure signal (`EditionResult.ImageError`, and `Existing` in
      the `edition` command's JSON output), which no branch had a line for.
- [ ] The fork PR description says `Creator.SetAudiobookshelfBaseURL` has no production caller until step 4 (`internal/multiuser`), so
      token scoping is tested here but inert for the `edition` CLI until then; and that the `edition` command's behavior changes
      (honored `edition_format`, duplicate and cross-book detection, `existing` in its output, optional `reading_format`).
- [ ] Decide CodeRabbit item F3: the pre-existing `CheckRedirect` in `edition.NewCreator` that copies the `Authorization` header
      to redirects. Either fix it here or open a separate small PR (owner's choice); do not let it drop.

**Step 3**
- [ ] Crosswalk scope ([section 5, Step 3](needs-review-edition-field-crosswalk.md#step-3-draft-endpoint)): this step owns
      the ABS side and the mapping into the draft. Decode the ABS item (`GetLibraryItem`, expanded, 404 typed) and decide
      what else the model reads (findings 2-4; `abridged`, finding 1, is already decoded by step 1); build fixtures from a real expanded item, one audiobook and one ebook; pin
      R1 (book ID from the run record, identifier requirement), R5 and R6 (counterpart only when derivable), R7 and R9
      (warnings, no narrator for an ebook), R8 (Audnex fallback, year fallback, warning), R10 (warning), R11, R13 and R14
      (rounding, ebook), R12 (`reading_format`), R15 and R16, and R17 (`CoverURL`: empty, stripped, escaped, no Hardcover
      cover fallback); verify step 1's export behavior through the draft.
- [ ] Replace this step's CHANGELOG bullets with ONE bullet for the draft endpoint (one-bullet-per-PR rule), and stop editing
      the step 1 bullet: step 3 currently rewrites step 1's hyphenated-ISBN line, which the rule forbids.
- [ ] The PR description says the response write-deadline mechanism (`extendEditionWriteDeadline`, `Unwrap()` in the logger, and
      `multiuser.EditionCreateTimeout`) lives here because a draft makes many paced Hardcover lookups, and that
      `internal/edition/editiontest` includes helpers first used in step 4 (`HoldInsert`, `HoldSearches`, `FailWith`).
- [ ] Some comments keep create wording (`editionWriteDeadline`, `editionRequestIDs`, the `newHeldCreateFixture` test helper name)
      so step 4 stays additive; reword them only if it does not make step 4 non-additive.
- [ ] Crosswalk findings, decide each and either do it in this step or record it as out of scope: (2) use the exact
      `authors[].name` and `narrators[]` arrays from the expanded item instead of splitting `authorName` and `narratorName`
      on commas; (3) do not silently create a non-English item as language 1 (at least warn in the draft when ABS
      `metadata.language` is set and is not English); (4) try ABS `publishedDate` before the year-only fallback; (7) skip
      Audnex for ebook items.
- [ ] Cleanup left by step 1: `BookMismatch.EditionInfo` is dead and should be removed in this step (or a small PR of its
      own straight after step 1 merges), because the draft is the first new caller of `AddWithMetadata` -> `ToEditionExport`.
      Its only production writer is the constant `"Audiobookshelf"` in `mismatch.AddWithMetadata`, which `ToEditionExport`
      discards, and nothing else reads it; since step 1 the export's `edition_information` is decided by the reading format
      and `BookMismatch.Abridged` alone (ebook empty, abridged `Abridged`, else `Unabridged`). Step 1 kept the field so the
      audiobook export stayed unchanged and no existing test expectation had to change. Remove: the field, the "a real
      value on the record wins" branch and the placeholder and debug-text filter in `ToEditionExport`, and the tests that
      only exercise them (the `EditionInfo: "Special Edition"` cases in `mismatch_test.go`, the real-value, placeholder and
      debug-text rows of `TestExportEditionInformation`, and the `"Audiobookshelf"` assertion in `TestAddWithMetadata`).
      The unresolved CodeRabbit thread about making the audiobook placeholder filter case-insensitive disappears with it. Keep `EditionExport.EditionInfo` (`edition_information`): the `edition` tool imports it. The exported files must be
      identical for every record production can build; say so in the PR body and add a CHANGELOG note only if a mismatch
      JSON field disappears from a file users see (check `BookMismatch`'s `edition_information` in the status API first).
- [ ] ISBN checksum flags from step 1: the export carries `isbn_10_valid` / `isbn_13_valid`. Decide what the draft does with a
      `false` one (a warning next to the ISBN field is the smallest change that loses nothing) and expose the flag in the draft;
      check with a real Hardcover token first whether `insert_edition` accepts a bad checksum, since nothing here has run
      against it. The raw `isbn` stays on the record, so the value is never lost.
- [ ] Crosswalk finding 9: an author or narrator name is matched on Hardcover by exact, case-sensitive name (no ordering,
      `canonical_id` ignored, and a narrator needs a prior `Narrator` credit). Decide whether the draft warnings are enough
      or the lookups need improving; a looser query must first be checked against the hosted API (see `AGENTS.md`).

**Step 4**
- [ ] Crosswalk scope ([section 5, Step 4](needs-review-edition-field-crosswalk.md#step-4-create-endpoint)): the editable
      set is exactly `EditionEdits` (any other field, including `reading_format`, `book_id` and `image_url`, is 400); test
      R1 (run record only), R12 (from the item fetched at create time), R2 (trim, blank is 422), R4-R6 (ASIN trim, ISBN
      normalization, wrong shape 422, an identifier required, an item without one 409), R7 and R9 (at most 50, positive),
      R10, R13, R15, R16 (not negative), R11 (at most 100), and R17 (URL rebuilt server-side, the first production caller of
      `SetAudiobookshelfBaseURL`, a cover failure is a warning with a 200); verify R3, R8 and R14 reach the creator unchanged.
      State finding 8 in the PR's testing notes.
- [ ] Its CHANGELOG diff must be ONE new bullet for the create endpoint (one-bullet-per-PR rule) and must not touch earlier
      steps' bullets; today it replaces step 3's entry and reshuffles steps 1-2's lines, so redo that hunk. This means step 4's
      tree will no longer equal the old combined branch's tree in `CHANGELOG.md`, which is intended.
- [ ] ISBN checksum policy on create: the endpoint validates only the shape and length of `isbn_10` / `isbn_13`. Decide whether a
      bad checksum is accepted (Hardcover flags it `..._valid = false`), warned about, or rejected with a 422, using what step 3
      learned about the real API, and test that one policy at the HTTP boundary. The maintainer of #195 recommended gating on the
      checksum; step 1 deliberately did not, so that this decision could be made here.
- [ ] The PR description says `CreateEditionFromRunBook` has no caller until the handler commit (consider squashing those two
      commits).
- [ ] Re-check CodeRabbit item F2 (the shutdown drain of in-flight creates) against this step's code.

**Step 5** (built; needs work before its fork PR)
- [ ] Crosswalk scope ([section 5, Step 5](needs-review-edition-field-crosswalk.md#step-5-sync-identifier-matching)): the
      sync must find every edition shape the crosswalk can create, so add one matching test each for: ASIN only; ISBN-10
      only; ISBN-13 only; both ISBNs; an ISBN-13 with no derivable ISBN-10; an ebook (R4-R6, R12). The created edition can
      hold just one ISBN form, so matching must not depend on both. This includes the ASIN reading-format test below.
- [ ] Rebase `step_5_needs_review_add_edition` onto `step_1_needs_review_add_edition`; expect conflicts with #190 in
      `internal/sync/service.go` and `internal/api/hardcover/client.go`; re-check the "unchanged by decision" claims (the
      reading-format filters) against the current code; re-run all gates.
- [ ] Add a test that the ASIN query keeps its reading-format filter (currently only the ISBN queries are guarded).
- [ ] Keep step 5's CHANGELOG to ONE bullet (it has one today) and tighten its sentence about bad-checksum ISBNs.
- [ ] `mismatch.AddWithMetadata`'s own enrichment searches do not try the converted ISBN form; decide whether to include it or
      leave it out of scope, and note the decision.

**Step 6** (not started; concurrency-sensitive)
- [ ] Crosswalk scope ([section 5, Step 6](needs-review-edition-field-crosswalk.md#step-6-immediate-read-status-resync)):
      nothing new is delivered; verify that after a create the resync's own lookup finds the new edition for each identifier
      shape and both formats (R4-R6, R12), on the same Hardcover book as the run record's candidate (R1), and that a dry run
      (`edition_id: 0`) attempts no resync.
- [ ] All items in the plan sections "Backend 3-4" and the Step 6 tests: `Service.SyncBook`, `bookOps` exclusivity, opt-in
      `resync`, dry-run skip, `-race` tests, and the regression test that `StartSync` is unaffected when no book operation runs.

**Step 7** (not started)
- [ ] Crosswalk scope ([section 5, Step 7](needs-review-edition-field-crosswalk.md#step-7-ui)): editable inputs are exactly
      R2 title, R3 subtitle, R4 ASIN, R5 ISBN-13, R6 ISBN-10, R8 release date, R14 edition information; read-only are R7, R9,
      R10 (resolved names), R11, R12, R13 and R17; an ebook hides R9 and R13 and shows R12; `author_ids`, `narrator_ids`,
      `publisher_id`, `language_id`, `country_id`, `audio_seconds` and `edition_format` are echoed from the draft unedited;
      every draft warning and the 409 and 422 messages are shown; every ABS-supplied string is escaped. Findings 9 and, if
      the language is shown, 3.
- [ ] All items in the "Frontend" section and its tests, plus: show the draft's `reading_format` and hide narrator and
      duration fields for an ebook; render the 409 (no identifier, existing edition not confirmed) and 422 messages; the
      user-facing README note and the Known limitation text.
- [ ] Verify in the browser pane (button only on eligible needs_review records, modal, resync result, error and dry-run paths,
      the modal surviving a status poll).
- [ ] Crosswalk finding 9: when no Hardcover author matches, create fails (an author is required) and the preview has no way
      to supply one. Decide between letting the user enter or search a Hardcover author, or showing a clear "cannot create
      an edition for this book" state instead of an enabled Create button.

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

1. ISBN and export foundations: Add shared ISBN normalization and reading-format helpers, and fix the mismatch export (hyphenated ISBNs are kept, no default publisher, ebook items export as ebook editions, an abridged audiobook exports as Abridged).

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
now seven steps. Nothing is user-visible until step 7, so no half-finished button ships. Which parts of the
Audiobookshelf-to-Hardcover field mapping each step delivers or must verify is in "Crosswalk scope by step" near the top.

- **Step 1 - ISBN and export foundations** (`step_1_needs_review_add_edition`, from `develop`). The `internal/isbn`
  package, `models.ReadingFormat`/`ReadingFormatID` and the reading-format context helper (with
  `hardcover.WithReadingFormat` delegating to it), and the mismatch export fixes: the unresolved-publisher default
  (1 -> 0), the hyphenated-ISBN split, publisher ID capture, ebook `reading_format` in `BookMismatch`/`EditionExport`
  (ebook label, no `Unabridged`, no audio length), an abridged audiobook exporting `Abridged` (ABS `metadata.abridged` is decoded), and removal of the unused `ToEditionInput`. It also swaps the
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
  A read-only endpoint that works on its own and is exercisable with curl. The field-by-field mapping from the
  Audiobookshelf item to the Hardcover edition is in
  [needs-review-edition-field-crosswalk.md](needs-review-edition-field-crosswalk.md).
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

## How the combined branch was split (done)

The combined branch `feature/edition-from-needs-review-api` held steps 1-4. Its commits were interleaved fixes and cleanups, so each step's branch was built from the previous
one by taking that step's files and hunks, as a few clean commits, then gated: `gofmt`, build, vet, `make test`, lint on
the Go 1.26.7 toolchain, the JS tests, and every commit builds. An independent validator confirmed all four, that no
step contains code from a later one, and that `step_4` has **no diff at all** against the old combined branch, so nothing
was lost.

What happened to the old branch and PR:

- The old combined branch was renamed with GitHub's branch-rename API and then deleted, locally and on `origin`.
  **The rename closed fork PR #20** (it did not follow the new name, contrary to what this plan had assumed). The owner
  chose to leave #20 closed. Lesson: do not rename the head branch of an open PR expecting it to survive; a closed PR keeps
  its comments, and the CodeRabbit threads on it are re-checked in whichever step's fork PR contains that code.
- Fork PRs for steps 2, 3 and 4 are opened only when the owner asks; each is based on the previous step's
  branch. Upstream PRs are created only on the owner's command, one at a time as described under "Stacking and merge
  order": step 1 first; then 2, 3, 4 each after the previous has merged, and step 5 after step 1. Each is rebased onto the
  current upstream `develop` first and carries the "Multi-Step Project" block.

Where the files actually went (differs slightly from the first plan):

- **Step 1:** `internal/isbn/`, `internal/models/reading_format*.go` (helpers and the `AudiobookshelfBook.ReadingFormat`
  method), `internal/mismatch/` (hyphenated ISBN, publisher default and captured ID, ebook export, removal of the unused
  `ToEditionInput`), the reading-format delegation in `internal/api/hardcover/client.go`, the `book.ReadingFormat()` calls
  in `internal/sync/service.go`, and its CHANGELOG bullet (to be collapsed to one, see the checklist).
- **Step 2:** `internal/edition/creator.go` and its tests, `GetEditionByISBN10` in `internal/api/hardcover/client.go`,
  `cmd/edition/README.md`, and its CHANGELOG bullet (to be collapsed to one, see the checklist).
- **Step 3:** `internal/api/audiobookshelf/client.go` (+ test), `internal/edition/draft/`, `internal/edition/editiontest/`
  (the whole shared-fakes package), the draft half of `internal/multiuser/edition.go` and `service.go`
  (`newHardcoverClient`, `admissionErrorLocked`, `checkEditionAdmission`), `GetEditionDraft` and its route, and **the
  response write-deadline mechanism** (`extendEditionWriteDeadline`, `editionWriteDeadline`, `Unwrap()` in
  `internal/logger/logger.go`, and `multiuser.EditionCreateTimeout`), because a draft makes many paced Hardcover lookups and
  needs the extended deadline too. The draft docs and its CHANGELOG bullet.
- **Step 4:** the create half of `internal/multiuser/edition.go` and `service.go` (validation, in-flight guard, edition wait
  group and `Shutdown` drain), the POST route and `CreateEdition` handler, the create, cover and shutdown tests, and the
  create docs. It restores the docs wording that steps 1-3 had narrowed. Its CHANGELOG diff currently also reshuffles earlier steps'
  lines, which the one-bullet-per-PR rule forbids; that is a checklist item to redo.

The follow-ups found while splitting are tracked as checkboxes in "Step checklists" near the top of this document, under the
step they belong to.

Each remaining step is started only on the owner's go-ahead, and nothing is pushed or opened without permission.

## Implementation notes for the original combined Slice 1 (now steps 1-4)

These notes were recorded when steps 1-4 were still one slice ("Slice 1"), implemented and validated, then extended. The
feature-to-step mapping is in "How the combined branch was split". The combined branch is deleted and its content lives in
steps 1-4; fork PR #20, its head, is closed. Where this differs from the plan above, this section is what shipped.

**Cleanup done after the ebook work** (a cleanup loop over the branch): the ebook rule and Hardcover
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
- **Cover URL hardening.** `draft.CoverURL` strips credentials, query and fragment from the profile's
  Audiobookshelf base URL before building the cover URL.
- **`newHardcoverClient` refactor.** The extracted helper's debug log no longer carries `profile_id`.
- **Edition format fix.** `Creator.createEdition` previously hardcoded `edition_format: "Audiobook"` and
  ignored `EditionInput.EditionFormat`. It now sends the trimmed requested format and falls back to "Audiobook" when
  empty; `reading_format_id` stays 2. This also changes the behavior of the `edition` CLI. The Hardcover schema shows
  `BookDtoInput.edition_format` is a free-text String.
- **Cover failure signal.** Cover upload failures were previously swallowed. `EditionResult` now has an
  additive `ImageError` (`image_error,omitempty`, a fixed step label only), and the POST response carries a
  `warnings` array (see Backend 6). The status is still 200 when only the cover failed.
- **Test seam.** An unexported `newEditionCreator` seam on `MultiUserService` allows service-level tests
  proving the ABS token goes only to the ABS cover host, and that removing the service's `hcClient.SetDryRun` makes
  the dry-run test fail.
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

Grounded in the code on the branch at that time; the ebook support that followed is in "Ebook items".

- **Cross-book guard.** `edition.Creator` refuses to adopt an existing edition that belongs to another book or whose
  book cannot be confirmed (`ErrEditionBelongsToOtherBook`); the API answers 409 with a fixed message ("An edition with
  this ASIN or ISBN already exists on Hardcover and could not be confirmed to belong to this book."). A reused
  same-book edition is returned untouched (no cover upload, no mutation), and `EditionResult.Existing` records this
  (the `edition` CLI prints `"existing": true`).
- **Shutdown.** A dedicated edition wait group means `Shutdown` first cancels running syncs and then drains in-flight
  creates; a draft holds no admission gate (it only refuses to start once shutdown or profile deletion has
  begun). Profile deletion still waits for an in-flight create (documented tradeoff).
- **Response write deadline.** The two routes use `audiobookshelf.RequestTimeout` (30s) + `EditionCreateTimeout` (2m) +
  15s, which required adding `Unwrap()` to the logger middleware's response writer.
- **Small cleanups.** Blank titles are rejected (422); `Draft.ToInput` was removed as unused.
- **ISBN helpers.** New leaf package `internal/isbn`: `Normalize`, and `Parse` with 978 <-> ISBN-10
  conversion; the counterpart form is derived only when the input checksum is valid, and a 979 ISBN-13 has no ISBN-10.
- **Hyphenated-ISBN fix.** The old length-only ISBN split in `mismatch.AddWithMetadata` dropped hyphenated
  ISBNs from the mismatch export and the draft. It now uses `internal/isbn`, and the draft also fills the derived
  counterpart form.
- **Identifier requirement.** A book whose Audiobookshelf item has neither an ASIN nor a parseable
  ISBN gets 409 (`ErrEditionNoIdentifier`, fixed message) on both the draft and the create route, before any Hardcover
  call. A create request needs at least one of `asin`, `isbn_10`, `isbn_13` (422), and its ISBNs are normalized and
  shape-checked. Such books can still be `needs_review` through the title/author match but can never be auto-matched,
  so an edition created for them could never help a sync.
- **Proactive duplicate detection.** Before inserting, the creator looks up an existing edition by ASIN, then
  ISBN-13, then ISBN-10, then the derived counterpart forms (audiobook format only; up to three extra reads; skipped in
  dry run). The "already exists" insert-error fallback remains as a safety net. `GetEditionByISBN10` was added to
  `hardcover.Client`.
- **Format and publisher.** `edition_format` is honored by the creator (trimmed, fallback "Audiobook", at most 100
  characters else 422), which also affects the `edition` CLI. The mismatch export's unresolved publisher is 0 instead of
  1, and a publisher resolved during export is exported.
- **Response shape.** The create response is `{edition_id, dry_run, warnings}`; `warnings` carries one fixed message if
  the cover could not be uploaded (`EditionResult.ImageError` internally).

### Review of fork PR #20 (CodeRabbit)

- **F1**, publisher ID stale in `ToEditionExport`: fixed.
- **F2**, the shutdown wait: fixed (see Shutdown above).
- **F3**, the pre-existing CLI-only `CheckRedirect` in `edition.NewCreator` copying `Authorization` to redirects:
  skipped for this PR as pre-existing; a candidate for a separate small PR.
- **F4**, docstring-coverage check: skipped (no repo rule; lint is clean).

The review threads were not yet replied to or resolved when this was written, and PR #20 has since been closed (see "How the
combined branch was split"). The threads are re-checked in whichever step's fork PR contains the code they refer to: F1 in step 1
(mismatch export), F2 in step 4 (shutdown), F3 in step 2 (creator/CLI).

## Step 5 implementation notes

Branch `step_5_needs_review_add_edition`, formerly `feature/sync-identifier-matching`. It is about 500 added lines, of which about 335 are tests. **It has not been
rebased onto the current combined branch, and the plan now stacks it on step 1 only** (it needs `internal/isbn`), so
its rebase target is step 1's branch (and current `develop`, which has #190). #190 (ebook detection and matching) changed
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
- **Removed: `order_by: { id: asc }`.** A deterministic-ordering addition was tried and removed because it could
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
  ISBNs could be tighter; `mismatch.AddWithMetadata`'s own enrichment
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
15. **Crosswalk rows are assigned to steps.** Each step delivers some rows of the field crosswalk and verifies others, and the
    assignment is a table near the top of this document, repeated as one checklist item per step. Row IDs (R1 to R18) never
    change, so PR descriptions and checklists can cite them.

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

The Audiobookshelf-to-Hardcover field mapping that this reuse produces is tabulated in
[needs-review-edition-field-crosswalk.md](needs-review-edition-field-crosswalk.md), with the transformation for each
field and the gaps found while writing it.

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

Every step's tests also cover the crosswalk rows that step delivers, with the edge cases listed for that step in the
crosswalk's [section 5](needs-review-edition-field-crosswalk.md#5-what-each-step-must-deliver-from-the-crosswalk), and add a
test for each row it only verifies. The groups below are the pre-existing test plan; where a crosswalk edge case is not
covered by a group, the crosswalk item is added to that step's checklist and tested there.

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
  ISBN matching, hyphen handling); `docs/openapi.yaml` for both operations. `CHANGELOG.md` gets exactly ONE bullet per step (see
  the one-bullet-per-PR rule in "Step checklists"); the earlier plan of several Added/Changed/Fixed lines for these steps is
  replaced by that.
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
