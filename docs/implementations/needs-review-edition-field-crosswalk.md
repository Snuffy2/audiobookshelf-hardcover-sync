# Field Crosswalk: Audiobookshelf Item to Hardcover Edition

**Revision notice (2026-09-23):** This crosswalk is a historical field study
for the earlier seven-step plan. Its audiobook R4 destination, Step 3–7
allocations, create pipeline, and Audible duplicate/matching rules are
superseded by the [current plan](needs-review-edition-creation.md) and the
[live mapping findings](../hardcover-audible-mapping-findings.md). Do not use
those older sections as implementation instructions. The merged Step 1–2
record and independently verified ABS/ISBN field observations remain useful;
the affected rows will be revised with their implementation PRs.

Companion to [needs-review-edition-creation.md](needs-review-edition-creation.md). That document says *what* the
"add an edition to Hardcover" feature does and how it is split into steps; this one says, field by field, what
Audiobookshelf (ABS) data ends up in which Hardcover edition field and what is done to it on the way.

**Status: 🚧 DRAFT** (2026-09-20). Documentation only; no code changes.

## Verified against

| Source | What was read |
|--------|---------------|
| Our code | Branch `step_4_needs_review_add_edition` (which contains steps 1-3). Functions are cited by name, not line, because the code is not on this branch. |
| ABS API docs | `audiobookshelf/audiobookshelf-api-docs` (`_items.md`, `_schemas.md`): `GET /api/items/{id}`, Library Item Expanded, Book Expanded, Book Metadata Expanded, EBook File, cover endpoint. |
| ABS server | `advplyr/audiobookshelf` (`master`): `server/models/Book.js` (`oldMetadataToJSON`, `toOldJSONMinified`, `toOldJSONExpanded`), `LibraryItemController.getCover`, `utils.reqSupportsWebp`, `CacheManager`. The server source is authoritative where the docs are older (for example `abridged`). |
| Hardcover schema | `docs/hardcover-schema.graphql` (same file as `internal/api/hardcover/hardcover-schema.graphql`): `insert_edition`, `EditionInput`, `BookDtoInput`, `ContributionInputType`, `ImageInput`. |
| Hardcover docs | `hardcoverapp/hardcover-docs`: `field-descriptions.json`, the Editions, Contributions, Languages, Countries and ReadingFormats schema pages, the librarian Edition Standards, ISBN and ASIN, and Editing FAQ pages, and `capabilities.json`. |

Nothing here was run against a live ABS server or live Hardcover. Statements that could only be settled by a live
call are marked **(assumed)** or **(unverified)**.

## 1. The pipeline

```
ABS  GET /api/items/{id}?expanded=1                    audiobookshelf.Client.GetLibraryItem            (step 3)
  |
  v  draft.New(...)                                                                                     (step 3)
mismatch local preparation (hc=nil)       Audnex release date (ASIN), ISBN split,                   (steps 1, 3)
  |                                       defaults (language 1, country 1), exact ABS names
  v
BookMismatch.ToEditionExport(..., nil)    edition_format, edition_information, audio_seconds         (steps 1, 3)
  v
draft.Draft  ->  GET .../edition-draft    editable preview, ABS names and local warnings; no HC call  (step 3)
  |
  v  user edits, then POST .../edition
ABS refetch + Hardcover resolution        exact author/narrator/publisher lookup                     (step 4)
  |
  v
multiuser.validateEditionInput            trim title, normalize ISBNs, require an identifier         (step 4)
  |                                       and a resolved author; validate server-derived values
  v
edition.Creator.CreateEdition             duplicate lookups, insert_edition (no cover upload)          (step 2)
```

The client supplies only the editable preview scalars. `book_id` comes from the run record's `hardcover_book_id`; reading
format, exact people/publisher names, their Hardcover IDs, duration, language/country and edition format are server-derived
from the ABS item fetched again at create time. The app sets no cover and uploads none (see R17).

## 2. Source: what ABS returns

The draft uses `GET /api/items/{id}?expanded=1`. On the ABS server, *expanded* is built as the minified book
JSON plus extra fields (`toOldJSONExpanded` spreads `toOldJSONMinified`), so `numTracks`, `duration` and
`ebookFormat` are present in it, and so are the full `authors[]` and `narrators[]` arrays.

Fields relevant to an edition (`media` is the book; `media.metadata` is its metadata):

| ABS field | Type | Decoded by `models.AudiobookshelfBook`? | Notes |
|-----------|------|------------------------------------------|-------|
| `id` | string | yes (`ID`) | Library item ID, for example `li_8gch9ve09orgn4fdz8`. |
| `libraryId` | string | yes | Carried in the mismatch export only. |
| `mediaType` | `"book"` or `"podcast"` | yes | ABS says `"book"` for ebooks too, so it does not identify an ebook. |
| `media.metadata.title` | string or null | yes | |
| `media.metadata.subtitle` | string or null | yes | |
| `media.metadata.authors[]` | `{id, name}` | **no** | Exact author names, one per element. |
| `media.metadata.authorName` | string | yes | The author names joined with `", "` (`Book.authorName`). |
| `media.metadata.narrators[]` | array of string | **no** | Exact narrator names, one per element. |
| `media.metadata.narratorName` | string | yes | `narrators.join(', ')`. |
| `media.metadata.publisher` | string or null | yes | |
| `media.metadata.publishedYear` | string or null | yes | For example `"2008"`. |
| `media.metadata.publishedDate` | string or null | yes | Free-form date string, preferred over the year-only fallback when Audnex has no release date. |
| `media.metadata.isbn` | string or null | yes | One field. It may hold an ISBN-10 or an ISBN-13, hyphenated or not. |
| `media.metadata.asin` | string or null | yes | |
| `media.metadata.language` | string or null | yes | Free text such as `"English"`. Not used today. |
| `media.metadata.abridged` | boolean | **no** | Present in the server source (all three metadata shapes) but not in the API docs sample. Step 1's review added it to the checked-in OpenAPI schema (`bookMetadataBase`, beside `explicit`). |
| `media.metadata.explicit` | boolean | **no** | |
| `media.metadata.description`, `descriptionPlain` | string or null | no | `descriptionPlain` is the description with HTML stripped (server source). |
| `media.metadata.genres[]`, `series[]`, `seriesName` | array, array of `{id, name, sequence}`, string | yes | Book-level on Hardcover, so not part of an edition. |
| `media.duration` | float, seconds | yes | Sum over the audio files. |
| `media.numTracks` | integer | yes | Non-excluded audio files. Format detection only. |
| `media.coverPath` | string or null | yes | An absolute path on the ABS server. Used only to tell whether a cover exists; it is never fetched. |
| `media.ebookFile` | object or null | yes (raw JSON) | A file object (`ino`, `metadata`, `ebookFormat`, ...). `null` for an audiobook. |
| `media.ebookFormat` | string | yes | Present in minified output. In expanded output the format also sits inside `ebookFile`. |
| `media.audioFiles[]`, `tracks[]`, `chapters[]`, `size`, `tags[]` | various | no | No Hardcover analogue. |

Ebook rule (`AudiobookshelfBook.IsEbook`, used by `ReadingFormat()`): the legacy `mediaType == "ebook"`, or an
`ebookFile` object or non-blank `ebookFormat` **and** no audio (`duration > 0` or `numTracks > 0` means audio). An item
with both audio and an ebook file is an audiobook.

## 3. Target: what Hardcover accepts

```graphql
insert_edition(book_id: Int!, edition: EditionInput!): EditionIdType   # { id: Int, errors: [String], edition: editions }

input EditionInput { book_id: Int, dto: BookDtoInput, locked: Boolean }

input BookDtoInput {
  asin: String            audio_seconds: Int              contributions: [ContributionInputType]
  country_id: Int         edition_format: String          edition_information: String
  image_id: Int           isbn_10: String                 isbn_13: String
  language_id: Int        page_count: Int                 publisher_id: Int
  reading_format_id: Int  release_date: date              subtitle: String
  title: String
}

input ContributionInputType { author_id: Int!, contribution: String }   # contribution: null = author; "Narrator", "Illustrator", ...
input ImageInput { imageable_id: Int!, imageable_type: String!, url: String! }   # insert_image
```

Points about the target that shape the mapping:

- **Nothing in `BookDtoInput` is required by the schema** (only the `book_id` argument, and `author_id` inside a
  contribution). The tool itself requires `book_id`, a title and at least one author (`EditionInput.Validate`), and our
  create endpoint also requires an ASIN or ISBN so a later sync can match the edition.
- **`reading_format_id`**: 1 Physical, 2 Audio, 3 Both, 4 Ebook (Hardcover field docs). The tool sends 2 or 4.
- **`edition_format`** is a string that Hardcover normalizes on write (`Audible Audio` was stored as `Audible`). Hardcover's Editing FAQ (a draft page) says it elaborates on the edition (for
  example "Full cast audiobook"), describes the edition rather than where it was obtained, and is usually left blank.
  `reading_format_id` is the required field on Hardcover's own form.
- **`release_date`** is a `date` scalar: `YYYY-MM-DD`. Hardcover's Edition Standards say to use January 1 of the year
  when only the year is known.
- **`language_id`, `country_id`, `publisher_id`** are foreign keys (`languages`, `countries`, `publishers`), not names.
  `languages` has `language`, `code2`, `code3`.
- **Identifiers**: Hardcover strips hyphens from ISBNs on write (confirmed: a hyphenated ISBN-13 was stored without them). `isbn_10_valid` /
  `isbn_13_valid` are computed on their side; they exist on the hosted API's `editions` (see the R5 and R6 field note for what
  was confirmed). Their ISBN guide warns that adding or removing `978` does not give a valid number, because the check digit
  must be recalculated (`internal/isbn` does recalculate it).
- **Cover**: `image_id` is set on a *second* call. The tool uploads the file to Hardcover's storage
  (`POST https://hardcover.app/api/upload/google`, then a multipart POST to the returned URL), calls `insert_image`,
  then `update_edition(id, {dto: {image_id}})`. Hardcover's Edition Standards: PNG and JPEG work best, **WebP is not
  supported**, and larger is better (at least 300x450 for the top quality tier). Against the real API with an API token the
  storage-credentials call to `hardcover.app/api/upload/google` answered `401` (a plain HTTP client gets a Cloudflare challenge), so the
  cover step could not complete and the edition was created without it; Hardcover itself attached a cover from the ISBN to an ebook edition.
  Nothing uploads a cover for now (see R17); the creator's flow stays in the code but is switched off.
- **Permissions** (confirmed): creating an edition needs `write:catalog:append`, and `read:catalog` covers the lookups. A token
  without it is refused with `403 insufficient_scope` ("Missing scopes: write:catalog:append") and nothing is created; with it the
  insert succeeds. The sync token's scopes
  (`read:library`, `read:catalog`, `read:lists`, `read:me`, `write:library`) do not include it, so creating an edition needs a
  second key or an added scope, and the docs for each step say so (see the plan's "Token scopes" items). A token's scopes cannot be
  read (no introspection endpoint, opaque tokens, no schema field). A pre-flight edition insert with a book id that cannot exist
  tells the cases apart without creating anything: HTTP 200 with a "Couldn't find Book" error when the scope is present, `403
  insufficient_scope` with a `scope` field naming the missing scope when it is not (both confirmed against the real API). Step 4
  builds the gate on it and step 7 uses it to show the add-edition action. The owner reports that
  the key also needs `read:library` and `write:library`; the capability table does not list them for these mutations and the create
  path only reads `books` and `editions`, so that is to be confirmed with a key that lacks them.
- **Not settable on an edition**: description, series, genres and tags are book-level (`BookDtoType`), and there is no
  physical-format field in `BookDtoInput`. There is no external-mappings field either: `EditionInput` is `{ book_id, dto, locked }`
  and `BookDtoInput` holds only edition metadata. External mappings are the separate `book_mappings` table, written by
  `insert_book_mapping(object: { edition_id, external_id, platform_id })`, which needs `write:catalog:map`, not
  `write:catalog:append`. Platform 32 is `Audible` and 19 is `amazon`. Our matching does not need one: the ASIN search already
  matches `editions.asin`, and also `book_mappings` with platform `Audible`. Adding a mapping would be a second call and a further
  scope, so it is out of scope for these steps.

## 4. The crosswalk

Row IDs (`R1` to `R18`) are stable: other documents refer to them, so a new row is appended and an existing row is never
renumbered. "Steps" lists every step that delivers part of the row (implements or changes it); section 5 says what each
step delivers and what it only has to verify. **UI** means the field appears as an editable input in the step 7 preview;
**API** means the POST body accepts it; **server** means the client cannot set it.

| Row | Hardcover field | ABS source | Transformation and logic | Steps | Edit |
|---|-----------------|------------|--------------------------|------|------|
| R1 | `book_id` (mutation argument) | none; the run record's `hardcover_book_id` | Integer greater than 0, else the book is not eligible. Overrides any candidate the enrichment guessed. | 2, 3, 4 | server |
| R2 | `title` | `metadata.title` | Passed through. Create trims it and rejects a blank one. No series or prefix clean-up. | 2, 3, 4, 7 | UI, API |
| R3 | `subtitle` | `metadata.subtitle` | Passed through; omitted when empty. | 2, 3, 7 | UI, API |
| R4 | `asin` | `metadata.asin` | Trimmed only. No case or shape check. Also triggers the Audnex lookup (R8) and sets `edition_format` (R11). | 2, 3, 4, 5, 7 | UI, API |
| R5 | `isbn_13` | `metadata.isbn` | Normalized (separators removed, trailing `x` uppercased), classified by shape. 13 digits goes here; a 10-character value goes to R6. The draft then fills the *other* form when it can be derived. The export also carries `isbn_10_valid` / `isbn_13_valid`: whether the exported ISBN's own check digit is correct (omitted when that ISBN is empty). A bad-checksum ISBN is still exported. | 1, 2, 3, 4, 5, 7 | UI, API |
| R6 | `isbn_10` | `metadata.isbn` | As R5. The ISBN-10 to ISBN-13 conversion adds `978` and recalculates the check digit; a 979 ISBN-13 has no ISBN-10. A counterpart is derived only when the input's own checksum is valid. | 1, 2, 3, 4, 5, 7 | UI, API |
| R7 | `contributions[]`, author (`contribution: null`) | expanded `metadata.authors[].name`, with `authorName` fallback | The draft carries exact trimmed ABS names without a Hardcover call. After create confirmation, each name is looked up on Hardcover by **exact, case-sensitive name** among active authors; the first row returned is used. IDs are de-duplicated and keep ABS order. No source author is a draft warning; no Hardcover match is a create-time 422. | 2, 3, 4, 7 | server |
| R8 | `release_date` | Audnex `releaseDate` (needs `asin`), else `metadata.publishedDate`, else `metadata.publishedYear` | Audnex is asked in the profile's configured region and then `us` (once, with no region, when none is configured). The first available value is normalized to `YYYY-MM-DD` (ISO 8601 with a time, RFC 3339, several slash and month-name layouts). A year alone becomes `YYYY-01-01`. Create requires `YYYY-MM-DD`. | 2, 3, 7 | UI, API |
| R9 | `contributions[]`, `"Narrator"` | expanded `metadata.narrators[]`, with `narratorName` fallback | The draft carries exact trimmed ABS names without a Hardcover call. On create, lookup is by exact name among active authors **that already have a `Narrator` contribution**. Audiobook only; an ebook has no narrators. A create-time miss is non-fatal, omits the narrator and returns a fixed warning. | 1, 2, 3, 4, 7 | server |
| R10 | `publisher_id` | `metadata.publisher` | The draft carries the exact ABS name only. On create it is looked up by **exact, case-sensitive** publisher name. Not found gives `0`, is omitted, and returns a fixed warning. Never defaults to a guessed ID (was `1` before step 1). | 1, 2, 3, 4, 7 | server |
| R11 | `edition_format` | `metadata.asin`, `metadata.publisher`, item format | Audiobook: an ASIN gives `Audible Audio`; else a publisher containing "libro" gives `libro.fm`; else empty, and the creator sends `Audiobook`. Ebook: always `Ebook`, replacing any label the record carries. Recomputed from the ABS item at create time, trimmed, at most 100 characters. Hardcover normalizes the stored label: `Audible Audio` was stored as `Audible` (`Ebook` is unchanged). | 1, 2, 3, 4, 7 | server |
| R12 | `reading_format_id` | `mediaType`, `ebookFile`, `ebookFormat`, `duration`, `numTracks` | `IsEbook()` gives 4, otherwise 2. Recomputed from ABS at create time; the request cannot set it (an extra `reading_format` field is rejected with 400). | 1, 2, 3, 4, 5, 7 | server |
| R13 | `audio_seconds` | `media.duration` | `int(duration + 0.5)`. Recomputed from the ABS item at create time and sent only when greater than 0 and the item is an audiobook. | 1, 2, 3, 4, 7 | server |
| R14 | `edition_information` | `metadata.abridged` | Audiobook: `Abridged` when ABS marks it abridged, else `Unabridged`. Ebook: empty (omitted). The mismatch record's own placeholder `Audiobookshelf` is discarded; a real value on the record would win (no production code sets one, so `BookMismatch.EditionInfo` is slated for removal, see the plan's step 3 checklist). | 1, 2, 3, 7 | UI, API |
| R15 | `language_id` | none (`metadata.language` is used only for a preview warning) | Server-derived constant `1` (assumed English). | 2, 3, 4 | server |
| R16 | `country_id` | none | Server-derived constant `1` (assumed United States). | 2, 3, 4 | server |
| R17 | cover: `insert_image`, then `update_edition {image_id}` | `media.coverPath` (existence only) | **Not used for now: nothing uploads a cover.** The creator keeps the flow but switched off (`EnableCoverUpload`, called by nothing), so `image_url` is neither fetched nor uploaded and `ImageError` says so. If it is switched on: URL is `<ABS base>/api/items/<id>/cover`, credentials, query and fragment stripped; empty when the item has no cover. The creator downloads it (ABS bearer token only when the host matches the profile's ABS URL), uploads it to Hardcover, creates the image record, and attaches it. Only a PNG or JPEG of at most 15 MiB is uploaded (decided from the downloaded bytes). Any failure is a warning; the edition stays. | 2 | server |
| R18 | `page_count` | none | Not sent. ABS has no page count. | n/a | n/a |

### Field notes

**R5 and R6, ISBN.** ABS has one `isbn` string; Hardcover has two fields. `isbn.Parse` accepts a value by shape only:
13 digits, or 9 digits followed by a digit or `X`. The draft first records only the form the item carries, then fills
the other with `isbn.ISBN10()/ISBN13()`, which returns the counterpart only for a valid checksum. A bad-checksum ISBN
is still sent as given and has no counterpart. Step 1's export adds `isbn_10_valid` / `isbn_13_valid` (the names Hardcover's
`editions` table uses) so a later step can decide what to do with it.

Confirmed against the hosted API with read-only GraphQL queries (the checked-in schema snapshot does not list these fields, so
their existence rests on the hosted API and its docs, not on this repository):
- `isbn_10_valid` and `isbn_13_valid` exist on `editions` as booleans, and `true` for ordinary valid ISBNs.
- Hardcover stores editions whose ISBN has a bad checksum and flags them `false`; it does not reject them. Both flags returned
  `false` rows, and a valid 979 ISBN-13 is `true`.
- The flag is `null` when the ISBN is empty, which matches our export omitting the key in that case.
- On 27 sampled values (valid and invalid, ISBN-10 and ISBN-13, 978 and 979) every Hardcover flag matched the checksum
  result of `internal/isbn`.
- An exact `_eq` lookup matches only the stored, hyphen-free value; a hyphenated form returned no row. Lookups must therefore
  send a normalized ISBN (step 5's client does).
- Some 979 ISBN-13 editions carry an ISBN-10 of their own, entered directly; we never derive one for a 979, and an ISBN-10
  search still finds them.

Confirmed by writes to a test book: `insert_edition` accepts a bad-checksum ISBN-13 and stores it, without hyphens, flagged
`isbn_13_valid: false` (no ISBN-10 was derived). Given only a valid ISBN-10, Hardcover also stored the matching ISBN-13, flagged valid,
which is the value `internal/isbn` derives, so a created edition can hold both forms.

**Not tested:** the `isbns_match` field, and how often Audiobookshelf serves a bad checksum. The draft exposes the flag and
warns when it is false; whether create accepts or rejects it remains open (see the step 4 checklist). On create, each field is
re-normalized and must have the right length for its slot (`isbn_10 must be a valid 10-character ISBN`), and at least
one of `asin`, `isbn_10`, `isbn_13` is required.

**R7 and R9, people.** ABS's `authorName` is `authors.map(name).join(', ')`, so a single author called
`"John Smith, Jr."` is indistinguishable from two authors once joined. The expanded item carries the exact
`authors[].name` and `narrators[]`; step 3 decodes and carries those exact values, with the joined strings only as a legacy
fallback (finding 2). Person lookup uses `SearchPeople` in the Hardcover client only after create confirmation:

- The query is `authors(where: {state: {_eq: "active"}, name: {_eq: $name}})` (plus, for narrators, `contributions:
  {contribution: {_eq: "Narrator"}}`), limit 5, **no `order_by`**. `people.go` says it takes "the top result by book
  count", but no ordering is requested, so it takes the first row Hardcover returns.
- `canonical_id` is selected but never used, so a merged or duplicate author record could be picked.
- The narrator fallback query is identical to the first one, so a narrator who exists on Hardcover only as an author
  (no earlier `Narrator` credit) is never matched.
- A name made only of digits is first tried as a Hardcover person ID, then as a name.
- Results are cached for the life of the process, keyed by type and lower-cased name.
- Because a name must match exactly, "J.R.R. Tolkien" and "J. R. R. Tolkien" are different lookups. A required-author
  miss is a create-time 422; optional narrator/publisher misses become fixed create-response warnings.

**R8, release date.** Audnex is an Audible metadata service, so it only helps for Audible ASINs. Without a date from
Audnex, the full ABS `publishedDate` is preferred; `publishedYear` is the final fallback, and the Hardcover librarian
rule (January 1 when only the year is known) is exactly what `YYYY-01-01` gives. Slash dates are read US-style
(`01/02/2006` is tried before `02/01/2006`).

**R11, edition format.** The audiobook labels come from `ToEditionExport`, which also produces the mismatch JSON the
`edition` CLI imports, so the draft matches what the file flow would have produced. This differs from the Hardcover
FAQ's advice to describe the edition rather than its source (finding 6).

**R17, cover.** ABS's cover endpoint scales the image: `width` defaults to 400, `height` to proportional, and
`format` to `webp` or `jpeg` depending on the request's `Accept` header (`reqSupportsWebp` is true when `Accept` contains
`image/webp` or equals `*/*`). The creator sends `Accept: image/jpeg, image/png`, so ABS answers with a **JPEG about 400 pixels wide**,
which is acceptable to Hardcover but not large. `?raw=1` returns the original file in whatever format it has (possibly
WebP, which Hardcover does not support). The creator follows Hardcover's Edition Standards (PNG and JPEG work best, WebP is
not supported, larger is better, no hard size maximum documented; a 15 MB file is named as fine): it decides the format from
the downloaded bytes, uploads only PNG (`png`) or JPEG (`jpg`), refuses a download over 15 MiB, and keeps the edition, with a
fixed `ImageError`, when it rejects a cover. This is the creator only: the app passes no `image_url`, so it never runs.

## 5. What each step must deliver from the crosswalk

Every step in [the plan](needs-review-edition-creation.md) touches some rows. For each step:

- **Delivers** means the step's code implements or changes that behavior. Its tests pin every delivered row, including the
  edge cases named below, at a real interface (the GraphQL variables sent, the HTTP response, the export JSON) rather than
  at implementation details.
- **Verifies** means the step depends on behavior that already exists (from an earlier step or from `develop`), so it needs
  a test that fails if that behavior changes.
- The step's PR description names the rows it delivers. If the code ends up behaving differently from a row, the row is
  corrected in the same commit as the code.

| Step | Delivers | Verifies | Findings to decide |
|------|----------|----------|--------------------|
| 1 ISBN and export foundations | R5, R6 (ISBN split); R10 (unresolved publisher is 0); R11, R13, R14 for ebook exports; R14 `Abridged` for an audiobook; R12 (format helpers) | Audiobook export otherwise unchanged: R2-R4, R7-R9, R11, R13-R17 | 1 (decided and done) |
| 2 Edition creator | The Hardcover side of every row: R1-R16 (the `dto`, duplicate detection), R17 (the creator's cover chain and token scoping; the app does not use it) | R5, R6 forms from step 1 | 5, 8 |
| 3 Draft endpoint | The ABS/Audnex side: decode and preview R1-R6, R8, R11-R16; carry exact R7/R9/R10 names without resolving IDs; identifier requirement | R5, R6, R11-R14 export behavior; R12 | 2, 3, 4, 7 (6 is informational) |
| 4 Create endpoint | The editable set, validation and normalization plus every Hardcover operation: resolve R7/R9/R10, duplicate-check and insert R1-R16; R11-R13/R15/R16 derived on the server | R3, R8 and R14 pass through unchanged; exact expanded names from step 3's ABS model | 5, 8, 9 |
| 5 Sync matching | R4, R5, R6, R12 as matching: the sync finds what the crosswalk creates | none | none |
| 6 Resync | nothing new | R1, R4-R6, R12: the resync finds the edition just created | none |
| 7 UI | What is editable, shown, hidden and echoed: R2-R14 | Escaping of every ABS-supplied string | 9 (UI part), 3 if language is shown |

### Step 1: ISBN and export foundations

Code: `internal/isbn`, `internal/models` (`ReadingFormat`, `ReadingFormatID`), `mismatch.AddWithMetadata`, `ToEditionExport`.

- **Delivers R5, R6.** Hyphenated ISBN-13 and ISBN-10 are kept (the old length-only split dropped them); a lowercase `x` check
  digit is uppercased; spaces, dots, underscores and dash variants are removed; a value that is neither 10 nor 13 characters
  in shape produces neither field; the export records only the form the item carries, never a derived counterpart;
  `isbn.Parse` derives a counterpart only for a valid checksum and never for a 979 ISBN-13.
- **Delivers the ISBN checksum flags (maintainer review).** A checksum-invalid ISBN is still exported as given (the package
  accepts by shape; this is documented in `internal/isbn`). `isbn.Result.Valid` reports the input's own checksum and the export
  adds `isbn_10_valid` / `isbn_13_valid`, omitted when that ISBN is empty; the `edition` command ignores them. A valid 979
  ISBN-13 is `true` even though it has no ISBN-10.
- **Delivers R10.** An unresolved publisher exports `publisher_id: 0` (it was `1`); a resolved ID is exported, including one
  resolved late inside `ToEditionExport`.
- **Delivers R12.** `ReadingFormat()` and `ReadingFormatID` are pinned by a truth table: audio plus an ebook file is an
  audiobook; an ebook file or `ebookFormat` and no audio is an ebook; the legacy `mediaType: "ebook"` is an ebook; nothing
  is an audiobook; an unknown format string maps to id 2.
- **Delivers R11, R13, R14 for an ebook.** The export has `edition_format: Ebook` (forced, so an audiobook platform label on a
  legacy or hand-built record cannot leak), `reading_format: ebook`, no audio seconds and no `Unabridged`.
- **Verifies (audiobook export is otherwise unchanged, field for field):** R2-R4 pass-through; the R7 and R9 ID lookups; R8 (Audnex
  date, region fallback, normalization, year fallback); the R11 audiobook labels (`Audible Audio`, `libro.fm`, empty); R13
  rounding; R14 `Unabridged` default (when not abridged) and the discarded `Audiobookshelf` placeholder; R15 and R16 constants; R17 cover
  preference (ABS cover, then its image URL, then the Hardcover cover).
- **Delivers R14 for an audiobook (finding 1, done).** `metadata.abridged` is decoded, carried on the mismatch record, and
  an abridged audiobook exports `Abridged`; a book that is not marked abridged, or where the flag is absent (older
  servers), still exports `Unabridged`. A real value on the record still wins, and an ebook is unaffected.

### Step 2: Edition creator hardening

Code: `edition.Creator`. This step owns everything sent to Hardcover, so its tests assert the exact variables passed to
`GraphQLMutation` for each row.

- **R1:** `book_id` is the `bookId` argument; zero is rejected by `Validate`.
- **R2, R3:** a title is required; the subtitle is omitted when empty.
- **R4-R6:** sent when set. Duplicate detection runs before `insert_edition`: ASIN, ISBN-13, ISBN-10, then the converted
  forms, de-duplicated and scoped to the input's reading format. A same-book edition is returned untouched (`Existing`);
  another book's, or one whose book is unknown, is `ErrEditionBelongsToOtherBook`; the "already exists" insert error falls
  back to the same lookup.
- **R7, R9:** authors are contributions with `contribution: null` and at least one is required; narrators are `"Narrator"`;
  an ebook sends no narrators.
- **R8:** `release_date` must be `YYYY-MM-DD`.
- **R10, R15, R16:** sent only when greater than 0.
- **R11:** the trimmed `edition_format` is sent; empty falls back to `Audiobook` or `Ebook`.
- **R12:** `reading_format_id` is 2 or 4; an invalid `reading_format` fails validation.
- **R13, R14:** `audio_seconds` only when greater than 0 and an audiobook; `edition_information` when non-empty.
- **R17:** switched off: with the upload off no cover request is made and `ImageError` says so. With it on (tested), the cover chain (download, storage credentials, upload, `insert_image`, `update_edition`); the ABS token goes only
  to the configured ABS base URL; each failure keeps the edition and sets `ImageError`; only a PNG or JPEG, decided from the
  downloaded bytes, is uploaded (extension `png` or `jpg`), and a download over 15 MiB is refused; the token is never sent on a
  non-https hop of a request that started on https, and not to a different domain on redirect. This is the creator level: the app
  uploads no cover and never calls `SetAudiobookshelfBaseURL`; the `edition` and `image-tool` commands call it, and it only matters once
  the upload is switched on.
- **Verifies:** the ISBN forms that step 1's `isbn` package produces for the converted lookups.
- **Decided:** finding 5 (cover size and format), see the findings list; finding 8 (token scope) is confirmed against the real API but cannot be tested offline, so say so
  in the PR.

### Step 3: Draft endpoint

Code: `audiobookshelf.Client.GetLibraryItem`, `internal/edition/draft`, `PrepareEditionDraft`. This step owns the ABS side and
the mapping into the previewable draft. Its fixtures are real expanded ABS items (the API docs sample), one audiobook and one
ebook.

- **ABS decode.** `GetLibraryItem` reads `?expanded=1`; a 404 is a typed not-found. Decide what else the model decodes:
  `authors[]`, `narrators[]`, `language`, `publishedDate` (findings 2-4; `abridged` is already decoded by step 1).
- **R1:** the target book ID comes from the run record; a record without a positive Hardcover book is not eligible. This
  value identifies where a later create will write and does not cause a draft-time Hardcover call. **Identifier
  requirement:** an item with neither an ASIN nor a parseable ISBN is refused (409).
- **R2-R4:** passed through.
- **R5, R6:** the counterpart form is filled only when it can be derived (valid checksum, 978 only); both stay editable.
- **R7, R9:** carry exact expanded ABS names, including commas within a name, with the joined legacy fields only as a
  fallback. No Hardcover IDs are present. A missing source author is a local warning; an ebook draft has no narrators and
  no narrator warning. Finding 2 is handled here; Hardcover matching in finding 9 moves to step 4.
- **R8:** Audnex region fallback, normalization, the ABS `publishedDate` fallback, then the year fallback; a warning when there is no date.
- **R10:** carry the publisher name without resolving it. There is no draft warning about whether Hardcover has a match.
- **R11, R13, R14:** taken from the export: audiobook labels, `Ebook` for an ebook; duration rounded with `int(d + 0.5)`
  (33854.905 becomes 33855); an ebook has no audio and no `Unabridged`.
- **R12:** the draft reports `reading_format`.
- **R15, R16:** constants; finding 3 adds a warning when ABS names a language other than English.
- **R17:** not used: the draft carries no cover URL.
- **Verifies:** the step 1 local/export behavior above, seen through the draft (R5, R6, R11-R14), and R12.
- **Decide:** findings 2, 3, 4 and 7; finding 6 is informational. Finding 9 is create-time work.
- **External-call boundary:** one ABS item fetch and optional Audnex enrichment only. Tests prove that draft preparation
  does not construct a Hardcover client or issue any Hardcover request.

### Step 4: Create endpoint

Code: the editable request DTO, create-time metadata resolver, `validateEditionInput`, `normalizeEditionIdentifiers`,
`CreateEditionFromRunBook`, the POST handler.

- **The editable set is exactly** title, subtitle, ASIN, ISBN-10, ISBN-13, release date and edition information (the API
  column of section 4). Any other field, including names, Hardcover IDs, `edition_format`, `audio_seconds`,
  `language_id`, `country_id`, `reading_format`, `book_id` and `image_url`, is rejected (400), and the body must be
  exactly one JSON object.
- **R1:** `book_id` from the run record only. **R12:** the reading format comes from the ABS item fetched at create time, so an
  ebook cannot be created as an audiobook or the reverse.
- **R2:** trimmed; a blank title is 422. **R4-R6:** the ASIN is trimmed and the ISBNs lose hyphens and spaces; a value of the
  wrong shape for its slot is 422; at least one of ASIN, ISBN-10, ISBN-13 is required (422); an ABS item with no identifier
  is 409.
- **R7, R9, R10:** refetch the expanded ABS item and resolve its exact names on Hardcover only after confirmation.
  Author IDs are required; no author match is 422. Narrator and publisher misses are non-fatal, omitted, and returned as
  fixed warnings. Transport, GraphQL, rate-limit or timeout failures are sanitized 502 responses and no insert occurs.
  Resolved author/narrator lists remain bounded to 50 positive IDs. **R11, R13, R15, R16:** recomputed from the ABS item
  and validated server-side; the client cannot override them.
- **R17:** not used: create sets no `image_url` (a request that sends one is 400), uploads no cover and returns no cover warning.
- **Also:** a duplicate on another book is 409 with the fixed message; a dry run issues no mutation and returns
  `edition_id: 0`.
- **Verifies:** R3, R8 and R14 reach the creator unchanged.
- **Decide:** finding 8 (the required scope) goes in the PR's testing notes and endpoint docs; finding 9 is handled at
  create time; finding 5 is informational.

### Step 5: Sync identifier matching

Code: `findBookInHardcover` and the identifier searches. This step is the read side of the identifier and format rows: an
edition written by the crosswalk must be found by the sync, which is what takes the book out of Needs review.

- **R4:** an ASIN matches after trimming, with case preserved on both sides.
- **R5, R6:** the item's ISBN is searched in the form it has first and then its counterpart, each in its own field;
  lowercase `x` and separators are normalized; a 979 or bad-checksum ISBN has no counterpart search.
- **R12:** the strict same-format rule (audiobook 2, ebook 4) is kept on every query, including the ASIN query, which has no
  test yet.
- **Verifies, with one test per shape the crosswalk can create:** ASIN only; ISBN-10 only; ISBN-13 only; both ISBNs; an
  ISBN-13 whose ISBN-10 cannot be derived; an ebook. The preview lets the user clear either ISBN, and Hardcover
  derives the other form only when the given ISBN is valid (observed live), so an edition can end up with one form or both and
  matching must not depend on both.

### Step 6: Immediate read-status resync

Code: `Service.SyncBook` and the create endpoint's `resync` option. Nothing new is delivered from the crosswalk; the step
depends on it.

- **R1:** the created edition is on the same Hardcover book as the run record's candidate, so the resync's own lookup finds
  it and not some other candidate.
- **R4-R6, R12:** after a create, `findBookInHardcover` finds the new edition for each identifier shape and both formats.
  Because create requires an identifier, a resync always has one to match on.
- **Dry run** creates nothing (`edition_id: 0`), so no resync is attempted.

### Step 7: UI

Code: `web/static/app.js`, `index.html`, `styles.css`. This step decides what the person edits, sees, and cannot see.

- **Editable inputs, exactly:** R2 title, R3 subtitle, R4 ASIN, R5 ISBN-13, R6 ISBN-10, R8 release date, R14 edition
  information.
- **Read-only:** R7, R9, R10 (the Audiobookshelf author, narrator and publisher names; not pre-resolved on Hardcover), R11 edition format, R12 reading format,
  R13 duration.
- **Ebook:** hide R9 (narrators) and R13 (duration) and show R12.
- **Submitted:** only the editable scalars. The UI never receives or sends Hardcover author, narrator or publisher IDs;
  create refetches the ABS item and resolves them after confirmation.
- **Messages:** every local draft warning, every fixed create-time optional-resolution warning, and the 409, 422 and
  sanitized 502 messages.
- **Safety:** every ABS-supplied string (title, names, publisher) is escaped.
- **Decide:** finding 9 (no author matched leaves the user with no way to continue), and finding 3 if the language is shown.

## 6. ABS fields that are not mapped

| ABS field | Why | By design or a gap |
|-----------|-----|--------------------|
| `description`, `descriptionPlain` | Description is book-level on Hardcover (`BookDtoType`), not in `BookDtoInput`. | By design |
| `genres`, `tags`, `series[]`, `seriesName` | Book-level on Hardcover (series, tags). | By design |
| `explicit` | No edition field. | By design |
| `chapters`, `audioFiles`, `tracks`, `size`, `libraryFiles`, `path`, timestamps | No Hardcover analogue. | By design |
| `titleIgnorePrefix`, `authorNameLF` | Sort helpers. | By design |
| `libraryId`, `folderId` | Only recorded in the mismatch export. | By design |
| `abridged` | Informs `edition_information` (`Abridged`). | Mapped in step 1 (finding 1); documented in the OpenAPI schema in step 1's review |
| `authors[]`, `narrators[]` | Exact names, instead of splitting the joined strings. | **Gap** (finding 2) |
| `language` | Should inform `language_id`. | **Gap** (finding 3) |
| `publishedDate` | Preferred over `publishedYear` when Audnex has no release date. | Mapped in step 3 (finding 4) |

Hardcover `BookDtoInput` fields the tool never sends: `page_count` (no source), and `image_id` in the insert (it is set
afterwards by `update_edition`). `EditionInput.locked` is not sent.

## 7. Server-controlled and editable fields

| Field | Who decides | Detail |
|-------|-------------|--------|
| `book_id` | server | Run record; the request cannot retarget the edition. |
| cover URL | none | Not used: the app sets no `image_url` and uploads no cover (see R17). |
| `reading_format_id`, `edition_format`, `audio_seconds`, `language_id`, `country_id` | server | Derived from the expanded ABS item fetched at create time. |
| `author_ids`, `narrator_ids`, `publisher_id` | server | Exact ABS names resolved through Hardcover after confirmation; never accepted from the client. |
| Editable request fields | client, validated | `title`, `subtitle`, `asin`, `isbn_10`, `isbn_13`, `release_date`, `edition_information`. |

The step 7 UI (planned) shows editable text inputs for title, subtitle, ASIN, ISBN-10, ISBN-13, release date and edition
information, and shows the ABS author, narrator and publisher names read-only. A person whose author name does not match
on Hardcover learns that only when create returns its fixed 422 and cannot override IDs through this API (finding 9).

## 8. Calls made

**Draft** (read-only): one ABS item fetch; if there is an audiobook ASIN, up to two Audnex region attempts sharing the
bounded Audnex policy. It makes **zero Hardcover requests** and constructs no Hardcover client.

**Create**: one expanded ABS item fetch; one lookup per exact author and narrator name plus a publisher lookup; duplicate
lookups by ASIN, ISBN-13, ISBN-10 and converted forms (de-duplicated and scoped to the item's reading format); then
`insert_edition`. The app makes no cover calls. A duplicate on the same book is reused untouched; one on a different
book is refused (409). All Hardcover work uses the profile's rate-limited client and the create operation's single
bounded context.

## 9. Worked examples

IDs below are **placeholders**, not real Hardcover IDs.

### Audiobook

ABS (from the API docs sample, shortened):

```json
{
  "id": "li_8gch9ve09orgn4fdz8", "libraryId": "lib_c1u6t4p45c35rf0nzd", "mediaType": "book",
  "media": {
    "metadata": {
      "title": "Wizards First Rule", "subtitle": null,
      "authors": [{ "id": "aut_z3leimgybl7uf3y4ab", "name": "Terry Goodkind" }],
      "narrators": ["Sam Tsoutsouvas"],
      "publishedYear": "2008", "publishedDate": null, "publisher": "Brilliance Audio",
      "isbn": null, "asin": "B002V0QK4C", "language": null, "explicit": false,
      "authorName": "Terry Goodkind", "narratorName": "Sam Tsoutsouvas"
    },
    "coverPath": "/audiobooks/Terry Goodkind/Sword of Truth/Wizards First Rule/cover.jpg",
    "duration": 33854.905, "ebookFile": null
  }
}
```

After the user confirms, create resolves the three ABS names and produces these Hardcover `insert_edition` variables,
assuming Hardcover knows the names exactly and Audnex returned no date (with a date, `release_date` is that date instead
of `2008-01-01`):

```json
{
  "bookId": 12345,
  "edition": {
    "dto": {
      "title": "Wizards First Rule",
      "edition_format": "Audible Audio",
      "reading_format_id": 2,
      "asin": "B002V0QK4C",
      "contributions": [
        { "author_id": 1001, "contribution": null },
        { "author_id": 2002, "contribution": "Narrator" }
      ],
      "publisher_id": 3003,
      "language_id": 1,
      "country_id": 1,
      "audio_seconds": 33855,
      "release_date": "2008-01-01",
      "edition_information": "Unabridged"
    }
  }
}
```

No cover is uploaded from the app (R17). `subtitle`,
`isbn_10` and `isbn_13` are absent because ABS had none. `isbn` is null here, so the request carries only the ASIN
identifier and duplicates are looked up by ASIN.

### Ebook

ABS excerpt (`isbn` is a checksum-valid sample taken from Hardcover's ISBN guide; the other values are invented):

```json
{ "mediaType": "book",
  "media": { "metadata": { "title": "Example Ebook", "authors": [{ "name": "A. Author" }],
                           "isbn": "978-0-525-50514-3", "asin": "B0768ZM5QH", "publishedYear": "2019" },
             "duration": 0, "numTracks": 0, "ebookFile": { "ebookFormat": "epub" } } }
```

`ReadingFormat()` is `ebook` (an ebook file and no audio). Result: `reading_format_id: 4`, `edition_format: "Ebook"`,
`isbn_13: "9780525505143"`, `isbn_10: "0525505148"` (derived), `asin: "B0768ZM5QH"`, an author contribution, **no**
narrator contributions, **no** `audio_seconds`, **no** `edition_information`, `release_date` from Audnex if it knows the
ASIN (Audnex is Audible-only, so a Kindle ASIN is not expected to resolve, which leaves `2019-01-01`). The duplicate lookups only consider ebook editions.

## 10. Findings and open decisions

Each is also an unchecked item under the matching step in the plan document's "Step checklists", and section 5 says
which step decides it.

1. **`abridged` was ignored** (resolved in step 1). Every audiobook export said `Unabridged`, even when ABS marked
   it abridged. `metadata.abridged` is now decoded and an abridged audiobook exports `Abridged` (R14); the draft gets it
   through the same export path.
2. **Names are split from a joined string** (step 3). Use the exact `authors[].name` and `narrators[]` arrays from the
   expanded item instead of splitting `authorName` and `narratorName` on commas.
3. **`language` is ignored** (step 3). A non-English item is created as language 1. Minimum: a draft warning when ABS gives
   a language other than English. Better: look the language up in Hardcover's `languages` table. `language_id 1` and
   `country_id 1` are English and the United States (checked against Hardcover).
4. **`publishedDate` is preferred over `publishedYear`** (step 3, done). When Audnex has no release date, the full ABS date is tried before the year-only fallback.
5. **Cover** (decided in step 2). The default is a 400 px JPEG; Hardcover prefers larger images and rejects WebP. `?raw=1` would
   return the original but in any format. The creator now applies Hardcover's documented rules: PNG and JPEG only, decided from
   the bytes, at most 15 MiB (the largest size Hardcover documents; it documents no hard maximum), dimensions not enforced.
   Not verified against the real API. The app uploads no cover for now, so this applies to the `edition` and `image-tool` commands only.
6. **`edition_format` from an ASIN** (informational). `Audible Audio` is kept for parity with the mismatch export, but Hardcover
   stores it as `Audible`. Its draft FAQ says the field should describe the edition and is usually blank.
7. **Audnex for ebooks** (step 3, minor). An ebook's Kindle ASIN still triggers up to two Audnex calls (sharing one 15 s cap) that are not expected to
   resolve; the draft could skip Audnex for ebooks.
8. **Token scope** (every step, confirmed). Creating an edition needs `write:catalog:append`; see the permissions bullet in section 3 and the plan's token-scope items.
9. **Exact-match people and publisher lookups** (steps 4 and 7). A name that differs by a space or a period matches
   nothing; a narrator needs an earlier `Narrator` credit; `canonical_id` is ignored; and no ordering is requested. When
   no author matches, create returns 422 and the step 7 UI has no way to supply one. Decide between a UI path for entering a
   Hardcover author ID or search, a clear "cannot create" state, or a looser lookup (an unverified query, because the
   hosted API disables some operators; see `AGENTS.md`). Do not move this lookup back into the draft.
