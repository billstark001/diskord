# Architecture and Data Contract

## Process and data paths

```text
Chrome / Discord (explicit proxy configuration by the user)
               |
     127.0.0.1:3901 shared loopback entry point
               |
      +--------+-------------------+
      |                            |
Target API / optional CDN       Gateway CONNECT
      |                            |
go-mitmproxy in same process    Local TLS termination
Temporary loopback backend     Bidirectional streaming relay via gorilla/websocket
      |                            |
Read only successful           Observe only server -> client
allowlisted responses
      |                            |
Bounded HTTP queue /           Bounded byte queue / continuous
 decompression                 decompression / JSON or ETF framing
      +------------+---------------+
                   |
          Explicit field projection -> model.Batch
                   |
             Bounded single-writer queue
                   |
           main.sqlite / resources/
                   |
       embedded Preact console + local JSON APIs
```

The console selects `zh-CN` or `en` from a local preference cookie or `Accept-Language`. A CSRF-protected JSON request changes the preference. The Go server exposes same-origin JSON endpoints for sessions, observed navigation, messages, status, settings, and CA operations. Separate versioned `/api/v1/` routes expose archive status, channels, message pages, and downloads using a read-only bearer token. The Preact frontend keeps route and scroll state client-side and requests only the local archive. No Discord API requests are made by the console or archive interfaces. Vite compiles the frontend into `internal/ui/webdist/`, which Go embeds into a single binary.

The non-target CONNECT branch is an undecrypted tunnel and does not enter either capture branch. The HTTP backend's random loopback port does not imply a separate process or deployed service; it remains inside the same binary. The upstream library interface accepts a listen address, not an already-bound listener, so there is a race between choosing a free port and startup. Startup verifies identity through a random health-check path and response; a conflict should fail startup rather than connect to another local service by mistake.

`go-mitmproxy` is pinned to v1.9.3 with a verified HTTP interface. The implementation does not depend on a WS hook from an unpinned main branch. Gateway is implemented separately to structurally guarantee that there is no client-frame capture entry point or full traffic-history list, while providing bounded memory, directional isolation, continuous compression dictionaries, and explicit failure states. It is neither a second process nor a replacement for the required proxy library.

## HTTP

`Requestheaders` always enables streaming mode and buffers no request body. Only when the response is 2xx and the domain, path, and Content-Type all match does `StreamResponseModifier` wrap it in a bounded tap; the forwarding reader itself is not rewritten. Data is submitted only after a successful read to EOF. Interrupted responses and responses over capacity do not submit partial content.

Response decompression supports identity, gzip, deflate, brotli, and zstd, with limits on both compressed input and decompressed output size. There are 8 HTTP observation slots, 8 queue slots, and 2 decompression workers. HTTP metadata entering a capture object consists only of target host, canonical path, MIME type, and encoding; it contains no header map, query string, or request body.

REST entry points cover user profiles, guild/channel profiles and lists, active and archived thread lists, forum thread creation responses, message lists and individual messages, and nested results from message search. Strict path matching excludes auth, webhook-token paths, and other unlisted endpoints. It does not dump everything under `/api/`.

## Gateway

Gateway CONNECT verifies that authority and SNI agree and uses normal TLS certificate verification upstream. The handshake explicitly carries necessary Origin / User-Agent / Accept-Language fields, without forwarding cookies or Authorization. The Discord Gateway client's authentication payload passes through as client WS data.

Forwarding uses `NextReader/NextWriter` and a 32 KiB buffer; the client-direction observer is nil. The server-direction observer has only 64 byte-block slots, each at most 32 KiB. Decoded events have a separate size limit. The zlib/zstd reader persists for the lifetime of the connection instead of reinitializing its dictionary for each message. The JSON framer handles concatenated objects, nested structures, escaped characters, and whitespace between objects. The ETF decoder reads one bounded versioned term at a time with limits on nesting and collection sizes.

The client's original URL parameters determine WS compression and encoding; `encoding` and `compress` are not modified. A bounded ETF subset is decoded into the same explicitly projected events as JSON; unknown ETF tags and encodings are not parsed, and there is no automatic “force JSON” feature. gorilla handles WS control frames, close reasons are not archived, and the proxy may change link-layer ping/pong/close behavior. Application messages are forwarded in their received direction. This is not a byte-for-byte transparent TLS/WebSocket packet replayer.

Once the observation queue exceeds its limit or decoding fails, parsing a compressed stream after missing bytes with a fresh dictionary would be invalid. Observation therefore stops for that connection while forwarding continues. The dashboard shows counters for lost synchronization, unsupported encodings, and decode errors. The forwarding layer also has a hard limit of 128 MiB per WS message; exceeding it interrupts the connection. This differs from the capture layer's “skip but continue” behavior.

## Entities and relationships

Primary entities: `users`, `guilds`, `channels`, and `messages`.

Relationships: channels belong to guilds, child channels/threads belong to parent channels, messages belong to channels, and message authors are users. `guild_members` stores observed membership edges, while `channel_recipients` stores observed DM recipient edges. These are not complete member directories, and full relationship cleanup after a member leaves is not implemented.

A user/guild/channel can be completed when its name arrives after its ID. All snowflakes are stored as strings, with ordering based on fixed-width zero-padded strings rather than floating-point conversion. Unknown entities get ID placeholders to prevent foreign-key failures caused by event ordering. Guild membership is reconciled through parent channels after each batch and on schema upgrade. Only explicit DM and group DM channel types appear in the DM list; unresolved channels remain available through the global message archive. Explicit `parent_id: null` clears a prior category relationship, while an omitted parent field leaves it unchanged.

Observed channel `position` is nullable and retained for guild navigation. The console places uncategorized channels before categories, then uses `position` and snowflake ID within each group. Missing positions sort after known positions. Forum (`GUILD_FORUM`) and media (`GUILD_MEDIA`) channels are thread containers: their observed public threads appear as read-only post cards rather than a direct message timeline. Forum channel tags, layout and sort defaults, thread status/flags, and starter messages are projected into typed columns or relationship tables. Forum cards show observed starter attachments and reactions. The reply count uses the larger of the latest observed thread snapshot and locally observed replies, so it is not guaranteed to equal Discord's current count. The archive does not request missing threads or messages from Discord.

Gateway allowlist:

```text
READY / READY_SUPPLEMENTAL
GUILD_CREATE / GUILD_UPDATE / GUILD_DELETE
CHANNEL_CREATE / CHANNEL_UPDATE / CHANNEL_DELETE
THREAD_CREATE / THREAD_UPDATE / THREAD_DELETE / THREAD_LIST_SYNC
MESSAGE_CREATE / MESSAGE_UPDATE / MESSAGE_DELETE / MESSAGE_DELETE_BULK
GUILD_MEMBER_ADD / GUILD_MEMBER_UPDATE / GUILD_MEMBERS_CHUNK
USER_UPDATE / USER_SETTINGS_UPDATE (ordered guild IDs only)
```

Unknown events are ignored. Fields use explicit projection; values such as `user.email`, `token`, and `session_id` do not enter a typed batch. READY yields only objects such as users, guilds, and channels; the entire READY payload is not persisted. Undisclosed first-party client variants require parser extensions based on testing.

Message edits use nullable fields to mean “leave the existing value unchanged when missing”; an explicit empty string can clear content. Revision order prefers `edited_timestamp`, then the message timestamp, and finally observation time if both are absent. This prevents common old HTTP snapshots from overwriting newer edits, but it does not prove a global causal order. Out-of-order events across connections and missing remote timestamps can remain ambiguous. Deletion sets a tombstone, and old events cannot revive the message; the last observed content is still retained. Previous edit versions are not stored.

`attachments`, `assets`, and `resource_urls` support attachment metadata and relationships to allowlisted cached images. Enabling caching starts a sequential, size-limited, unauthenticated retry of missing paths on approved CDN hosts. Query strings and fragments are stripped from signed URLs, so replay credentials are not stored; expired signed attachments require a fresh client request. `reaction_totals` stores observed counts and `reaction_users` stores identities observed in Gateway events or reaction-user responses. Historical counts do not imply that every reactor name is known.

Message bodies are rendered in the console with `markdown-it`, with raw HTML disabled. A text-rendering rule resolves the existing observed mentions, cached custom emoji, and standard emoji shortcodes; code spans and blocks retain their literal content. Markdown image syntax does not trigger remote image requests. Attachment previews continue to use authenticated local cache URLs. This is a Markdown presentation layer, not a stored HTML copy of a message.

The overview API exposes the running binary version and SQLite schema version. The console formats message, forum, and certificate timestamps in the browser's local timezone. Message actions copy the raw stored text or a Discord channel/thread link; links are omitted when a channel has not yet been classified as a guild channel or DM. Double-clicking an all-channel result navigates to its source channel or thread and centers that message. Message pages expose cursors in both directions when a cursor bounds the page, allowing periodic infinite-query refetches to rebuild pages after long backward scrolling.

## SQLite and shutdown

The application uses a pure Go SQLite driver, WAL, foreign keys, a 5-second busy timeout, FULL synchronization, and in-memory temporary tables. There is one write connection and a pool of four `query_only` read connections. Each entity batch is a transaction. Resource files are written atomically before their index entries, so a crash may leave orphaned cache files. Startup quota accounting still includes these files, preventing silently unbounded duplicate usage.

Shutdown first stops external access and closes hijacked connections, then cancels active console requests and closes the console/internal proxy, stops producers, drains the HTTP and entity-write queues, checkpoints/closes SQLite, and releases the file lock. Entity writes have a 30-second drain window; after timeout, remaining writes are canceled and counted. `run-bg` starts the same server as a detached process in the current runtime directory; `stop-bg` uses a private token to request shutdown through the loopback console and waits for the lock to be released. Sensitive batches awaiting a write are not staged in the system temporary directory.

There is no measured concurrency or memory-stress report for this implementation. Event limits apply at event boundaries and are not a strict limit on process RSS. A limit of 128 concurrent Gateway connections and large batches can consume substantial memory. Initial use should keep the number of clients small and use the default queues while monitoring the dashboard.

## Archive interfaces and export snapshots

`internal/store/archive.go` implements the common archive message query used by the versioned API and offline CLI. Message fields and enrichment are read in one SQLite transaction per page request or complete export. All IDs are strings; nullable stored message fields retain their distinction from empty content. Timestamp filtering parses RFC3339 in Go to retain offsets and nanosecond precision; it scans ordered candidates instead of using SQLite's rounded date functions. Missing/invalid timestamps fall back to unsigned 64-bit snowflake creation time. Bounds are `[since, until)` and concern message creation, not observation or edit time. No time index is added. Normal startup migrates schema 4 to 5 with a typed guild-position table; offline access accepts schemas 4 and 5 without migrating.

`internal/archive` stages Markdown or JSONL below `runtime/tmp/`, recording scope limitations and a terminal local-completion summary. It releases the transaction before optional ZIP packaging and HTTP delivery. `documents.go` manages whole-message Markdown splitting and scope/date filenames; split documents and attachments share one ZIP with final document counts and sizes in its manifest. Attachment ZIP members use validated content-hash filenames, include only referenced cached images, and verify their hashes. Missing references are recorded without network requests. JSONL contains manifest/message/summary records and preserves stored line endings; Markdown fences normalize body line endings and preserve text without rendering remote image syntax or raw HTML. Optional Discord source links require observed channel ownership. Candidate queries omit bodies from sorting, pages account for JSON escaping within their approximate payload budget, and Markdown bodies stream through bounded writes. Exported data remains the latest observed state, with no historical revision log or persisted change feed.

The online CLI uses the same API and a separate private `state/api-token`; it does not use the console session or administration token. The API retains the console's loopback, Host/Origin, and no-store boundary checks. One export is admitted at a time, with bounded preparation, input bytes, and attachment counts. Temporary artifacts are removed after normal success, error, or cancellation.

Offline commands load configuration without preparing a runtime, hold an existing runtime lock read-only, and open only the supported SQLite schema with `mode=ro`, `immutable=1`, and `query_only`. A nonempty WAL is rejected so immutable access never silently skips captured data. Offline exports stage privately beside an explicit new output file outside the source runtime; source files, permissions, and sidecars are unchanged. See [ARCHIVE.md](ARCHIVE.md) for the contract and limits.

`internal/store/navigation.go` provides one snapshot and shared hierarchy/order for the browser, bearer API, CLI lists, and name resolution. Explicit ordered guild IDs from plain-JSON settings are projected into `model.Batch` without other settings fields, folder names, tokens, or raw blobs. Unknown server order falls back to numeric ID; private conversations use observed activity, channels use positions, and forum threads use the default post order. Protobuf-only user settings are not decoded.

The frontend uses theme-aligned menu primitives and native modal-dialog focus/inert behavior. Export scope controls normalize browser-local times before calling a console-session-authenticated export route backed by the common query/export pipeline; the read-only API token never enters frontend state. API timestamps stay strict, while the CLI normalizes date-only and local-time forms and resolves exact names through the shared navigation query before selecting an ID.
