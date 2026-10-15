# Automatic Audible import fallback for librarians

Status: agreed implementation plan; not implemented.

## Summary

Try `upsert_book` first. For profiles with verified mapping-write permission,
automatically fall back, within the same user-initiated edition action, to
matching an existing edition or creating an audiobook edition, then adding its
regional Audible mapping. Sync itself never writes to the catalogue.

Success requires consistent book/edition identity, audiobook format, and the
exact Audible mapping. Throughout, an Audible mapping means a `book_mappings`
row whose external ID equals the requested `ASIN:region` exactly. For
librarians, an audiobook edition with that mapping is a final match with no
catalogue writes. An audiobook edition matched by exact `editions.asin` or ISBN
is reused, and the mapping is added to it. Ambiguous results, and wrong-format
editions identified by an Audible mapping, exact `editions.asin`, or ISBN, fail
without offering edition choices.

## Import flow

1. Submit the existing regional `upsert_book` request. If its result verifies as
   an audiobook edition with the expected `ASIN:region` mapping, finish normally.
2. For mapping-capable profiles, the returned edition is a match only if its
   `book_mappings` contain the exact requested `ASIN:region`; an audiobook
   edition that does is the match and the import is done. An edition without
   it is not a match, whatever its format: it is often just the book's first edition. Do
   not reuse it or treat it as a conflict; use only its book and continue to
   step 3. Stop and report when the returned book conflicts with the anchored
   book, or when an edition that carries the exact mapping is not an audiobook.
   Other profiles keep the existing behavior: wrong-format or conflicting
   results stop.
3. For mapping-capable profiles, look up editions whose `book_mappings`
   contain the exact requested `ASIN:region`. An audiobook edition found this
   way is the match and the import is done: save the association with no
   catalogue writes. Otherwise, look up editions by exact `editions.asin`, by
   valid ABS ISBN identifiers, including equivalent ISBN-10/13 forms, and by
   `book_mappings` carrying the same ASIN under a different region. Use a
   dedicated lookup across formats; leave ordinary sync matching unchanged.
4. Reuse one unambiguous audiobook edition. An audiobook edition matched by
   `editions.asin`, or by ISBN alone, is the match; step 6 adds the requested
   `ASIN:region` to that edition's `book_mappings`. **If ASIN and ISBN match
   separate editions, fail.** Also fail for multiple distinct candidates,
   wrong-format ASIN or ISBN candidates, or disagreement with the confirmed
   book. Do not present edition options. If an edition carries the ASIN only
   under a different region, stop and report it when there is no reuse
   candidate or when it differs from the reuse candidate; never create a new
   edition alongside it. An ISBN-only match on a non-audiobook edition stops
   the action, even when its book agrees with the confirmed book. Do not use it
   only as book evidence, discard its ISBN, or continue to edition creation.
5. If no edition matches, create an audiobook edition from reviewed ABS metadata
   under the confirmed Hardcover book. If no confirmed book exists, fail; do not
   create a book. Do not omit an ISBN to bypass an existing wrong-format or
   conflicting edition. Rerun the ASIN/ISBN lookup immediately before
   `insert_edition` so a concurrent import by another profile is reused, not
   duplicated.
6. Add the regional Audible identifier through `insert_book_mapping`. Reread the
   mapping and edition before saving the association and resyncing progress.

The confirmed book comes from any of: the anchored record's `HardcoverBookID`;
the book returned by `upsert_book` (even when its edition is not a match); or a
book established by the step 3 identifier lookup. Every available source must
agree; any disagreement is an identity conflict and fails. Title/author
anchoring (`unanchoredImportAnchorBook`) treats search errors as "no anchor",
so it does not confirm a book for fallback writes unless it is changed to
report lookup errors distinctly.

Lookup errors never count as "no match." An existing mapping to the intended
edition is a no-op; the exact `ASIN:region` mapped to a different edition is a
conflict.

Mappings can appear late. If the reread after `insert_book_mapping` does not
yet show the mapping, record the outcome as unconfirmed, keeping the mutation's
returned mapping ID when available, and recover through a later read-only
mapping/edition check against the persisted fallback target. Never resubmit the
mapping write automatically.

## Permissions, recovery, and interfaces

- Add an `audible_mapping` capability to the existing capability response using
  a validation-only probe. Require affirmative permission evidence and reuse
  token-specific caching and refresh. Only `allowed` enables fallback; the
  existing `unverified` state (which permits ordinary attempts) does not. The
  probe needs its own `write:catalog:map` denial classification alongside the
  existing `write:catalog:append` check.
- The mapping probe runs with the existing `insert_edition` and `upsert_book`
  probes when a sync run summary is opened in the web UI
  (`GET /api/profiles/{id}/edition-capability`). It is not a sync-start or
  per-book check; sync never writes to the catalogue. The result is cached per
  profile and token fingerprint and cleared by token or profile changes and
  the existing refresh endpoint. The UI chooses the fallback or existing
  pathway from that result.
- Fallback actions read the cached capability before each write instead of
  probing again. If a real `insert_book_mapping` or `insert_edition` call
  returns a scope denial, treat it as authoritative: fail that action without
  saving an association, and invalidate the profile's cached capability so
  the next summary probes again.
- Edition creation (step 5) additionally requires the existing `insert_edition`
  capability to be `allowed`. A profile with mapping but not insertion permission
  gets reuse-only fallback and fails when no edition matches.
- Enable fallback automatically through **Add edition** and **Import Audible
  edition** for mapping-capable profiles and explain it in the preview. Other
  profiles retain the existing workflow.
- For unknown import outcomes, search existing editions but defer fallback
  writes until the five-minute recovery window ends and a fresh status/mapping
  check runs. Still-fetching imports remain pending. Tell the user in the
  preview/error that they must return and continue after the window.
- Keep `check-import` read-only. Add
  `POST /api/profiles/{id}/edition-drafts/continue-librarian-import`, using signed
  recovery data to resume eligible fallback stages.
- Persist fallback stage, confirmed identifier, submission time, target IDs,
  and source snapshot in the existing action record. After partial success,
  resume verification or mapping for the created edition rather than creating
  another.
- Before sending `insert_edition`, durably record `creation_outcome_unknown`
  for the profile/item and regional identifier. If that record cannot be saved,
  do not send the write. Use the existing single-attempt mutation boundary;
  never automatically replay an insertion after a transport failure. Persist
  the returned edition ID before proceeding to mapping. A timeout, lost response,
  process interruption, or failure to persist that ID leaves creation unknown,
  even if the request may have succeeded remotely.
- Within the profile, an unknown creation blocks another insertion for the same
  item or regional identifier, including continuation requests and later runs. Read-only
  recovery may resolve it through an exact Audible mapping to a freshly verified
  audiobook edition on the confirmed book. An absent mapping or an empty
  ASIN/ISBN lookup does not prove that creation failed: the new edition may have
  neither identifier. If no verifiable identity appears, report that manual
  Hardcover investigation is required and keep insertion blocked. Only a definite
  pre-execution failure permits another creation attempt; elapsed time alone does
  not clear the unknown state.
- Make `check-import` recovery stage-aware. Before fallback begins, retain the
  existing import-status verification. Unknown fallback creation uses the
  read-only mapping recovery rule above. Once a fallback edition is
  persisted, verify its exact regional mapping and freshly read edition against
  that target, the confirmed book, and the original source identity. The old
  `upsert_book` status may still name an ignored edition, lack an edition ID, or
  report an old failed/pending state; it must not reject an otherwise verified
  fallback mapping or cause another upsert. Preserve book-identity conflict
  checks. Missing mappings remain unconfirmed, and mappings to a different target
  or wrong-format editions fail. Known created editions with no mapping resume
  only the mapping stage through the write continuation endpoint.
- Revalidate profile access, cached capability, source identity, and target
  identity before writes. Preserve concurrency guards, rate limiting, retries,
  and concrete-client dry-run protection.
- Create editions with audiobook format and available ABS metadata. Add Audible
  identity through the regional mapping, without writing it into
  `editions.asin`.

## Validation

Cover successful import, missing mapping, ASIN/ISBN reuse, ASIN and ISBN matching
separate editions, multiple candidates, wrong format, missing target book, and
edition creation. Verify conflicts fail without writes or an edition-selection
UI. Cover a returned edition without the exact `ASIN:region` mapping (including
one mapped only under another region), both audiobook and print (book used,
edition ignored, fallback continues); an edition carrying the ASIN only under
another region with no reuse candidate, or on a different edition from the
reuse candidate (stops without writes); a returned non-audiobook
edition that carries the mapping (fails); an edition found by `book_mappings`
(done, no writes); an `editions.asin` match without the mapping (reused,
mapping added); an ISBN-only match on an audiobook edition without the mapping
(reused, mapping added); wrong-format ASIN and ISBN matches (including an
ISBN-only print match on the confirmed book), which fail without fallback
creation or mapping writes; and reuse-only fallback without
`insert_edition` permission.

Cover permission denial/unverified results, a write-time scope denial after a
cached `allowed` result (action fails, cache invalidated), mapping no-op/conflict, lookup
failures, a mapping not yet visible after
`insert_book_mapping`, delayed imports, partial success, restart recovery, and
dry run.
Confirm retries do not repeat confirmed creation and failures do not save
associations.

Cover `insert_edition` succeeding remotely with its response lost, interruption
before the returned ID is persisted, and inability to save the pre-write record.
Include a new edition with neither ASIN nor ISBN: restarts, continuation, and a
new run must not insert again while its outcome is unknown, even after an empty
lookup or the five-minute import window. Verify that an exact late mapping can
resolve that state through reads.

Cover a persisted fallback target E2 while the original upsert status still
names E1, omits its edition ID, or reports a failed/pending state. A verified
mapping to audiobook E2 on the confirmed book must recover successfully without
another catalogue write. A different mapped edition, wrong format, or conflicting
book must still fail.

Run focused Go/UI tests, then `make test`, `make lint`, and `make build` for
each step. Each step's PR updates README and OpenAPI for the behavior it ships
and adds its own single Unreleased changelog bullet.

## Delivery

Deliver the feature as four PRs, each targeting `develop`. Each step must work
and be safe on its own: a merged step never leaves a path that writes to the
catalogue without the safeguards that step's behavior requires. Later steps
build on earlier ones but do not change their contracts. Validate real mapping
behavior with a mapping-capable token before step 2 merges, since it is the
first step that writes mappings.

1. **Hardcover client groundwork.** Add the `insert_book_mapping` client method
   behind the concrete-client dry-run boundary, with single-attempt mutation
   budgeting and `write:catalog:map` denial classification. Add the
   `audible_mapping` capability probe and response field (only `allowed`
   counts). Add the read-only fallback lookup: exact `ASIN:region` mapping,
   same ASIN under another region, exact `editions.asin`, and ISBN-10/13. No
   edition workflow uses these yet; the only visible change is the capability
   field.
2. **Reuse-only fallback.** For mapping-capable profiles, implement import-flow
   steps 1–4 and 6: use the returned book but not its edition, finish on an
   exact mapping match with no writes, and reuse `editions.asin` or ISBN
   audiobook matches by adding the mapping. Apply every conflict and stop rule,
   including other-region mappings and print ISBN matches. When no edition
   matches, stop and report; there is no creation yet. Persist the mapping
   stage in the action record. A mapping not yet visible after the write is
   reported as unconfirmed without saving the association, and the user is
   told to sync again later. Unknown `upsert_book` outcomes keep today's
   behavior and do not enter fallback. Update the preview and errors for
   **Add edition** and **Import Audible edition**.
3. **Mapping-stage recovery.** Make `check-import` stage-aware for the mapping
   stage: verify the persisted fallback target's exact mapping and fresh
   edition, and ignore the edition named by the old upsert status. Add
   `continue-librarian-import` to resume the mapping stage for a persisted
   target and to run deferred fallback for unknown `upsert_book` outcomes after
   the five-minute window and a fresh status/mapping check. Never resubmit a
   mapping write automatically.
4. **Edition creation.** Implement import-flow step 5: require `insert_edition`
   capability, rerun the lookup before the write, record
   `creation_outcome_unknown` before sending `insert_edition`, block another
   insertion while the outcome is unknown, and persist the returned edition ID
   before mapping. Extend `check-import` and `continue-librarian-import` to the
   creation stage. Profiles without `insert_edition` permission keep step 2's
   reuse-only behavior.

Each step carries the validation cases for the behavior it ships.

## Defaults and limits

- Initial scope is the web/API edition workflow; ordinary sync matching and CLI
  behavior remain unchanged.
- Token capability determines fallback availability. The mapping operation
  requires `write:catalog:map`, which the
  [Hardcover PAT documentation](https://docs.hardcover.app/api/pat-link-builder/)
  identifies as requiring the librarian role.
- Wrong-format editions identified by an Audible mapping, exact `editions.asin`,
  or ISBN fail. For other profiles, any wrong-format upsert result still fails.
- No book creation, edition-choice UI, automatic format correction, or
  cover-upload changes.
- Validate real mapping behavior with a mapping-capable token before deployment;
  the supplied normal-user write token lacks `write:catalog:map`.

## Investigation evidence

Discovery's retained logs examined on October 14, 2026 contained 61 import
timeout events and three wrong-format import failures. The logs did not expose
raw import/status responses sufficient to distinguish empty results, pending
imports, and completed imports with missing mappings.

Validation-only probes with the supplied normal-user write token passed scope
checks for `insert_edition` and `upsert_book`, reaching their expected missing
required-argument errors. The `insert_book_mapping` probe returned HTTP 403 with
`Missing scopes: write:catalog:map`. Required inputs were omitted, so no catalog
writes executed. This establishes the tested token's permission boundary, not
successful librarian mapping behavior.
