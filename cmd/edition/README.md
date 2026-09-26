# Edition Creation Tool

The standalone `edition` command provides `prepopulate` templates and explicit
edition creation. Audiobooks use a region-qualified Audible import; ebooks use
Hardcover's format-aware edition insertion. Normal sync does not create
catalogue editions.

## Requirements

Build and run the command locally; the published Docker image contains only
the main `audiobookshelf-hardcover-sync` service.

```bash
go build -o edition ./cmd/edition
```

The command reads `config.yaml` by default or the file passed with `--config`.
Set `hardcover.token`. When associating an Audiobookshelf item, also set
`audiobookshelf.url` and `audiobookshelf.token`. Supported environment values
override the file; relevant names include `HARDCOVER_TOKEN`,
`AUDIOBOOKSHELF_NETWORK_TRUST`, `AUDIOBOOKSHELF_AUDNEXUS_REGION`, and
`SYNC_STATE_FILE`.

The Hardcover token needs `read:catalog` for book/edition reads and duplicate
checks, plus `write:catalog:append` or a broader catalogue-write scope for
creation. Audiobook imports use `upsert_book`; ebook creation uses
`insert_edition`. A missing catalogue-write scope leaves sync available but
makes the explicit create operation fail.

## Commands

```bash
./edition --config ./config.yaml prepopulate --book-id 12345 --output edition.json
./edition --config ./config.yaml create --input edition.json
./edition --config ./config.yaml create --input edition.json --abs-item-id li_123
./edition --config ./config.yaml create --input edition.json --abs-item-id li_123 --state-file ./data/sync_state.json
./edition --config ./config.yaml --dry-run create --input edition.json
```

The `--abs-item-id` and `--state-file` flags apply to `create`. An
`--abs-item-id` flag overrides `abs_item_id` in the input JSON. The state-file
flag overrides `sync.state_file`; its fallback is `./data/sync_state.json`.

## Audiobook input

The audiobook input needs a positive Hardcover `book_id` and a nonblank bare
ASIN. `reading_format` defaults to `audiobook`.

```json
{
  "book_id": 12345,
  "asin": "B00XXXYYZZ",
  "reading_format": "audiobook",
  "asin_region": "uk",
  "abs_item_id": "li_123"
}
```

`asin_region` is optional; `region` is an alias. If both are supplied they
must agree. A supplied region is confirmed with Audnex. Without one, the
command discovers a region by finding the requested ASIN in Audnex; it prefers
`audiobookshelf.audnexus_region` (default `us`) and does not assume a region
from the ASIN. An unknown region or failed lookup stops before the Hardcover
import.

For audiobook imports, the command uses only `book_id`, `asin`, region,
`reading_format`, and the optional ABS item ID. Other JSON fields are ignored,
so existing mismatch export fields remain acceptable.

## Ebook input

Set `reading_format` to `ebook` and provide the ebook metadata used by
Hardcover insertion. The input requires a positive `book_id`, a `title`, at
least one `author_ids` entry, and at least one ASIN or ISBN. If supplied,
`release_date` must use `YYYY-MM-DD` format. For example:

```json
{
  "book_id": 12345,
  "title": "Book Title",
  "subtitle": "Digital Edition",
  "asin": "B00XXXYYZZ",
  "isbn_13": "9781234567890",
  "author_ids": [1],
  "publisher_id": 10,
  "release_date": "2023-01-01",
  "edition_format": "Ebook",
  "reading_format": "ebook"
}
```

The ebook path retains duplicate checks within ebook editions. An edition
already present for the requested book is returned as `existing`; the command
does not resend its metadata. An existing edition associated with another
book is refused.

## Results and saved matches

The command prints a JSON result with `success`, `status`, `book_id`,
`edition_id`, `image_id`, and `reading_format`; it may also include
`image_error`, `existing`, `abs_item_id`, and `association_saved`. Audiobook
statuses are `loaded`, `created`, or `dry_run`. Ebook statuses are `existing`,
`created`, or `dry_run`.

When an ABS item ID is supplied, the command fetches the item before making a
Hardcover change and requires its format to match the input. After verifying
the Hardcover result, it saves a local match in the configured state file
while holding the state-file lock. If another sync holds the lock, the command
returns an error before contacting Hardcover. A local save failure after a
successful Hardcover operation is reported separately; retry with the same
input to reuse the result and save the match.

Without an ABS item ID, no match is saved. A `loaded` audiobook import without
a saved mapping remains unresolved by later syncs. Dry run performs no
Hardcover mutation and saves no match. It can still fetch the ABS item and
check Audnex when those steps are requested by the input.

## Audiobookshelf URL trust

`audiobookshelf.network_trust` (or `AUDIOBOOKSHELF_NETWORK_TRUST`) controls
which addresses this command may contact for Audiobookshelf requests.
`allow_private` is the default for self-hosted addresses. `public_only`
requires HTTPS and public addresses. Both modes require an absolute
`http://` or `https://` base URL; invalid URLs or trust values stop the
command. See the [main configuration reference](../../README.md#configuration-reference)
for the deployment-wide setting.

For ebook creation, an `image_url` is not fetched or uploaded; the result may
include `image_error` for the unsupported cover upload.
