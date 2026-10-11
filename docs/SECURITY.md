# Security Boundaries

This project is intended only for explicitly authorized observation on the local machine. It has no features for covert deployment, automatic CA installation, system proxy hijacking, Discord authentication-token export, cookie export, account takeover, process injection, TLS pinning bypass, or active API replay. `ui-token` and `api-token` explicitly print diskord's own local access tokens for their respective interfaces.

When resource caching is enabled, diskord retries missing image paths against allowlisted Discord CDN hosts over HTTPS. It sends no Discord credentials, does not use the system proxy, and rejects redirects. Signed URL query strings are not retained, so some attachments remain unavailable until the client requests them again.

## Data that is not persisted

The application does not create a raw database or save packet-capture files, WS frame history, HTTP request/response headers, HTTP request bodies, URL query strings, Identify/Resume, or any client WS frames. Only allowlisted entity fields are extracted from server data before it enters the main database. The go-mitmproxy logging/saving/UI addons are not attached, upstream library logging is disabled, and `SSLKEYLOGFILE` / `SSLKEYLOG_FILE` are removed to prevent TLS key logs.

This does not mean sensitive bytes never appear in memory: MITM forwarding must handle plaintext HTTP/WS, so short-lived byte buffers are unavoidable. Nor does it mean secrets in user messages are detected. Message bodies, attachment names, usernames, and guild and channel names may all be sensitive. Keeping them is the application's purpose; the archive should not be treated as a fully de-identified dataset.

Capture errors appear only as aggregate counters; payloads containing authentication fields are not added to logs. When diagnosing issues, do not casually enable upstream debugging, TLS key logging, HTTP dumps, or sharing of a real runtime directory.

Both optional file loggers are off by default. `logs/diskord-<UTC-start-time>.log` receives application diagnostics when enabled; `logs/discord-<UTC-start-time>.log` receives the desktop client's own stdout and stderr only when launched through `diskord discord launch`. Treat Discord output as sensitive and do not share it without review. Each start gets a separate private file. The diskord log has a 10 MiB size limit and three backups per start; the Discord log can grow during a long client session. Old session logs remain until removed manually.

## Files and certificates

Application-owned directories default to Unix mode 0700, and application-owned files to 0600. On Windows, these paths receive protected DACLs for the current user and SYSTEM. The application does not change the permissions of the user's working directory itself, which might contain other files. A dedicated private directory is therefore still recommended.

Path resolution rejects escapes from the runtime directory, symbolic-link path components, and nonconforming CA files. The root directory is canonicalized first, and temporary files for atomic writes remain under `runtime/tmp`. Atomic replacement is rejected if the YAML configuration and runtime directory are on different file systems; there is no fallback to an external temporary directory. The directory is not a security sandbox against malicious programs running as the same OS user. Replacing files, debugging the process, racing paths, or reading memory as that user are outside the threat-protection guarantees.

OpenSSL is invoked with an exec argument array rather than a shell, using explicit file paths, a separate temporary configuration, a timeout, and no overwriting. CA loading checks that the certificate and private key match, that the root is self-signed, and that CA basic constraints, signature usage, validity dates, and key strength are valid. Only target domains can receive leaf certificates. Leaf certificates are cached in memory rather than written per site. The current CA private key has no passphrase to allow unattended persistent handshakes, so protect the runtime directory carefully.

Trust only the public certificate of a CA you just issued and whose SHA-256 fingerprint you verified. Do not trust a “ready-made private key/root CA” supplied in an archive, chat, unknown script, or remote download. This package contains no CA private key or real user data.

## Local console

The console accepts only literal loopback listeners and rejects other Hosts, cross-Origin form submissions and background requests, and cross-site browser requests other than top-level GET navigations. A random administration token is explicitly retrieved from a restricted local file and compared in constant time; it is never put in a URL or routine log. Login is rate-limited, forms have CSRF protection, and the session cookie is HttpOnly / SameSite Strict with a 12-hour client lifetime.

The session key is a shared random value in the process, not a separate server-side session record for each login. Logging out clears the current browser cookie; it does not revoke individual sessions server-side. Stopping the process invalidates sessions. This model suits a single-user local prototype, not multi-user administration. The console uses loopback HTTP rather than a Secure cookie and must not be exposed through another network interface or a public reverse proxy.

The console renders message Markdown with `markdown-it` while disabling raw HTML and rejecting dangerous link protocols. Mention names and emoji markup are escaped before insertion into the generated HTML. Markdown image syntax is rendered as text, so a message cannot make the browser load an arbitrary remote image. The compiled JavaScript and CSS are embedded, with no reliance on external sites. CSP blocks external and inline scripts, arbitrary objects, and cross-origin requests; responses use no-store. The console administration token is sent only to the local login API and is not saved in browser storage. Resource images are read through authenticated local endpoints rather than making the browser request the Discord CDN directly. Turning off resource display does not delete existing files.

## Remaining risks

The versioned archive API uses a separate private `state/api-token` and accepts it only as a bearer credential for archive reads/downloads, not administration. The console token and session are not used by this bearer API. Browser export uses a separate console-session-authenticated route with the same boundary checks and exporter; it does not expose the API token. Existing loopback and Host/Origin checks remain in place, with no CORS access. The CLI disables environment proxies and redirects so its token is not forwarded elsewhere.

Read-only means no entity or administration changes. Online exports still stage temporary files under `runtime/tmp`; offline commands additionally preserve source files, permissions, and sidecars by rejecting active locks/nonempty WAL and using immutable/query-only SQLite without initialization or migration. Explicit outputs are new private files outside the source runtime; offline staging is private and placed beside them. Normal cleanup removes failed or canceled artifacts, but forced termination can leave partial/staging files.

Exports include selected message bodies, names, deletion state, attachment metadata, and optionally cached attachment images. They contain neither CA material, local access tokens, raw traffic, nor saved protocol credentials; secrets in message text remain content. Markdown fences keep message HTML and remote image syntax literal. ZIP members use validated hash filenames rather than untrusted attachment names, and copied bytes are hash-verified. Missing files remain recorded without outbound requests. Preparation limits constrain temporary use but are not comprehensive disk-space or memory quotas. A terminal completion marker means the selected local export finished; it is not a full database backup, edit-history log, or capture-completeness guarantee. See [ARCHIVE.md](ARCHIVE.md) for the interface and output contract.

SQLite is not encrypted. Use disk encryption and user permissions rather than describing this project as an encrypted archive. Protect the CA private key, database, WAL, backups, and console token in the terminal. Operating system swap, core/crash dumps, antivirus/backup tools, and external client disk writes are outside the application's control.

There is no periodic deletion, total database space limit, automated key rotation, or independent audit. Before long capture periods, choose an explicit retention period, check free disk space, and back up and clean up after stopping. If error counters are nonzero, the archive should not be treated as complete evidence.

The public proxy port accepts only local connections but requires no additional proxy authentication; local processes on the same machine can use it. CONNECT to non-target domains is limited to port 443, and some obviously local addresses are rejected, but this is not a complete outbound firewall against SSRF or DNS rebinding. For stronger isolation, use a separate OS user or test VM and a system firewall.

## Decommissioning

Completely exit diskord and the test clients using it, remove the clients' proxy arguments, and delete the diskord root CA by fingerprint in the system/browser certificate manager. Then delete the runtime directory and separate Chrome profile according to your retention policy. Do not stop the process first if clients still need to use that proxy, since they may lose connectivity.
