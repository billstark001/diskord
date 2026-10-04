# Changelog

All notable changes to diskord are documented here. This file follows the
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) format, and releases
use [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.2.0] - 2026-10-05

### Added

- Preserve observed channel positions and display uncategorized channels above categories.
- Read-only forum and media channel views with observed thread posts, tags, previews,
  reactions, reply counts, search, and list/gallery layouts.
- Render message Markdown with `markdown-it` while retaining observed mentions,
  custom emoji, and emoji shortcodes.
- Show application and database schema versions on the overview page. Display observed
  times in the browser's local timezone and offer message-link and raw-text copy actions.
- Double-click a message in any all-channel timeline to open its source channel or thread.

### Fixed

- Keep unresolved channels out of direct messages, and reconcile guild membership
  when a parent channel becomes known.
- Apply explicit `parent_id: null` updates when a channel leaves its category.
- Preserve both directions of message pagination across periodic query refetches after
  scrolling far back in a timeline.

## [0.1.0] - 2026-09-29

### Added

- A local observation proxy with a Chinese and English Preact archive console.
- SQLite storage for observed users, servers, channels, messages, edits,
  deletion markers, attachments, and reactions.
- Cursor-based message navigation, search, and jumps to surrounding messages.
- Opt-in CDN resource capture and backfill for avatars, server icons, and emoji.
- Current-directory background start and stop commands, optional file logging,
  and timestamped log filenames.
- Explicit CA selection, trust instructions, and desktop Discord launch helpers.

### Fixed

- Cached resources now display across Discord's CDN host and image-format variants.
- Standard emoji shortcodes and observed mentions render in the archive.
- Newly known channel names replace hidden placeholders while known names remain
  intact if a later snapshot is hidden.
- Direct-message channels display observed recipient names.
- Server switching keeps the server rail populated while channels load.
- Windows CI checks native paths and file ACLs instead of Unix permission bits.

[Unreleased]: https://github.com/billstark001/diskord/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/billstark001/diskord/releases/tag/v0.2.0
[0.1.0]: https://github.com/billstark001/diskord/releases/tag/v0.1.0
