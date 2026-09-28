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

The console selects `zh-CN` or `en` from a local preference cookie or `Accept-Language`. A CSRF-protected JSON request changes the preference. The Go server exposes same-origin JSON endpoints for sessions, observed navigation, messages, status, settings, and CA operations. The Preact frontend keeps route and scroll state client-side and requests only the local archive. No Discord API requests are made by the console. Vite compiles the frontend into `internal/ui/webdist/`, which Go embeds into a single binary.

The non-target CONNECT branch is an undecrypted tunnel and does not enter either capture branch. The HTTP backend's random loopback port does not imply a separate process or deployed service; it remains inside the same binary. The upstream library interface accepts a listen address, not an already-bound listener, so there is a race between choosing a free port and startup. Startup verifies identity through a random health-check path and response; a conflict should fail startup rather than connect to another local service by mistake.

`go-mitmproxy` is pinned to v1.9.3 with a verified HTTP interface. The implementation does not depend on a WS hook from an unpinned main branch. Gateway is implemented separately to structurally guarantee that there is no client-frame capture entry point or full traffic-history list, while providing bounded memory, directional isolation, continuous compression dictionaries, and explicit failure states. It is neither a second process nor a replacement for the required proxy library.

## HTTP

`Requestheaders` always enables streaming mode and buffers no request body. Only when the response is 2xx and the domain, path, and Content-Type all match does `StreamResponseModifier` wrap it in a bounded tap; the forwarding reader itself is not rewritten. Data is submitted only after a successful read to EOF. Interrupted responses and responses over capacity do not submit partial content.

Response decompression supports identity, gzip, deflate, brotli, and zstd, with limits on both compressed input and decompressed output size. There are 8 HTTP observation slots, 8 queue slots, and 2 decompression workers. HTTP metadata entering a capture object consists only of target host, canonical path, MIME type, and encoding; it contains no header map, query string, or request body.

REST entry points cover user profiles, guild/channel profiles and lists, message lists and individual messages, and nested results from message search. Strict path matching excludes auth, webhook-token paths, and other unlisted endpoints. It does not dump everything under `/api/`.

## Gateway

Gateway CONNECT verifies that authority and SNI agree and uses normal TLS certificate verification upstream. The handshake explicitly carries necessary Origin / User-Agent / Accept-Language fields, without forwarding cookies or Authorization. The Discord Gateway client's authentication payload passes through as client WS data.

Forwarding uses `NextReader/NextWriter` and a 32 KiB buffer; the client-direction observer is nil. The server-direction observer has only 64 byte-block slots, each at most 32 KiB. Decoded events have a separate size limit. The zlib/zstd reader persists for the lifetime of the connection instead of reinitializing its dictionary for each message. The JSON framer handles concatenated objects, nested structures, escaped characters, and whitespace between objects. The ETF decoder reads one bounded versioned term at a time with limits on nesting and collection sizes.

The client's original URL parameters determine WS compression and encoding; `encoding` and `compress` are not modified. A bounded ETF subset is decoded into the same explicitly projected events as JSON; unknown ETF tags and encodings are not parsed, and there is no automatic “force JSON” feature. gorilla handles WS control frames, close reasons are not archived, and the proxy may change link-layer ping/pong/close behavior. Application messages are forwarded in their received direction. This is not a byte-for-byte transparent TLS/WebSocket packet replayer.

Once the observation queue exceeds its limit or decoding fails, parsing a compressed stream after missing bytes with a fresh dictionary would be invalid. Observation therefore stops for that connection while forwarding continues. The dashboard shows counters for lost synchronization, unsupported encodings, and decode errors. The forwarding layer also has a hard limit of 128 MiB per WS message; exceeding it interrupts the connection. This differs from the capture layer's “skip but continue” behavior.

## Entities and relationships

Primary entities: `users`, `guilds`, `channels`, and `messages`.

Relationships: channels belong to guilds, child channels/threads belong to parent channels, messages belong to channels, and message authors are users. `guild_members` stores observed membership edges, while `channel_recipients` stores observed DM recipient edges. These are not complete member directories, and full relationship cleanup after a member leaves is not implemented.

A user/guild/channel can be completed when its name arrives after its ID. All snowflakes are stored as strings, with ordering based on fixed-width zero-padded strings rather than floating-point conversion. Unknown entities get ID placeholders to prevent foreign-key failures caused by event ordering.

Gateway allowlist:

```text
READY / READY_SUPPLEMENTAL
GUILD_CREATE / GUILD_UPDATE / GUILD_DELETE
CHANNEL_CREATE / CHANNEL_UPDATE / CHANNEL_DELETE
THREAD_CREATE / THREAD_UPDATE / THREAD_DELETE / THREAD_LIST_SYNC
MESSAGE_CREATE / MESSAGE_UPDATE / MESSAGE_DELETE / MESSAGE_DELETE_BULK
GUILD_MEMBER_ADD / GUILD_MEMBER_UPDATE / GUILD_MEMBERS_CHUNK
USER_UPDATE
```

Unknown events are ignored. Fields use explicit projection; values such as `user.email`, `token`, and `session_id` do not enter a typed batch. READY yields only objects such as users, guilds, and channels; the entire READY payload is not persisted. Undisclosed first-party client variants require parser extensions based on testing.

Message edits use nullable fields to mean “leave the existing value unchanged when missing”; an explicit empty string can clear content. Revision order prefers `edited_timestamp`, then the message timestamp, and finally observation time if both are absent. This prevents common old HTTP snapshots from overwriting newer edits, but it does not prove a global causal order. Out-of-order events across connections and missing remote timestamps can remain ambiguous. Deletion sets a tombstone, and old events cannot revive the message; historical content is still retained.

`attachments`, `assets`, and `resource_urls` support only attachment metadata and relationships to allowlisted cached images. Query strings and fragments are stripped from signed URLs, so replay credentials are not stored. Resources are matched by the same host/path; CDN transcodes, different paths, and resources that did not pass through the proxy are not guaranteed to merge automatically.

## SQLite and shutdown

The application uses a pure Go SQLite driver, WAL, foreign keys, a 5-second busy timeout, FULL synchronization, and in-memory temporary tables. There is one write connection and a pool of four read connections. Each entity batch is a transaction. Resource files are written atomically before their index entries, so a crash may leave orphaned cache files. Startup quota accounting still includes these files, preventing silently unbounded duplicate usage.

Shutdown first stops external access and closes hijacked connections, then closes the console/internal proxy, stops producers, drains the HTTP and entity-write queues, checkpoints/closes SQLite, and releases the file lock. Entity writes have a 30-second drain window; after timeout, remaining writes are canceled and counted. There is no resident background service, and sensitive batches awaiting a write are not staged in the system temporary directory.

There is no measured concurrency or memory-stress report for this implementation. Event limits apply at event boundaries and are not a strict limit on process RSS. A limit of 128 concurrent Gateway connections and large batches can consume substantial memory. Initial use should keep the number of clients small and use the default queues while monitoring the dashboard.
