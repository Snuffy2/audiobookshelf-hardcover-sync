# Hardcover Audible Identifier and Book-Matching Findings

**Investigation date:** 2026-09-23
**Document type:** Factual investigation record

This document records observations from repository inspection, live Hardcover
GraphQL queries and mutations, inspection of Hardcover's production web client,
Audnex lookups, and manual tests in the Hardcover web interface. It does not
define an implementation plan or make product decisions.

All book names, author names, edition and book IDs, ASINs, credentials, and
credential locations used during the investigation have been omitted. The case
labels below are intentionally generic.

## Terminology

- **Bare ASIN**: an ASIN without a marketplace suffix.
- **Regional external ID**: an Audible identifier in Hardcover's
  `<ASIN>:<region>` form.
- **Edition ASIN**: the value stored in Hardcover's `editions.asin` field.
- **Audible mapping**: a `book_mappings` row whose platform is Audible and whose
  `external_id` is regional.
- **Loaded result**: an `upsert_book` import status that resolved an existing
  Hardcover edition rather than creating one.

## 1. Audiobookshelf's identifier has no region

Audiobookshelf stores the ASIN as a plain string. The application model and the
checked-in Audiobookshelf API schema do not associate a marketplace or country
with that value.

Consequently, a sync begins with less information than Hardcover stores in an
Audible mapping. A bare Audiobookshelf ASIN cannot by itself distinguish between
regional Audible catalogues.

## 2. Hardcover has two distinct ASIN-bearing surfaces

Hardcover can expose an ASIN in two places with different data models:

1. `editions.asin` is a single field on an edition.
2. `book_mappings.external_id` is an external-platform identifier. Audible
   mappings use a regional `<ASIN>:<region>` value and point to both a book and
   an edition.

An exact `book_mappings` lookup returns the associated `book_id` and
`edition_id` when that regional mapping exists. The same lookup returns no row
when that exact regional mapping is absent, even when another region's Audible
mapping points to the correct edition.

The investigation also found that multiple regional Audible mappings can point
to one edition. The base ASIN can differ between marketplaces, so regional
variants cannot be derived by merely changing the suffix on one ASIN.

## 3. `editions.asin` is not a reliable Audible mapping, but contains useful legacy data

The Edition input model describes `asin` as an Amazon retail identifier, while
Audible identifiers have a dedicated external-mapping representation. In
practice, Hardcover's crowdsourced catalogue contains many Audible ASINs in
`editions.asin`.

Observed consequences:

- Some editions have the same Audible ASIN represented in both
  `editions.asin` and an Audible `book_mappings` row.
- Some editions contain an Audible ASIN only in `editions.asin`, with no Audible
  mapping.
- An edition's `editions.asin` can reflect one marketplace while its Audible
  mappings contain different ASINs for other marketplaces.
- Therefore, `editions.asin` is neither a complete nor semantically precise
  representation of Audible identity.
- It remains a real legacy signal: ignoring it can miss an existing edition
  that Hardcover itself already associates with the supplied ASIN.

## 4. Region discovery is separate from Hardcover matching

A live cross-region Audnex probe was performed with a bare Audiobookshelf ASIN.
The title resolved in only one marketplace among the tested regions. This
demonstrated that an external metadata lookup can identify the marketplace for
at least some bare ASINs without the region being stored in Audiobookshelf.

This result does not establish that every Audible title can be resolved in this
way. It also does not establish that querying a single configured region is
sufficient for a library containing purchases from multiple marketplaces.

## 5. Hardcover's web interface accepts regional Audible identifiers

Inspection of Hardcover's production web client found the following behavior:

- The Audible identifier control supports a region selection.
- A pasted Audible URL is used to infer the region from the marketplace host.
- A bare ASIN entered without a marketplace indication defaults to the United
  States region in the client.
- The resulting external ID is represented as `<ASIN>:<region>`.
- The edition-editing interface uses `insert_book_mapping` to add the mapping.
- The mapping interface also exposes a rebuild action backed by
  `book_mapping_normalize` with deep normalization enabled.

A regular user can add an Audible identifier through the web interface without
being asked to supply a country as a separate field. In observed catalogue
data, adding one Audible mapping through the webpage was followed within
seconds by the appearance of multiple regional mappings on the same edition.
Those rows contained marketplace-specific ASINs rather than one ASIN copied
across suffixes.

The private server-side mechanism responsible for that expansion was not
observable. The production client proves that it inserts a mapping and exposes
normalization, but it does not prove whether automatic expansion is performed
by the insert mutation itself, a server-side job, normalization, or another
backend process.

## 6. Web-user capability and API-token capability are different

The live GraphQL schema exposes `insert_book_mapping`, but a direct call with
the tested API credential failed with HTTP 403 and reported the missing
`write:catalog:map` scope. No mapping was created.

The user account could nevertheless add an Audible identifier through the
Hardcover webpage. The investigation therefore established that authenticated
web-user permissions are not equivalent to the scopes available to ordinary
API credentials.

The `write:catalog:map` scope is associated with librarian access and is not a
capability that can be assumed for the application's users. The successful web
workflow does not demonstrate that the same user can perform the mutation with
an API credential.

## 7. `upsert_book` resolves and imports, but does not guarantee mapping persistence

The live schema exposes `upsert_book` with a platform ID, an external ID, and an
optional existing book ID. The operation is asynchronous; its result is read
from `book_import_statuses`. Observed terminal statuses included `failed`,
`loaded`, and `created`.

Live tests established these behaviors:

- Passing a bare Audible ASIN failed while loading external data.
- Passing a region-qualified Audible external ID could resolve successfully.
- When the external ID represented a genuinely absent edition, the operation
  could create an edition and its Audible mapping.
- When the external ID resolved to an existing edition, the operation returned
  `loaded` with the correct existing book and edition.
- A `loaded` result did not add the previously missing regional Audible mapping
  to `book_mappings`.
- Repeating the qualified upsert returned the same existing edition and still
  did not persist the missing mapping.

This establishes that successful identity resolution and durable mapping
creation are separate outcomes. A caller cannot treat `loaded` as evidence that
the submitted external ID was stored.

## 8. The webpage reproduced the existing-edition mapping gap

The New Book flow in Hardcover's production client calls `upsert_book`, polls
the import status, and treats both `loaded` and `created` as successful. A
`loaded` result is shown as an existing edition and redirects the user to it.

The manual webpage test used a regional Audible ASIN that was absent from
`book_mappings`, while another regional Audible mapping already identified the
edition. The webpage reported that the edition was found and opened the correct
existing edition. A subsequent live query confirmed that the submitted
regional external ID was still absent from `book_mappings`.

The behavior therefore matches the direct API result: the flow can resolve a
missing regional identifier to an existing edition without making that
identifier available to later exact `book_mappings` lookups.

## 9. A legacy `editions.asin` match does not always prevent duplication

In a separate live test, an existing edition had the tested Audible ASIN in
`editions.asin` but no corresponding Audible mapping. A qualified
`upsert_book` request created another edition and an Audible mapping instead of
reusing the legacy edition.

This demonstrates that `upsert_book` cannot be assumed to use
`editions.asin` as a complete duplicate guard for Audible imports. It also
provides concrete evidence that excluding `editions.asin` from all matching can
permit duplicate editions in legacy catalogue data.

## 10. Exact mapping alone is not complete book matching

The combined evidence establishes these boundaries:

- An exact Audible `book_mappings` match is strong evidence and directly
  supplies the Hardcover book and edition IDs.
- Absence of that exact mapping does not mean the book or edition is absent.
- A sibling regional mapping may identify the same edition, but its ASIN can be
  different and is not derivable from the bare input.
- `editions.asin` can identify a real pre-existing edition when the mapping is
  missing, but its meaning and coverage are inconsistent.
- `upsert_book` may resolve an existing edition without recording the submitted
  regional identifier.
- `upsert_book` may also create a duplicate when only a legacy Edition ASIN
  connects the submitted identifier to an existing edition.

These are catalogue and API constraints, not an ordering decision for the
application's eventual matching algorithm.

## 11. The repository's persistent ASIN cache does not close the catalogue gap

The current sync code has a `PersistentASINCache` that stores a raw ASIN mapped
to a resolved Hardcover book. It is checked before a remote lookup and is
persisted in `asin_cache.json`.

Positive entries currently expire after 24 hours. The cache can therefore
reuse a recent successful resolution, but it does not permanently compensate
for a missing Hardcover mapping. After expiry, a later sync again depends on
the available Hardcover identifiers and matching paths.

## 12. Anonymized case record

| Case | Starting catalogue state | Action | Observed result |
|---|---|---|---|
| A | Exact regional Audible mapping exists | Query `book_mappings` by the regional external ID | Correct book and edition IDs returned |
| B | One regional mapping exists; a sibling regional mapping is absent | Qualified `upsert_book` with the known book ID | Existing edition returned as `loaded`; missing mapping remained absent |
| B, webpage | Same as Case B | Submit the absent sibling identifier in New Book | “Edition found” behavior and redirect to the existing edition; mapping remained absent |
| C | Audible ASIN exists only in `editions.asin` | Qualified `upsert_book` | New edition and mapping created instead of reusing the legacy edition |
| D | One Audible mapping added through the webpage | Observe the edition after the web action | Multiple marketplace-specific mappings appeared on the same edition |
| E | Bare ASIN with unknown region | Probe supported Audnex marketplaces | Exactly one tested marketplace resolved the title |

## 13. What the investigation did not establish

- Whether an API-token `upsert_book` that creates a new edition always expands
  all available regional mappings. A single-region result is inconclusive
  because many titles have no international equivalents.
- Whether `upsert_book` without a supplied `book_id` is safe and sufficiently
  deterministic for the application's initial book-matching path. That variant
  was not live-tested as part of this investigation.
- Which private Hardcover backend operation performs the observed regional
  expansion after a webpage mapping addition.
- Whether regional expansion is synchronous, eventually consistent, or
  conditional on external catalogue coverage.
- Whether every bare Audible ASIN can be assigned a region through Audnex.
- Whether Hardcover will later backfill a regional mapping after an
  `upsert_book` result of `loaded`; the tested mapping remained absent through
  the observation window.

## Evidence sources

- Audiobookshelf models and the checked-in Audiobookshelf API schema in this
  repository.
- Hardcover's live GraphQL schema and read/mutation responses.
- Hardcover's production web-client code for Audible identifier entry, edition
  mapping actions, and New Book import status handling.
- Live Audnex marketplace lookups.
- Direct API tests against existing and absent Hardcover catalogue records.
- Manual reproduction through the ordinary Hardcover webpage.
- The application's current ASIN matching and persistent-cache code.
