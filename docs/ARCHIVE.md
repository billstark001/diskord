# Archive API, CLI, and exports

These interfaces read locally observed entities. They do not fetch Discord history, download missing attachments, change capture settings, issue a CA, or replay traffic. QUIC/HTTP/3 interception remains unsupported; the existing explicit TCP proxy and `--disable-quic` launch behavior are unchanged.

## Commands

Run commands with the same working directory and configuration as the instance. Relative `runtime_dir` paths resolve against the process working directory, including for read-only commands. The configuration loader does not create directories, initialize tokens, change permissions, or load the CA private key.

```sh
./diskord status
./diskord guilds                    # Servers in shared navigation order
./diskord dms                       # Private conversations, latest activity first
./diskord channels                  # All observed channels, including threads
./diskord channels --guild 123456789012345678
./diskord threads --guild 123456789012345678
./diskord messages --thread-name 'general' --guild 123456789012345678
./diskord export --thread-name 'general' --since 2026-10-01 --until 2026-10-07 --output ../week.md
./diskord messages --thread 234567890123456789 --limit 100
./diskord messages --thread 234567890123456789 --after 345678901234567890

./diskord export --thread 234567890123456789 --since '2026-10-01T00:00:00+09:00' --until '2026-10-08T00:00:00+09:00' --format md --output ../thread.md

./diskord export --thread 234567890123456789 --format jsonl --attachments --output ../thread.zip
```

On Windows use `.\diskord.exe`; flags and formats are the same. For example:

```powershell
.\diskord.exe export --thread 234567890123456789 --format jsonl --attachments --output '..\thread.zip'
```

`status`, `guilds` (`servers` alias), `dms`, `channels`, `threads`, and `messages` emit JSON to stdout. `export` emits Markdown by default; `--format jsonl` selects JSON Lines. Online text exports can write to stdout by omitting `--output` or using `--output -`. Attachment exports always produce ZIP and require a file output. `--config PATH` is supported on all commands.

An omitted `--thread` selects all local messages; the flag accepts any channel ID, including a DM or ordinary channel, not only Discord thread channel types. Explicit empty CLI selectors (`--thread`, `--thread-name`, `--guild`, `--scope`) are rejected. Thread/channel IDs and message cursors are decimal strings, never JSON numbers. `channels` includes placeholders and deleted channels; `--guild` filters by the observed guild relationship. Unknown relationships are not inferred remotely.

`--thread-name` performs an exact, case-sensitive match against the observed display name, including recipient-derived private-conversation names. It is mutually exclusive with `--thread`. An ambiguous name fails before export preparation and lists every matching channel/thread ID together with its server ID/name or private-conversation classification. Specify `--thread ID` to disambiguate, or narrow the search with `--guild ID` / `--scope dm`. Category names are not eligible for name lookup. No match is an error; there is no fuzzy or remote search.

`--guild ID` and `--scope dm` also filter message queries and exports and are mutually exclusive. `--include-threads` includes recursively observed child channels/threads of the selected channel, using either ID or resolved name; it requires a channel selection. Without it, selection remains exact. `threads` lists Discord thread types 10, 11, and 12, optionally scoped to a server. `channels` includes categories and other channel types. Listing responses preserve order and include IDs, names, parent IDs, server IDs, and observed channel positions.

All listing commands and browser navigation share a snapshot query. Guild order uses observed plain-JSON `user_settings.guild_folders` (flattened folder contents) or `guild_positions`, including later `USER_SETTINGS_UPDATE` events. Guilds without observed positions follow by numeric ID, not name. The project does not decode protobuf user-settings blobs; when a client supplies only those, the personalized server order is unknown rather than inferred from READY's guild array. Private conversations use the newer of stored `last_message_id` and the latest observed message, descending. Guild channels follow the sidebar hierarchy: uncategorized roots, categories by position, then children by position, with numeric-ID ties. Forum threads follow their parent's default pinned/activity/creation ordering; a temporary user-selected forum sort or search is not a persistent CLI order. Missing positions/activity cannot reproduce unobserved client ordering.

## Browser export

Right-click a server, private-message entry, channel/category, forum post, or message with an observed channel to choose **Export messages…**. The message view's top export button provides the same dialog without a pointer menu. Keyboard users can invoke the context menu with Shift+F10 or the context-menu key; menus support arrows, Home/End, Escape, and focus return. Native modal dialogs contain focus, make the background inert, and support Escape/backdrop dismissal.

The dialog selects the clicked scope or the whole local archive, optional local start/end times, Markdown/JSONL, optional child threads, and cached attachment ZIPs. Categories and forums include child threads by default. Blank times are unbounded; nonexistent local times caused by daylight-saving or skipped-date transitions are rejected instead of shifted. The current search query does not constrain export. A console-session-authenticated `GET /api/export` route invokes the same exporter; no API token is disclosed to the browser and the bearer API remains separately authenticated. Errors remain in the dialog; canceling aborts preparation/download. Successful files go through the browser's download flow. The browser buffers the response as a Blob before saving, so very large exports should use the streaming CLI.

## Time and pagination

CLI `--since` is inclusive and timed `--until` exclusive: `[since, until)`. Both are optional. The CLI accepts `YYYY-MM-DD`, local `YYYY-MM-DDTHH:MM[:SS[.fraction]]` (a space may replace `T`), and RFC3339 timestamps with an explicit timezone. Omitted timezones use the CLI process's local timezone; explicit offsets take precedence. Date-only `--since` starts at 00:00:00 and date-only `--until` includes that whole date through 23:59:59 and its fractional seconds, represented internally by the next local midnight as an exclusive bound. Calendar-day arithmetic preserves daylight-saving transitions; nonexistent local times are rejected, while ambiguous fall-back times use Go's local-time resolution. Date-only bounds also reject nonexistent local midnights, including a next-day midnight used for `--until`; use an explicit RFC3339 bound for such transitions. Omitting both bounds remains unbounded. Values are normalized to UTC, with nanosecond precision retained. Equal or reversed normalized intervals are rejected. The HTTP API retains strict RFC3339 timestamps with explicit timezones; CLI convenience forms are normalized before API calls.

Filtering uses a message's stored creation timestamp. If it is absent or invalid, the creation time is derived from a valid unsigned 64-bit Discord snowflake. The JSON fields `creation_time` and `time_source` (`timestamp`, `snowflake`, `unknown`) make this explicit. A record whose creation time cannot be established is included in unbounded queries and excluded from bounded queries. Original `created_at` and `edited_at` values are retained separately.

Messages are ordered by numeric message ID, ascending, using zero-padded SQLite sort keys. `messages --limit` accepts 1..1000 and defaults to 100. Pass the returned `next_after` to `--after`, preserving the same selection, until the field is absent. Each page is internally consistent, but separate page requests can see newer database states. The export command has no page truncation and reads every matching page from one database snapshot.

This cursor is for browsing messages. It is not a change feed: edits or deletions of older IDs cannot be discovered through an ID-only incremental cursor. `observed_at_ms` is the latest local observation time, not the first capture time or an edit-history sequence.

## Online API and authentication

The API shares the loopback console listener, normally `http://127.0.0.1:3900`. Starting the updated binary creates a separate private `state/api-token`. `diskord api-token` explicitly prints that existing token and never creates it. The CLI reads it directly and sends it in an `Authorization: Bearer TOKEN` header. It does not use console cookies, the administration token, URL credentials, environment proxies, or redirects. The administration token does not authorize the versioned API, and the read-only token does not authorize administration.

| Method and path | Parameters | Result |
| --- | --- | --- |
| `GET /api/v1/status` | None | Output schema version, binary version, database schema, entity counts |
| `GET /api/v1/navigation` | None | Ordered `guilds` and `channels` for listing and exact-name resolution |
| `GET /api/v1/channels` | Optional `guild` | All observed channels or the selected guild's channels |
| `GET /api/v1/messages` | Optional `thread`, `guild`, `scope`, `include_threads`, `since`, `until`, `after`, `limit` | Message page with `selection`, `messages`, and optional `next_after` |
| `GET /api/v1/export` | Optional `thread`, `guild`, `scope`, `include_threads`, `since`, `until`, `format`, `attachments` | Markdown/JSONL download, or ZIP when `attachments=true` |

`format` defaults to `md` and accepts `md` or `jsonl`. `attachments` defaults to false. Each export request generates a new snapshot and delivers the complete file; HTTP range and conditional headers are ignored, so downloads cannot be resumed across requests. Encode query values, especially timezone `+` characters, using a URL query encoder. For example, this Python snippet uses only the standard library:

```python
import json
from pathlib import Path
from urllib.parse import urlencode
from urllib.request import ProxyHandler, Request, build_opener

token = Path("state/api-token").read_text().strip()
query = urlencode({"thread": "234567890123456789", "since": "2026-10-01T00:00:00+09:00"})
request = Request("http://127.0.0.1:3900/api/v1/messages?" + query, headers={"Authorization": "Bearer " + token})
with build_opener(ProxyHandler({})).open(request, timeout=20) as response:
    page = json.load(response)
```

The existing loopback peer, Host, Origin, Fetch Metadata, and no-store checks still apply. No CORS access is added. Mutating methods are not provided on these routes. Selection endpoints reject unknown or repeated query parameters. Invalid filters produce HTTP 400, missing or incorrect authentication 401, and boundary violations 403. One export is admitted at a time; concurrent exports receive 429 with `Retry-After: 5`. Query/preparation failures return 503 before file delivery.

Output `schema_version: 1` describes the archive interface, independently of SQLite schema version 5 and the binary's application version. Message fields include IDs, current observed names, nullable body/timestamps/type, deletion and revision state, source, attachment metadata/cache hashes, reactions, and observed mention names. Reaction user names are incomplete observations, not a member directory or a complete reaction-user list.

## Export formats and attachments

Markdown contains selection metadata, scope limitations, author/time/message headers, verbatim message text in fences, deletion/edit markers, attachment metadata or local links, reactions, and mention-name mappings. The fences adapt to backticks in messages, so stored HTML and Markdown image syntax remain text instead of loading remote resources. JSONL is the lossless representation of the selected stored message fields; Markdown is a reading format.

JSONL contains a `type: "manifest"` record, zero or more `type: "message"` records, then a `type: "summary"` record. The summary includes `messages`, `attachment_files`, `missing_attachments`, and `complete: true`. Completeness here means the selected **local export finished**, not that Discord history was captured completely. Consumers should require the terminal summary to recognize truncated streams. Nullable fields distinguish unobserved values from empty text.

With attachments enabled the ZIP contains:

```text
messages.md                 # Or messages.jsonl
manifest.json               # Manifest plus the terminal export summary
attachments/<sha256>.<ext>  # Only referenced cached attachment images
```

Files are deduplicated by cache hash and stored without recompressing their image bytes; text entries use ZIP compression. Original attachment filenames are metadata, not filesystem paths. `export_path` points to a safe relative ZIP member. Cached bytes are checked against their SHA-256 hash while packaging. Missing, unsupported, or unavailable cached attachments remain listed in each message without an `export_path`; `missing_attachments` counts those attachment references. Cached avatars and inline emoji are not bundled. There are no CDN requests.

An export preserves the latest locally observed state, including deleted-message tombstones and retained content. It cannot reconstruct unobserved messages, previous edit versions, historical author/channel names, reply references, embeds, or files that were never cached. A known thread's reply-count snapshot does not establish message completeness.

## Offline source protection and output lifecycle

Add `--offline` to `status`, `guilds`, `dms`, `channels`, `threads`, `messages`, or `export` to read a stopped archive without a running API or selected CA. Offline export requires an explicit file output. Commands acquire an existing runtime lock read-only, open SQLite with `mode=ro`, `immutable=1`, and `query_only`, and never initialize or migrate. The archive must have schema version 4 or 5 and an existing `state/diskord.lock`. An active lock or nonempty WAL causes rejection. Use the online API for a running instance; do not delete its WAL or force immutable access to it. A source with a nonempty WAL must be cleanly shut down using the normal application lifecycle.

Explicit file outputs must be outside the source runtime, their parent directory must already exist, and existing files are never overwritten. Symlinked parents are resolved before checking this boundary. Outputs use private Unix permissions or protected Windows ACLs. An interrupted transfer removes its partial output when normal error/cancellation cleanup can run; abrupt termination can leave it.

Online preparation stages sensitive files below `runtime/tmp/`. Offline preparation uses a private temporary directory beside the selected output, leaving source files, permissions, and SQLite sidecars unchanged. These explicit export outputs and offline staging are exceptions to the runtime-only write rule for normal capture. Temporary export directories are removed on success, error, or normal cancellation. Service shutdown cancels active request contexts to release export snapshots promptly. An OS crash or forced kill can leave private staging data; there is no startup garbage collector.

Preparation uses one SQLite read transaction and paged queries, then releases the snapshot before ZIP packaging and HTTP delivery. Slow downloads do not pin the SQLite WAL. Long preparation can still retain WAL pages while capture continues. Online preparation has a one-minute limit, file delivery a five-minute deadline, and CLI operations a six-minute limit. Exports are limited to 8 GiB of message and attachment input, 8 GiB of ZIP output, and 100,000 unique attachment files. These limits fail the export instead of silently omitting records.

Tests use synthetic temporary runtimes. Never point builds, fixture tests, or proxy acceptance runs at a user's existing `run/` instance. Export files contain the selected archive data and are not encrypted by diskord.
