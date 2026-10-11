# diskord · Local Discord Message Observation Proxy

Go 1.27 / go-mitmproxy / Preact + TypeScript + Vite / SQLite. The console uses signals, TanStack Query, vanilla-extract styles, and a small built-in Chinese/English dictionary. The repository includes pinned Go and pnpm dependency checksums; the build generates frontend assets and embeds them into the Go binary. Generated frontend files and the local `dist/` binaries are ignored by Git. Integration with the Discord desktop client still needs to be tested on each target machine.

Observe only local traffic from your own devices and accounts, or traffic for which you have explicit authorization. The application does not automatically install a root certificate, change the system proxy, request Discord history, read browser account databases, or extract or replay authentication credentials.

## Design and initial scope

| Requirement | Implementation |
| --- | --- |
| Single binary | The final Go binary embeds the compiled Preact console. SQLite uses a pure Go driver, and the final build disables CGO. |
| One YAML file / one runtime directory | Defaults to `diskord.yaml` and `runtime_dir: .`, where `.` is the process working directory. Normal capture and online staging use runtime `tmp/`; explicit export outputs and offline staging remain outside the source runtime. |
| Console | Defaults to `127.0.0.1:3900`; provides Chinese/English pages, version metadata, a read-only observed server/category/channel browser with channel positions and forum post views, Markdown messages, local-time timestamps, message copy and navigation actions, message search and pagination, a resource toggle, and CA issuance and selection. The Preact shell preserves scroll position across local actions. |
| Persistent proxy | Defaults to `127.0.0.1:3901`; starts with `run`, operates whether or not the web page is open, and stops when the process exits. |
| CA | Invokes system OpenSSL explicitly; requires certificate and private key paths, reports a missing tool, validates the certificate before selection, and never grants trust automatically. |
| Configuration updates | Patches YAML nodes, preserves unrelated nodes and comments where possible, checks a revision hash, stages writes inside the runtime directory, and replaces the file atomically. |
| HTTP / WS | HTTP uses go-mitmproxy. Gateway uses a separate, bounded WebSocket forwarding and observation component in the same process. |
| Entities and relationships | Users, guilds, channels, and messages, with member, DM recipient, and attachment relationships; string IDs, sparse edits, and deletion markers. |
| Raw traffic | **No raw database is created.** `capture.raw: true` produces an error instead of silently changing privacy behavior. |
| Resources | Disabled by default. When enabled, allowlisted CDN images passing through the proxy are cached. Enabling it also retries previously observed missing CDN paths, with per-file and total size limits. |
| Automation and export | Versioned loopback read-only API with a separate token; online/offline query CLI; thread/channel and creation-time selection; Markdown or JSONL exports with scope/date filenames, optional Markdown source links, and a single ZIP for split Markdown and/or cached attachment images. |
| Platforms | Provides Windows/macOS build and integration instructions. Whether the client accepts proxy arguments and trusts the CA still requires testing. |

This is an archive built from observed traffic, not a full client sync: historical messages the client never requested will not appear. Events with unsupported encodings or over size limits are skipped, and the console displays the relevant counters.

## Build

Building from source requires Go 1.27 or newer, Node 26, and pnpm 12.6.0. Run `pnpm -C frontend install --frozen-lockfile` and `pnpm -C frontend run build` to compile the frontend separately, or use the build scripts below. Resolving dependencies for the first time may require network access. The final binary needs neither Node nor pnpm. Issuing a root CA also requires an OpenSSL executable. Normal `run` operation with a selected CA does not require a development toolchain.

macOS / Linux developers can use the Makefile:

```sh
make lint      # gofmt, frontend type/format checks, go vet, Staticcheck
make verify    # lint plus Go race tests
make audit     # scan the Go vulnerability database
make build     # binary for the current platform
make dist      # standalone binaries for four macOS and Windows architectures; no ZIP
```

macOS / Linux:

```sh
chmod +x scripts/*.sh
./scripts/build.sh
# Cross-compilation examples; generators and tests still run on the host:
TARGET_GOOS=windows TARGET_GOARCH=amd64 ./scripts/build.sh
TARGET_GOOS=darwin TARGET_GOARCH=arm64 ./scripts/build.sh
TARGET_GOOS=darwin TARGET_GOARCH=amd64 ./scripts/build.sh
```

Windows PowerShell:

```powershell
.\scripts\lint.ps1
.\scripts\build.ps1
# Windows ARM64 can also be built locally; the cross-compiled binary has not yet been run on Windows hardware:
# .\scripts\build.ps1 -TargetOS windows -TargetArch arm64
```

The build scripts install from the pinned pnpm lockfile, typecheck and compile the frontend, resolve Go dependencies, run tests, and create `dist/diskord-<os>-<arch>[.exe]`. They stop if any step fails. Generated frontend assets in `internal/ui/webdist/` are ignored by Git. The frontend build script hashes its source inputs and verifies cached output hashes, so repeated builds reuse unchanged assets. Set `FORCE_FRONTEND_BUILD=1` (PowerShell: `$env:FORCE_FRONTEND_BUILD = '1'`) to rebuild them, or run `make generate`. Build caches and temporary files go under `.build/`.

Prebuilt binaries in `dist/` are unsigned and not notarized. On a fresh checkout, build the frontend first with `node scripts/build-frontend.mjs` before running `go build ./cmd/diskord`, or use the project build script. Go modules are not vendored; an offline rebuild still requires a populated module cache. The target system trust chain and the behavior of Chrome and the Discord desktop application must be tested using [the platform guide](docs/PLATFORMS.md).

## GitHub releases

See [the release procedure](docs/RELEASING.md) and [changelog](CHANGELOG.md) before creating a tag.

Ordinary commits and PRs run builds, linting, and tests. After a `vX.Y.Z` tag is pushed, CI waits for validation and all four platform builds to pass, then creates a GitHub Release with four separate binaries. [GitHub automatically provides](https://docs.github.com/en/repositories/releasing-projects-on-github/about-releases) standard **Source code (zip)** and **Source code (tar.gz)** archives for the tag; the project does not make another local source ZIP. Hyphenated prerelease tags are marked as prereleases.

Before release, commit `go.sum` and `frontend/pnpm-lock.yaml` in the tagged commit. CI builds the ignored frontend assets before compiling the single-file binaries. The release workflow runs only when a tag is pushed; a local `make dist` does not publish a release.

## First run

A dedicated directory is recommended for sensitive data, although the default runtime directory is the current working directory. Copy the binary for your platform into that directory and rename it to `diskord` or `diskord.exe`. If a downloaded macOS binary lacks execute permission, first run `chmod +x diskord`. Then run these commands from that directory:

```sh
./diskord init
./diskord ca issue --cert ca/root.pem --key ca/root.key
./diskord ca select --cert ca/root.pem --key ca/root.key
./diskord doctor
./diskord ui-token
./diskord run
```

To run without keeping a terminal open, use `./diskord run-bg` instead of `run`; later run `./diskord stop-bg` from the **same working directory**. `run-bg` requires the configuration file and `runtime_dir` to resolve to that directory. You may select another configuration file in that directory with `--config`, but use the same path for both commands; `stop-bg` can still work if that file has since been removed. `run-bg` records a private control token and PID in `state/diskord-bg.json` and redirects standard output and error to `logs/diskord-bg-<UTC-start-time>.log`. `stop-bg` verifies the background instance through its local authenticated control endpoint and waits for the runtime lock to be released. Ordinary `run` instances are unaffected. Do not share the state file or its token.

On Windows, use the same subcommands with `.\diskord.exe` in place of `./diskord`.

`ui-token` explicitly prints the local console access token. Do not send it to anyone; proxy startup logs do not print that token or Discord credentials. Once running, open this address in a browser:

```text
http://127.0.0.1:3900
```

Log in with the token. The proxy address is:

```text
http://127.0.0.1:3901
```

The console selects Chinese or English from your browser language on first visit. Use the language control on the login page or in the header to override it. The message page presents observed servers, categories, and channels in a read-only three-column archive. Uncategorized channels appear first; channels with an observed Discord position follow that position within their group. Forum and media channels show observed posts as a searchable list or gallery; opening a post shows its archived thread messages. These views cannot create posts. Missing servers, channels, threads, or messages are not fetched from Discord; they appear only after the proxy observes them. A thread's reply count may be an incomplete or stale snapshot. Unknown channels remain outside the direct-message list until their type or guild relationship is known. Markdown formatting is rendered in message bodies, while cached author avatars, observed mentions, emoji, and reactions remain available. Hover over a reaction to see known user names; a count of other users appears when their identities were not observed. The narrow layout stacks the channel list above messages. The frontend uses same-origin JSON APIs and an HttpOnly session cookie; it does not store the access token in browser storage.

Follow `docs/PLATFORMS.md` to **manually verify and trust the public CA certificate**, then explicitly configure test Chrome / Discord desktop processes to use the proxy. Never import `root.key` into a browser, send it to anyone, or commit it to the repository.

Without OpenSSL, `ca issue` fails with an explicit error. You may also set an absolute path during initialization:

```sh
./diskord init --openssl /absolute/path/to/openssl
# Windows example; replace this path with the actual installed executable:
# .\diskord.exe init --openssl 'C:\Tools\OpenSSL\bin\openssl.exe'
```

If a YAML file already exists, edit `ca.openssl` and restart, or pass `--openssl` to this invocation of `ca issue`. Do not pass an entire shell command to `--openssl`.

## Configuration and commands

See `diskord.example.yaml` for the default configuration. Initialization does not overwrite an existing YAML file.

```sh
./diskord --config /absolute/path/diskord.yaml init --runtime-dir /absolute/path/runtime
./diskord --config /absolute/path/diskord.yaml run
./diskord config set resources.enabled true
./diskord config set resources.enabled false
./diskord config set logging.file.enabled true
./diskord config set logging.discord.enabled true
./diskord discord launch
```

The configuration file may be outside the runtime directory, but it must be on the same file system for atomic replacement from `runtime/tmp/`. Cross-file-system updates fail rather than falling back to the system temporary directory.

Both file loggers are disabled by default. `logging.file.enabled` writes diskord application logs to `logs/diskord-<UTC-start-time>.log`, with three 10 MiB backups per start, while retaining stderr output. `logging.discord.enabled` redirects Discord stdout and stderr to `logs/discord-<UTC-start-time>.log` when it is started with `diskord discord launch`; an already running Discord process is unaffected. Each start creates a new private log file, and the log contents also include the start timestamp. Quit Discord completely before using that command so its proxy arguments take effect. The launcher searches typical installation paths; pass `--path` if necessary. A single long-running Discord session may grow past 10 MiB. Old session files are retained until you remove them. Restart diskord for changes to its file logger to take effect.

`ca.cert` and `ca.key` accept paths relative to the runtime directory or absolute paths. Paths that escape the directory or contain symbolic links are rejected. The CA is a locally self-signed root certificate, not a public CA certificate requested for an external domain. `ca issue` requires two explicit, distinct paths to files that do not yet exist; issuance does not automatically select or trust the CA.

At runtime, the web console hot-updates only `resources.enabled` and a validated CA pair. The new CA applies to new TLS handshakes; existing sessions are not retroactively re-signed. Other configuration changes require a restart. If external disk changes conflict with the running configuration, a web save is rejected and prompts a reload. Comments and unrelated nodes are preserved where possible, but the YAML encoder may normalize overall formatting; byte-for-byte preservation is not guaranteed.

`run`, CLI issuance, CLI selection, and CLI configuration changes are mutually exclusive through an operating system file lock. When the proxy is running, use the web console to avoid two processes writing to the same directory. `ui-token` and `doctor` may run in another terminal.

## Read-only automation and export

Right-click a server, channel/category, forum post, or message with an observed channel to export its local archive, or use the message view's export button. The dialog offers current/all scope, local time bounds, Markdown/JSONL, child threads, and optional cached-attachment ZIPs, using the existing console session.

`status`, `guilds`, `dms`, `channels`, `threads`, `messages`, and `export` use the running instance's local read-only API by default. The CLI reads the separate `state/api-token`, created when the updated server first starts. `api-token` prints that existing token for explicit API integrations; it never initializes a runtime. The token does not grant configuration or CA administration rights.

```sh
./diskord status
./diskord channels
./diskord messages --thread 234567890123456789 --limit 100
./diskord export --thread 234567890123456789 --since '2026-10-01T00:00:00+09:00' --until '2026-10-08T00:00:00+09:00' --format md --output ../thread.md
./diskord export --thread 234567890123456789 --format jsonl --attachments --output ../thread.zip
```

`--thread` also accepts ordinary channel IDs; omitting it selects all local messages. CLI dates can omit the time or timezone: a date-only start begins at local midnight, and a date-only end includes the whole final date; omitted bounds are unbounded. `--thread-name` matches exact names and reports every ambiguous candidate with its server/private-conversation ownership. Navigation listings share the browser order, using observed positions and latest private-message activity. Time bounds select creation time with an ID-derived fallback and use `[since, until)`. Exports include the latest observed states and deletion markers, not edit history or a guarantee of complete Discord history. Missing attachments are reported, never fetched. Text exports can go to stdout; attachment exports produce ZIP.

Add `--offline` to read an existing stopped archive without changing its files, permissions, or schema. It rejects active runtimes and nonempty WAL files. Offline exports require an explicit output file. File outputs must be new paths outside the source runtime. See [the archive interface guide](docs/ARCHIVE.md) for API routes, schemas, authentication, limits, and Windows examples.

## Runtime directory

```text
runtime/
  diskord[.exe]        # May be placed here or elsewhere
  diskord.yaml         # Default location; the only configuration file
  ca/                  # CA certificate / private key specified by the user
  db/
    main.sqlite        # Entities, relationships, attachments, and resource index
    main.sqlite-wal    # May exist while SQLite is running
    main.sqlite-shm
  resources/           # Passive image cache named by content hash
  state/
    ui-token           # Local administration token
    api-token          # Separate read-only archive token; created on server startup
    diskord.lock       # File used for the operating system lock
  tmp/                 # Staging for atomic writes, temporary OpenSSL config, etc.
  logs/                # Optional diskord and Discord file logs
```

Normal capture and online export preparation write under the runtime. Explicit export files are written outside the source runtime; offline exports stage privately beside their chosen output so the source remains unchanged. These constraints apply to **file writes controlled by diskord itself**. They do not mean the operating system or external clients never write files: system certificate stores, Chrome / Discord configuration, swap, crash dumps, and security software are outside the application's control. A separate Chrome profile can be placed under the runtime directory for testing; diskord does not take over Discord's normal data directory.

## Capture scope and boundaries

The target allowlist is in `internal/scope/`. HTTPS CONNECT requests to non-target domains are tunneled without decryption and do not enter either capture path. For target domains, decryption is still needed to inspect the HTTP path, but request and response bodies for non-target paths are not captured. Plain HTTP is only streamed through and is not captured. CONNECT supports only port 443; plain `ws://` upgrades are unsupported.

HTTP observes only successful JSON responses on allowlisted paths. It does not save request bodies, request headers, URL query strings, cookies, or Authorization. Gateway observes only server-to-browser/client traffic. **There is no observer on the client-to-server side**; Identify, Resume, and all subsequent client frames are forwarded without capture.

Here, “does not capture tokens” refers to authentication fields in the protocol. Forwarding necessarily handles decrypted bytes briefly in process memory. Passwords or secrets users put in message bodies are not automatically detected or redacted by a content filter. Message bodies retained in the main database remain sensitive data.

Gateway supports JSON and a bounded subset of ETF with continuous `zlib-stream` and `zstd-stream` decompression. It does not change client negotiation, downgrade to plaintext, or force a different encoding. Unknown ETF tags and encodings are forwarded but not parsed, with errors counted. The undisclosed payload structures used by first-party Discord clients may differ from the public API documentation, so each version needs testing.

Raw traffic is not written to disk. Resource caching is off by default. When enabled, it caches allowed image responses passing through the proxy and directly retries previously observed missing CDN paths without credentials or a system proxy. Signed attachment URL parameters are never stored; those requests may fail after expiry and can be captured when the Discord client supplies a fresh URL. Backfill does not request message history or user data from the Discord API. URLs for different transcodes or sizes are not guaranteed to be merged.

## Reliability and limitations

Proxy forwarding does not wait for SQLite writes. The HTTP queue, WS byte queue, and entity batch queue all have capacity limits. Full queues, decode failures, and write failures increment counters. If observation of a compressed stream falls out of sync, observation stops for that connection while forwarding continues. Decode state is rebuilt only after reconnecting. This design prioritizes continued client operation and **does not promise zero loss, unlimited throughput, or audit-grade completeness**.

SQLite uses WAL, a single writer queue, transactions, foreign keys, a busy timeout, and `synchronous=FULL`; message IDs are strings. Sparse edits do not clear missing fields, deletions use tombstones, and older snapshots do not overwrite higher revisions. The last known content remains locally after deletion; a remote deletion request does not erase the archive.

There is no database encryption, automatic historical cleanup, automatic recovery from a full disk, or database backup UI. The resource cache has quotas, but the main message database has no total size or retention limit. Use an encrypted disk, monitor free space, limit capture periods, and avoid synchronizing the directory with system backups. Stop the process before backing up the entire `db/` directory; do not copy only `main.sqlite` during a write and omit the WAL.

The console has loopback binding, an access token, HttpOnly / SameSite cookies, Host/Origin checks, CSRF protection, CSP, HTML escaping, no remote static resources, and no caching of message pages. It has not undergone an independent security audit. It is not a multi-user or remote administration service; do not expose it to the public internet or broaden access through port forwarding.

## File guide

- `docs/ARCHITECTURE.md`: data paths, events, relationships, and reliability decisions.
- `docs/ARCHIVE.md`: read-only API/CLI, export formats, time semantics, and offline access.
- `docs/SECURITY.md`: sensitive data boundaries, certificates, disk, and local attack surface.
- `docs/PLATFORMS.md`: explicit Windows/macOS and Chrome/Discord integration steps.
- `THIRD_PARTY.md`: dependency and asset information.
