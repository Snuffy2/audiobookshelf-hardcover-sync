# Automatic Audible import fallback for librarians

Status: agreed implementation plan; not implemented.

## Summary

Try `upsert_book` first. For profiles with verified mapping-write permission,
automatically fall back, within the same user-initiated edition action, to
matching an existing edition or creating an audiobook edition, then adding its
regional Audible mapping. Sync itself never writes to the catalogue.

Success requires consistent book/edition identity, audiobook format, and the
exact Audible mapping. Wrong-format or ambiguous results fail without offering
edition choices.

## Import flow

1. Submit the existing regional `upsert_book` request. If its result verifies as
   an audiobook edition with the expected `ASIN:region` mapping, finish normally.
2. If the returned edition has the wrong format or conflicting identity, stop
   and report it.
3. Otherwise, for mapping-capable profiles, search exact `editions.asin` and
   valid ABS ISBN identifiers, including equivalent ISBN-10/13 forms. Use a
   dedicated lookup across formats; leave ordinary sync matching unchanged.
   When `upsert_book` returns an edition whose mapping is missing
   (`RegionalAudiobookMappingMissingError`), use its book as a match but not its
   edition: the edition is often the book's first edition rather than a real
   match. The returned edition is neither a reuse candidate nor a conflict; only
   the identifier lookup selects the edition.
4. Reuse one unambiguous audiobook edition. **If ASIN and ISBN match separate
   editions, fail.** Also fail for multiple distinct candidates, wrong-format
   ASIN candidates, or disagreement with the confirmed book. Do not present
   edition options.
   - An ISBN-only match on a non-audiobook edition is not a reuse candidate.
     ABS audiobook ISBNs are often print ISBNs. Treat its book as identity
     evidence: it must agree with the confirmed book, or supply it when none is
     otherwise confirmed. Fail if it disagrees.
5. If no edition matches, create an audiobook edition from reviewed ABS metadata
   under the confirmed Hardcover book. If no confirmed book exists, fail; do not
   create a book. Omit any ISBN already owned by another edition, because
   Hardcover ISBNs are unique. Rerun the ASIN/ISBN lookup immediately before
   `insert_edition` so a concurrent import by another profile is reused, not
   duplicated.
6. Add the regional Audible identifier through `insert_book_mapping`. Reread the
   mapping and edition before saving the association and resyncing progress.

The confirmed book comes from any of: the anchored record's `HardcoverBookID`;
the book returned by `upsert_book` (including mapping-missing results); or a
book established by the step 3 identifier lookup. Every available source must
agree; any disagreement is an identity conflict and fails. Title/author
anchoring (`unanchoredImportAnchorBook`) treats search errors as "no anchor",
so it does not confirm a book for fallback writes unless it is changed to
report lookup errors distinctly.

Lookup errors never count as "no match." An existing mapping to the intended
edition is a no-op; the exact `ASIN:region` mapped to a different edition is a
conflict. Mappings Hardcover adds for sibling regions are not conflicts.

Mappings can appear late. If the reread after `insert_book_mapping` does not
yet show the mapping, record the outcome as unconfirmed, keeping the mutation's
returned mapping ID, and recover through a later status check. Never resubmit
the mapping write automatically.

## Permissions, recovery, and interfaces

- Add an `audible_mapping` capability to the existing capability response using
  a validation-only probe. Require affirmative permission evidence and reuse
  token-specific caching and refresh. Only `allowed` enables fallback; the
  existing `unverified` state (which permits ordinary attempts) does not. The
  probe needs its own `write:catalog:map` denial classification alongside the
  existing `write:catalog:append` check.
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
- Revalidate profile access, current capability, source identity, and target
  identity before writes. Preserve concurrency guards, rate limiting, retries,
  and concrete-client dry-run protection.
- Create editions with audiobook format and available ABS metadata. Add Audible
  identity through the regional mapping, without writing it into
  `editions.asin`.

## Validation

Cover successful import, missing mapping, ASIN/ISBN reuse, ASIN and ISBN matching
separate editions, multiple candidates, wrong format, missing target book, and
edition creation. Verify conflicts fail without writes or an edition-selection
UI. Cover a mapping-missing upsert whose returned edition differs from the
lookup result (the lookup wins), an ISBN match on a print edition (book
evidence, ISBN omitted from the new edition), and reuse-only fallback without
`insert_edition` permission.

Cover permission denial/unverified results, mapping no-op/conflict (including
sibling-region mappings), lookup failures, a mapping not yet visible after
`insert_book_mapping`, delayed imports, partial success, restart recovery, and
dry run.
Confirm retries do not repeat confirmed creation and failures do not save
associations.

Run focused Go/UI tests, then `make test`, `make lint`, and `make build`. Update
README, OpenAPI, and one Unreleased changelog entry when implementing the feature.

## Defaults and limits

- Initial scope is the web/API edition workflow; ordinary sync matching and CLI
  behavior remain unchanged.
- Token capability determines fallback availability. The mapping operation
  requires `write:catalog:map`, which the
  [Hardcover PAT documentation](https://docs.hardcover.app/api/pat-link-builder/)
  identifies as requiring the librarian role.
- Wrong-format results fail for everyone.
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
