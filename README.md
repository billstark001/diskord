# diskord · Local Discord Message Observation Proxy

Go 1.27 / go-mitmproxy / templ + htmx / SQLite, v0.1 prototype. The repository contains the source code, pinned Go dependency checksums, generated templ Go files, and embedded htmx 4.0.0. The local `dist/` directory holds only built binaries. See `TEST_REPORT.md` for build and automated test results; integration with the Discord desktop client still needs to be tested on each target machine.

Observe only local traffic from your own devices and accounts, or traffic for which you have explicit authorization. The application does not automatically install a root certificate, change the system proxy, request Discord history, read browser account databases, or extract or replay authentication credentials.

## Design and initial scope

| Requirement | Implementation |
| --- | --- |
| Single binary | The final Go binary embeds templ-generated pages, CSS, and the actual htmx library. SQLite uses a pure Go driver, and the final build disables CGO. |
| One YAML file / one runtime directory | Defaults to `diskord.yaml` and `runtime_dir: .`, where `.` is the process working directory. Temporary writes controlled by the application go under `tmp/` in the runtime directory. |
| Console | Defaults to `127.0.0.1:3900`; provides login, overview, message search, pagination, a resource toggle, and CA issuance and selection. |
| Persistent proxy | Defaults to `127.0.0.1:3901`; starts with `run`, operates whether or not the web page is open, and stops when the process exits. |
| CA | Invokes system OpenSSL explicitly; requires certificate and private key paths, reports a missing tool, validates the certificate before selection, and never grants trust automatically. |
| Configuration updates | Patches YAML nodes, preserves unrelated nodes and comments where possible, checks a revision hash, stages writes inside the runtime directory, and replaces the file atomically. |
| HTTP / WS | HTTP uses go-mitmproxy. Gateway uses a separate, bounded WebSocket forwarding and observation component in the same process. |
| Entities and relationships | Users, guilds, channels, and messages, with member, DM recipient, and attachment relationships; string IDs, sparse edits, and deletion markers. |
| Raw traffic | **No raw database is created.** `capture.raw: true` produces an error instead of silently changing privacy behavior. |
| Resources | Disabled by default. When enabled, only allowlisted CDN images passing through the proxy are cached passively, with per-file and total size limits. |
| Platforms | Provides Windows/macOS build and integration instructions. Whether the client accepts proxy arguments and trusts the CA still requires testing. |

This is an archive built from observed traffic, not a full client sync: historical messages the client never requested will not appear. Events with unsupported encodings or over size limits are skipped, and the console displays the relevant counters.

## Build

Building from source requires Go 1.27 or newer. The repository includes generated page code and the htmx file; resolving Go modules for the first time may still require network access. Node/npm is not required. Issuing a root CA also requires an OpenSSL executable. Normal `run` operation with a selected CA does not require Go, the templ CLI, or a continuously running OpenSSL process.

macOS / Linux developers can use the Makefile:

```sh
make lint      # gofmt, templ fmt/generate checks, go vet, Staticcheck
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

The build scripts verify the pinned htmx version, generate templ Go code, resolve module dependencies, run tests, and create `dist/diskord-<os>-<arch>[.exe]`. They stop if any step fails. Build caches and temporary files go under the project's `.build/` directory, which is not deployment content.

Prebuilt binaries in `dist/` are unsigned and not notarized. You can build directly from source with `go build ./cmd/diskord`, but Go modules are not vendored; an offline rebuild still requires a populated module cache. The target system trust chain and the behavior of Chrome and the Discord desktop application must be tested using `docs/ACCEPTANCE.md`.

## GitHub releases

Ordinary commits and PRs run builds, linting, and tests. After a `vX.Y.Z` tag is pushed, CI waits for validation and all four platform builds to pass, then creates a GitHub Release with four separate binaries. [GitHub automatically provides](https://docs.github.com/en/repositories/releasing-projects-on-github/about-releases) standard **Source code (zip)** and **Source code (tar.gz)** archives for the tag; the project does not make another local source ZIP. Hyphenated prerelease tags are marked as prereleases.

Before release, commit `go.sum`, `internal/ui/views_templ.go`, and `internal/ui/static/htmx.min.js` in the tagged commit. CI checks that Git tracks these files and that regeneration produces no changes. The release workflow runs only when a tag is pushed; a local `make dist` does not publish a release.

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

On Windows, use the same subcommands with `.\diskord.exe` in place of `./diskord`.

`ui-token` explicitly prints the local console access token. Do not send it to anyone; proxy startup logs do not print that token or Discord credentials. Once running, open this address in a browser:

```text
http://127.0.0.1:3900
```

Log in with the token. The proxy address is:

```text
http://127.0.0.1:3901
```

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
```

The configuration file may be outside the runtime directory, but it must be on the same file system for atomic replacement from `runtime/tmp/`. Cross-file-system updates fail rather than falling back to the system temporary directory.

`ca.cert` and `ca.key` accept paths relative to the runtime directory or absolute paths. Paths that escape the directory or contain symbolic links are rejected. The CA is a locally self-signed root certificate, not a public CA certificate requested for an external domain. `ca issue` requires two explicit, distinct paths to files that do not yet exist; issuance does not automatically select or trust the CA.

At runtime, the web console hot-updates only `resources.enabled` and a validated CA pair. The new CA applies to new TLS handshakes; existing sessions are not retroactively re-signed. Other configuration changes require a restart. If external disk changes conflict with the running configuration, a web save is rejected and prompts a reload. Comments and unrelated nodes are preserved where possible, but the YAML encoder may normalize overall formatting; byte-for-byte preservation is not guaranteed.

`run`, CLI issuance, CLI selection, and CLI configuration changes are mutually exclusive through an operating system file lock. When the proxy is running, use the web console to avoid two processes writing to the same directory. `ui-token` and `doctor` may run in another terminal.

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
    diskord.lock       # File used for the operating system lock
  tmp/                 # Staging for atomic writes, temporary OpenSSL config, etc.
```

These constraints apply to **file writes controlled by diskord itself**. They do not mean the operating system or external clients never write files: system certificate stores, Chrome / Discord configuration, swap, crash dumps, and security software are outside the application's control. A separate Chrome profile can be placed under the runtime directory for testing; diskord does not take over Discord's normal data directory.

## Capture scope and boundaries

The target allowlist is in `internal/scope/`. HTTPS CONNECT requests to non-target domains are tunneled without decryption and do not enter either capture path. For target domains, decryption is still needed to inspect the HTTP path, but request and response bodies for non-target paths are not captured. Plain HTTP is only streamed through and is not captured. CONNECT supports only port 443; plain `ws://` upgrades are unsupported.

HTTP observes only successful JSON responses on allowlisted paths. It does not save request bodies, request headers, URL query strings, cookies, or Authorization. Gateway observes only server-to-browser/client traffic. **There is no observer on the client-to-server side**; Identify, Resume, and all subsequent client frames are forwarded without capture.

Here, “does not capture tokens” refers to authentication fields in the protocol. Forwarding necessarily handles decrypted bytes briefly in process memory. Passwords or secrets users put in message bodies are not automatically detected or redacted by a content filter. Message bodies retained in the main database remain sensitive data.

Gateway supports JSON and a bounded subset of ETF with continuous `zlib-stream` and `zstd-stream` decompression. It does not change client negotiation, downgrade to plaintext, or force a different encoding. Unknown ETF tags and encodings are forwarded but not parsed, with errors counted. The undisclosed payload structures used by first-party Discord clients may differ from the public API documentation, so each version needs testing.

Raw traffic is not written to disk. Resource caching is off by default; when enabled, it caches only images that already pass through the proxy, without replaying signed URLs or actively fetching missing resources. It records only relationships observable between attachments and actually cached files. URLs for different transcodes or sizes are not guaranteed to be merged.

## Reliability and limitations

Proxy forwarding does not wait for SQLite writes. The HTTP queue, WS byte queue, and entity batch queue all have capacity limits. Full queues, decode failures, and write failures increment counters. If observation of a compressed stream falls out of sync, observation stops for that connection while forwarding continues. Decode state is rebuilt only after reconnecting. This design prioritizes continued client operation and **does not promise zero loss, unlimited throughput, or audit-grade completeness**.

SQLite uses WAL, a single writer queue, transactions, foreign keys, a busy timeout, and `synchronous=FULL`; message IDs are strings. Sparse edits do not clear missing fields, deletions use tombstones, and older snapshots do not overwrite higher revisions. The last known content remains locally after deletion; a remote deletion request does not erase the archive.

There is no database encryption, automatic historical cleanup, automatic recovery from a full disk, or database backup UI. The resource cache has quotas, but the main message database has no total size or retention limit. Use an encrypted disk, monitor free space, limit capture periods, and avoid synchronizing the directory with system backups. Stop the process before backing up the entire `db/` directory; do not copy only `main.sqlite` during a write and omit the WAL.

The console has loopback binding, an access token, HttpOnly / SameSite cookies, Host/Origin checks, CSRF protection, CSP, HTML escaping, no remote static resources, and no caching of message pages. It has not undergone an independent security audit. It is not a multi-user or remote administration service; do not expose it to the public internet or broaden access through port forwarding.

## File guide

`docs/ARCHITECTURE.md`: data paths, events, relationships, and reliability decisions.  
`docs/SECURITY.md`: sensitive data boundaries, certificates, disk, and local attack surface.  
`docs/PLATFORMS.md`: explicit Windows/macOS and Chrome/Discord integration steps.  
`docs/ACCEPTANCE.md`: target-platform acceptance checklist.  
`THIRD_PARTY.md`: dependency and asset information.
