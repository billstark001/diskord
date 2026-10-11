# Changelog

All notable changes to diskord are documented here. This file follows the [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) format, and releases use [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Browser context-menu exports with a themed range/format/attachment dialog, keyboard access, cancellation, and downloads through the console session.
- Shared navigation for browser/API/CLI lists, `guilds`/`servers`, `dms`, and `threads` commands, exact `--thread-name` lookup with ownership-aware ambiguity reports, and server/private/child-thread export scopes.
- CLI date-only and local-time bounds, full-day end dates, explicit timezone overrides, and observed server/private/channel/forum ordering; schema 5 stores typed guild order while offline reads retain schema 4 compatibility.
- Versioned loopback read-only archive API with a separate bearer token, plus `api-token`, `status`, `channels`, and cursor-paged `messages` CLI commands.
- Snapshot exports filtered by thread/channel and RFC3339 creation-time ranges, in Markdown or JSONL, optionally bundled with deduplicated cached attachment images, missing-attachment counts, and an export manifest in ZIP.
- Immutable offline queries and exports for stopped archives, without runtime initialization, schema migration, permission changes, or SQLite sidecar writes.

### Changed

- SQLite reader connections enforce `query_only`; explicit export outputs must be new files outside the source runtime.
- Document archive formats, scope limits, authentication, staging, and validation checks; repair outdated platform source references and remove the development acceptance checklist.

### Fixed

- Reject nonexistent browser-local export times instead of silently shifting the selected range during daylight-saving or skipped-date transitions.
- Preserve existing guild order when settings payloads are malformed; share forum activity ordering and attachment/name resolution across archive and console queries.
- Reject explicit empty CLI selectors and disable channel exports for messages without an observed channel.
- Reject date-only bounds whose midnight would be shifted or skipped by a timezone transition, and scope signal cancellation to archive commands.
- Keep canceled browser downloads from resetting a later export, interrupt delivery on shutdown, and deliver each HTTP export as a complete snapshot without conditional or range responses.

## [0.2.0] - 2026-10-05

### Added

- Preserve observed channel positions and display uncategorized channels above categories.
- Read-only forum and media channel views with observed thread posts, tags, previews, reactions, reply counts, search, and list/gallery layouts.
- Render message Markdown with `markdown-it` while retaining observed mentions, custom emoji, and emoji shortcodes.
- Show application and database schema versions on the overview page. Display observed times in the browser's local timezone and offer message-link and raw-text copy actions.
- Double-click a message in any all-channel timeline to open its source channel or thread.

### Fixed

- Keep unresolved channels out of direct messages, and reconcile guild membership when a parent channel becomes known.
- Apply explicit `parent_id: null` updates when a channel leaves its category.
- Preserve both directions of message pagination across periodic query refetches after scrolling far back in a timeline.

## [0.1.0] - 2026-09-29

### Added

- A local observation proxy with a Chinese and English Preact archive console.
- SQLite storage for observed users, servers, channels, messages, edits, deletion markers, attachments, and reactions.
- Cursor-based message navigation, search, and jumps to surrounding messages.
- Opt-in CDN resource capture and backfill for avatars, server icons, and emoji.
- Current-directory background start and stop commands, optional file logging, and timestamped log filenames.
- Explicit CA selection, trust instructions, and desktop Discord launch helpers.

### Fixed

- Cached resources now display across Discord's CDN host and image-format variants.
- Standard emoji shortcodes and observed mentions render in the archive.
- Newly known channel names replace hidden placeholders while known names remain intact if a later snapshot is hidden.
- Direct-message channels display observed recipient names.
- Server switching keeps the server rail populated while channels load.
- Windows CI checks native paths and file ACLs instead of Unix permission bits.

[Unreleased]: https://github.com/billstark001/diskord/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/billstark001/diskord/releases/tag/v0.2.0
[0.1.0]: https://github.com/billstark001/diskord/releases/tag/v0.1.0
