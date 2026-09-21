# Field Crosswalk: Audiobookshelf Item to Hardcover Edition

Companion to [needs-review-edition-creation.md](needs-review-edition-creation.md). That document says *what* the
"add an edition to Hardcover" feature does and how it is split into steps; this one says, field by field, what
Audiobookshelf (ABS) data ends up in which Hardcover edition field and what is done to it on the way.

**Status: 🚧 DRAFT** (2026-09-20). Documentation only; no code changes.

## Verified against

| Source | What was read |
|--------|---------------|
| Our code | Branch `step_4_needs_review_add_edition` (tip `7ce35cd`, which contains steps 1-3). Functions are cited by name, not line, because the code is not on this branch. |
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
mismatch.Collector.AddWithMetadata(...)   Audnex release date (ASIN), publisher-name lookup,        (step 1)
  |                                       ISBN split, defaults (language 1, country 1)
  v
BookMismatch.ToEditionExport(...)         author and narrator ID lookups, edition_format,           (step 1)
  |                                       edition_information, audio_seconds, cover preference
  v
draft.Draft  ->  GET .../edition-draft    editable preview, plus display names and warnings          (step 3)
  |
  v  user edits, then POST .../edition
multiuser.validateEditionInput            trim title, normalize ISBNs, require an identifier         (step 4)
  |                                       and an author, bound the ID lists
  v
edition.Creator.CreateEdition             duplicate lookups, insert_edition, then the cover chain    (step 2)
```

Two fields never come from the client: `book_id` (from the run record's `hardcover_book_id`) and the cover URL (rebuilt
from the profile's ABS base URL). The reading format is also decided on the server, from the ABS item fetched again
at create time.

## 2. Source: what ABS returns

The draft uses `GET /api/items/{id}?expanded=1`. On the ABS server, *expanded* is built as the minified book
JSON plus extra fields (`toOldJSONExpanded` spreads `toOldJSONMinified`), so `numTracks`, `duration` and
`ebookFormat` are present in it, and so are the full `authors[]` and `narrators[]` arrays.

Fields relevant to an edition (`media` is the book; `media.metadata` is its metadata):

| ABS field | Type | Decoded by `models.AudiobookshelfBook`? | Notes |
|-----------|------|------------------------------------------|-------|
| `id` | string | yes (`ID`) | Library item ID, for example `li_8gch9ve09orgn4fdz8`. Used for the cover URL. |
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
| `media.metadata.publishedDate` | string or null | **no** | Free-form date string. |
| `media.metadata.isbn` | string or null | yes | One field. It may hold an ISBN-10 or an ISBN-13, hyphenated or not. |
| `media.metadata.asin` | string or null | yes | |
| `media.metadata.language` | string or null | yes | Free text such as `"English"`. Not used today. |
| `media.metadata.abridged` | boolean | **no** | Present in the server source (all three metadata shapes) but not in the API docs sample. |
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
- **`edition_format`** is free text. Hardcover's Editing FAQ (a draft page) says it elaborates on the edition (for
  example "Full cast audiobook"), describes the edition rather than where it was obtained, and is usually left blank.
  `reading_format_id` is the required field on Hardcover's own form.
- **`release_date`** is a `date` scalar: `YYYY-MM-DD`. Hardcover's Edition Standards say to use January 1 of the year
  when only the year is known.
- **`language_id`, `country_id`, `publisher_id`** are foreign keys (`languages`, `countries`, `publishers`), not names.
  `languages` has `language`, `code2`, `code3`.
- **Identifiers**: Hardcover strips hyphens from ISBNs itself. `isbn_10_valid`/`isbn_13_valid` are computed on their
  side. Their ISBN guide warns that adding or removing `978` does not give a valid number, because the check digit
  must be recalculated (`internal/isbn` does recalculate it).
- **Cover**: `image_id` is set on a *second* call. The tool uploads the file to Hardcover's storage
  (`POST https://hardcover.app/api/upload/google`, then a multipart POST to the returned URL), calls `insert_image`,
  then `update_edition(id, {dto: {image_id}})`. Hardcover's Edition Standards: PNG and JPEG work best, **WebP is not
  supported**, and larger is better (at least 300x450 for the top quality tier).
- **Permissions** (unverified): Hardcover's `capabilities.json` lists `insert_edition`, `insert_image` and
  `update_edition` under the `write:catalog`, `write:catalog:append` and `write:catalog:edit` scopes. Whether an
  ordinary user token carries one of them has not been checked.
- **Not settable on an edition**: description, series, genres and tags are book-level (`BookDtoType`), and there is no
  physical-format field in `BookDtoInput`.

## 4. The crosswalk

"Step" is the step whose code owns the logic. **UI** means the field appears as an editable input in the step 7
preview; **API** means the POST body accepts it (`EditionEdits`); **server** means the client cannot set it.

| # | Hardcover field | ABS source | Transformation and logic | Step | Edit |
|---|-----------------|------------|--------------------------|------|------|
| 1 | `book_id` (mutation argument) | none; the run record's `hardcover_book_id` | Integer greater than 0, else the book is not eligible. Overrides any candidate the enrichment guessed. | 3, 4 | server |
| 2 | `title` | `metadata.title` | Passed through. Create trims it and rejects a blank one. No series or prefix clean-up. | 3, 4 | UI, API |
| 3 | `subtitle` | `metadata.subtitle` | Passed through; omitted when empty. | 3 | UI, API |
| 4 | `asin` | `metadata.asin` | Trimmed only. No case or shape check. Also triggers the Audnex lookup (row 8) and sets `edition_format` (row 11). | 3, 4 | UI, API |
| 5 | `isbn_13` | `metadata.isbn` | Normalized (separators removed, trailing `x` uppercased), classified by shape. 13 digits goes here; a 10-character value goes to row 6. The draft then fills the *other* form when it can be derived. | 1, 3, 4 | UI, API |
| 6 | `isbn_10` | `metadata.isbn` | As row 5. The ISBN-10 to ISBN-13 conversion adds `978` and recalculates the check digit; a 979 ISBN-13 has no ISBN-10. A counterpart is derived only when the input's own checksum is valid. | 1, 3, 4 | UI, API |
| 7 | `contributions[]`, author (`contribution: null`) | `metadata.authorName` | Split on `,`, each name trimmed, empty names dropped. Each name is looked up on Hardcover by **exact, case-sensitive name** among active authors; the first row returned is used. IDs are de-duplicated and keep ABS order. No match on any name means zero authors, which is a draft warning and a create failure. | 1, 3, 4 | API |
| 8 | `release_date` | Audnex `releaseDate` (needs `asin`), else `metadata.publishedYear` | Audnex is asked in the profile's configured region and then `us` (once, with no region, when none is configured). The result is normalized to `YYYY-MM-DD` (ISO 8601 with a time, RFC 3339, several slash and month-name layouts). A year alone becomes `YYYY-01-01`. `metadata.publishedDate` is not read. Create requires `YYYY-MM-DD`. | 1, 3, 4 | UI, API |
| 9 | `contributions[]`, `"Narrator"` | `metadata.narratorName` | Same split as row 7. Lookup is by exact name among active authors **that already have a `Narrator` contribution**. Audiobook only; an ebook draft passes no narrator. No match is a warning; the edition is created without a narrator. | 1, 3, 4 | API |
| 10 | `publisher_id` | `metadata.publisher` | Looked up by **exact, case-sensitive** publisher name. Not found gives `0`, which is omitted (warning). Never defaults to a guessed ID (was `1` before step 1). | 1, 3, 4 | API |
| 11 | `edition_format` | `metadata.asin`, `metadata.publisher`, item format | Audiobook: an ASIN gives `Audible Audio`; else a publisher containing "libro" gives `libro.fm`; else empty, and the creator sends `Audiobook`. Ebook: `Ebook`. Trimmed, at most 100 characters. | 1, 2, 3, 4 | API |
| 12 | `reading_format_id` | `mediaType`, `ebookFile`, `ebookFormat`, `duration`, `numTracks` | `IsEbook()` gives 4, otherwise 2. Recomputed from ABS at create time; the request cannot set it (an extra `reading_format` field is rejected with 400). | 1, 2, 4 | server |
| 13 | `audio_seconds` | `media.duration` | `int(duration + 0.5)`. Sent only when greater than 0 and the item is an audiobook. | 1, 3, 4 | API |
| 14 | `edition_information` | none | Audiobook: `Unabridged`. Ebook: empty (omitted). The mismatch record's own placeholder `Audiobookshelf` is discarded. `metadata.abridged` is not consulted. | 1, 3, 4 | UI, API |
| 15 | `language_id` | none (`metadata.language` is ignored) | Constant `1` (assumed English). | 1, 3, 4 | API |
| 16 | `country_id` | none | Constant `1` (assumed United States). | 1, 3, 4 | API |
| 17 | cover: `insert_image`, then `update_edition {image_id}` | `media.coverPath` (existence only) | URL is `<ABS base>/api/items/<id>/cover`, credentials, query and fragment stripped; empty when the item has no cover. The creator downloads it (ABS bearer token only when the host matches the profile's ABS URL), uploads it to Hardcover, creates the image record, and attaches it. Any failure is a warning; the edition stays. | 2, 3, 4 | server |
| 18 | `page_count` | none | Not sent. ABS has no page count. | n/a | n/a |

### Field notes

**Rows 5 and 6, ISBN.** ABS has one `isbn` string; Hardcover has two fields. `isbn.Parse` accepts a value by shape only:
13 digits, or 9 digits followed by a digit or `X`. The draft first records only the form the item carries, then fills
the other with `isbn.ISBN10()/ISBN13()`, which returns the counterpart only for a valid checksum. A bad-checksum ISBN
is still sent as given (Hardcover will flag it `..._valid = false`); it has no counterpart. On create, each field is
re-normalized and must have the right length for its slot (`isbn_10 must be a valid 10-character ISBN`), and at least
one of `asin`, `isbn_10`, `isbn_13` is required.

**Rows 7 and 9, people.** ABS's `authorName` is `authors.map(name).join(', ')`, so a single author called
`"John Smith, Jr."` is indistinguishable from two authors once joined. The expanded item carries the exact
`authors[].name` and `narrators[]`, but the model does not decode them (finding 2). Person lookup details
(`SearchPeople` in the Hardcover client):

- The query is `authors(where: {state: {_eq: "active"}, name: {_eq: $name}})` (plus, for narrators, `contributions:
  {contribution: {_eq: "Narrator"}}`), limit 5, **no `order_by`**. `people.go` says it takes "the top result by book
  count", but no ordering is requested, so it takes the first row Hardcover returns.
- `canonical_id` is selected but never used, so a merged or duplicate author record could be picked.
- The narrator fallback query is identical to the first one, so a narrator who exists on Hardcover only as an author
  (no earlier `Narrator` credit) is never matched.
- A name made only of digits is first tried as a Hardcover person ID, then as a name.
- Results are cached for the life of the process, keyed by type and lower-cased name.
- Because a name must match exactly, "J.R.R. Tolkien" and "J. R. R. Tolkien" are different lookups, and a miss is
  silent apart from the draft warning.

**Row 8, release date.** Audnex is an Audible metadata service, so it only helps for Audible ASINs. Without a date from
Audnex the fallback is the year only, and the Hardcover librarian rule (January 1 when only the year is known) is
exactly what `YYYY-01-01` gives. Slash dates are read US-style (`01/02/2006` is tried before `02/01/2006`); that only
matters if `publishedDate` is ever used, because Audnex and the year are ISO.

**Row 11, edition format.** The audiobook labels come from `ToEditionExport`, which also produces the mismatch JSON the
`edition` CLI imports, so the draft matches what the file flow would have produced. This differs from the Hardcover
FAQ's advice to describe the edition rather than its source (finding 6).

**Row 17, cover.** ABS's cover endpoint scales the image: `width` defaults to 400, `height` to proportional, and
`format` to `webp` or `jpeg` depending on the request's `Accept` header (`reqSupportsWebp` is true when `Accept` contains
`image/webp` or equals `*/*`). The creator sends `Accept: image/*`, so ABS answers with a **JPEG about 400 pixels wide**,
which is acceptable to Hardcover but not large. `?raw=1` returns the original file in whatever format it has (possibly
WebP, which Hardcover does not support). The creator picks the upload extension from the response's `Content-Type`
(`jpg`, `png` or `webp`).

## 5. ABS fields that are not mapped

| ABS field | Why | By design or a gap |
|-----------|-----|--------------------|
| `description`, `descriptionPlain` | Description is book-level on Hardcover (`BookDtoType`), not in `BookDtoInput`. | By design |
| `genres`, `tags`, `series[]`, `seriesName` | Book-level on Hardcover (series, tags). | By design |
| `explicit` | No edition field. | By design |
| `chapters`, `audioFiles`, `tracks`, `size`, `libraryFiles`, `path`, timestamps | No Hardcover analogue. | By design |
| `titleIgnorePrefix`, `authorNameLF` | Sort helpers. | By design |
| `libraryId`, `folderId` | Only recorded in the mismatch export. | By design |
| `abridged` | Should inform `edition_information`. | **Gap** (finding 1) |
| `authors[]`, `narrators[]` | Exact names, instead of splitting the joined strings. | **Gap** (finding 2) |
| `language` | Should inform `language_id`. | **Gap** (finding 3) |
| `publishedDate` | Could come before the year fallback. | Gap, minor (finding 4) |

Hardcover `BookDtoInput` fields the tool never sends: `page_count` (no source), and `image_id` in the insert (it is set
afterwards by `update_edition`). `EditionInput.locked` is not sent.

## 6. Server-controlled and editable fields

| Field | Who decides | Detail |
|-------|-------------|--------|
| `book_id` | server | Run record; the request cannot retarget the edition. |
| cover URL | server | `draft.CoverURL(profile ABS URL, item)` at both draft and create. A client-supplied URL would be a way to send the ABS token elsewhere, so none is accepted. |
| `reading_format_id`, reading-format-dependent fields | server | `item.ReadingFormat()` on the item fetched at create time. |
| Everything else in `EditionEdits` | the client, validated | `title`, `subtitle`, `asin`, `isbn_10`, `isbn_13`, `release_date`, `edition_information`, `edition_format`, `audio_seconds`, `language_id`, `country_id`, `author_ids`, `narrator_ids`, `publisher_id`. At most 50 author and 50 narrator IDs, all positive; publisher, language, country and audio length not negative. |

The step 7 UI (planned) shows editable text inputs for title, subtitle, ASIN, ISBN-10, ISBN-13, release date and edition
information, and shows the resolved author, narrator and publisher names read-only. So a person whose author name did
not match on Hardcover cannot fix that in the UI, although the API would accept `author_ids` (finding 9).

## 7. Calls made

**Draft** (read-only): one ABS item fetch; if there is an ASIN, up to two Audnex requests (region, then `us`, each
retried up to three times, all sharing one 15 s cap); a publisher lookup (repeated in `ToEditionExport` when the first
found nothing); a handful of Hardcover reads from `AddWithMetadata`'s candidate
enrichment (ISBN or ASIN lookup, then a title and author search with up to five detail reads), whose results the draft
mostly discards; one lookup per author name and per narrator name. Lookups are cached per process.

**Create**: one ABS item fetch; up to three duplicate lookups (ASIN, ISBN-13, ISBN-10 and the converted forms,
de-duplicated, scoped to the item's reading format); `insert_edition`; then, if there is a cover, download, storage
credential request, upload, `insert_image`, `update_edition`. A duplicate on the same book is reused untouched; one
on a different book is refused (409).

## 8. Worked examples

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

Hardcover `insert_edition` variables, assuming Hardcover knows the three names exactly, and Audnex returns no date (with
a date, `release_date` is that date instead of `2008-01-01`):

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

The cover then goes through the four-step chain from `https://<abs>/api/items/li_8gch9ve09orgn4fdz8/cover`. `subtitle`,
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

## 9. Findings and open decisions

Each is also an unchecked item under the matching step in the plan document's "Step checklists".

1. **`abridged` is ignored** (step 1 export, step 3 draft). Every audiobook draft says `Unabridged`, even when ABS marks it
   abridged. Needs the model field and a decision (`Abridged`, or blank).
2. **Names are split from a joined string** (step 3). Use the exact `authors[].name` and `narrators[]` arrays from the
   expanded item instead of splitting `authorName` and `narratorName` on commas.
3. **`language` is ignored** (step 3). A non-English item is created as language 1. Minimum: a draft warning when ABS gives
   a language other than English. Better: look the language up in Hardcover's `languages` table. `language_id 1` and
   `country_id 1` are assumed to be English and the United States and have not been checked against Hardcover.
4. **`publishedDate` is unused** (step 3, minor). It could be tried before the year-only fallback.
5. **Cover** (informational). The default is a 400 px JPEG; Hardcover prefers larger images and rejects WebP. `?raw=1` would
   return the original but in any format. No change proposed; the owner decides.
6. **`edition_format` from an ASIN** (informational). `Audible Audio` is kept for parity with the mismatch export. Hardcover's
   draft FAQ says the field should describe the edition and is usually blank.
7. **Audnex for ebooks** (step 3, minor). An ebook's Kindle ASIN still triggers up to two Audnex calls (sharing one 15 s cap) that are not expected to
   resolve; the draft could skip Audnex for ebooks.
8. **Token scope** (every step, unverified). See the permissions bullet in section 3.
9. **Exact-match people and publisher lookups** (steps 3 and 7). A name that differs by a space or a period matches
   nothing; a narrator needs an earlier `Narrator` credit; `canonical_id` is ignored; and no ordering is requested. When
   no author matches, create fails and the step 7 UI has no way to supply one. Decide between a UI path for entering a
   Hardcover author ID or search, a clear "cannot create" state, or a looser lookup (an unverified query, because the
   hosted API disables some operators; see `AGENTS.md`).
