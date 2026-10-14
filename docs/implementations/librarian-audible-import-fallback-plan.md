# Automatic Audible import fallback for librarians

Status: agreed implementation plan; not implemented.

## Summary

Try `upsert_book` first. For profiles with verified mapping-write permission,
automatically fall back to matching an existing edition or creating an audiobook
edition, then adding its regional Audible mapping.

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
4. Reuse one unambiguous audiobook edition. **If ASIN and ISBN match separate
   editions, fail.** Also fail for multiple distinct candidates, wrong-format
   candidates, or disagreement with a confirmed target book. Do not present
   edition options.
5. If no edition matches, create an audiobook edition from reviewed ABS metadata
   under the confirmed Hardcover book. If no confirmed book exists, fail; do not
   create a book.
6. Add the regional Audible identifier through `insert_book_mapping`. Reread the
   mapping and edition before saving the association and resyncing progress.

Lookup errors never count as "no match." An existing mapping to the intended
edition is a no-op; a mapping pointing elsewhere is a conflict.

## Permissions, recovery, and interfaces

- Add an `audible_mapping` capability to the existing capability response using
  a validation-only probe. Require affirmative permission evidence and reuse
  token-specific caching and refresh.
- Enable fallback automatically through **Add edition** for mapping-capable
  profiles and explain it in the preview. Other profiles retain the existing
  workflow.
- For unknown import outcomes, search existing editions but defer fallback
  writes until the five-minute recovery window ends and a fresh status/mapping
  check runs. Still-fetching imports remain pending.
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
UI.

Cover permission denial/unverified results, mapping no-op/conflict, lookup
failures, delayed imports, partial success, restart recovery, and dry run.
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
