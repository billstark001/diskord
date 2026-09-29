# Changelog

All notable changes to diskord are documented here. This file follows the
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) format, and releases
use [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

[Unreleased]: https://github.com/billstark001/diskord/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/billstark001/diskord/releases/tag/v0.1.0
