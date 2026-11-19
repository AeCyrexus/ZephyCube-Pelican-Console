# Changelog

## [1.3.0] - 2026-11-19
### Added
- Player actions: Clear inventory, Effect (all effects, duration default infinite, level default 1, clear effects), Give (searchable item list, amount defaults to stack size).
- Java/Bedrock command syntax with auto-detection and per-server override.
- Quick command groups; defaults are now Time (Day/Night/Noon) and Weather (Clear/Rain/Thunder).
- Backup rotation: deletes the oldest unlocked backup when the limit is reached.
### Changed
- Compact layout for small screens (smaller header, cards, buttons; window fits the screen).
### Removed
- Default quick commands List players, Say, Kick player, OP player; Clear inventory moved to Player actions (existing configs migrated).

## [1.2.0] - 2026-11-19
### Added
- Hide individual servers (right-click menu), "Show hidden (N)" button, Unhide all.
- "Server list" header toggle / Ctrl+B to hide the whole server list (remembered).

## [1.1.0] - 2026-11-19
### Added
- ZephyCube logo as exe/window/header icon (embedded resources + version info).
- Dark mode (default) and light mode; themed owner-drawn controls, dark title bars.
- Automatic `list` after connecting and live join/leave tracking with an Online Players panel.
### Changed
- Start/Stop/Restart enable based on server state; Kill only shown when a stop hangs.
- "TP me → player" / "Summon player → me" renamed to TP / Summon and grouped under Teleport; gamemode grouped.
- Menu bar replaced by header buttons (Dark mode, Settings, Help).

## [1.0.0] - 2026-11-19
### Added
- Native Win32 client (single exe, no browser) for the Pelican Client API.
- Server list with live state, auto-connect to last server.
- Live Wings console websocket with reconnect, token refresh, coloured output.
- Command input with history and per-command success/error feedback.
- Player actions: gamemode, TP me → player, summon player → me, online player list.
- Custom quick-command buttons with placeholders and multi-command support.
- Start / stop / restart / kill and backup creation with completion notice.
- DPAPI-encrypted API key storage.
