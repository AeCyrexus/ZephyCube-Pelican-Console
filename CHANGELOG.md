# Changelog

## [1.6.1] - 2026-11-27
### Changed
- Bedrock max enchant now works: the item is put into the player's hand only if it's empty (`replaceitem … keep`), then `enchant <player> <enchant> <level>` runs for each enchantment. Nothing is enchanted if the hand wasn't empty. Armor sets go one piece per click (equip it, click again for the next); the feedback line says which piece is next.

## [1.6.0] - 2026-11-27
### Added
- Armor in Player Actions: give a full set (Netherite, Diamond, Iron, Copper, Golden, Chainmail, Leather) with **Max enchant** (Java: Protection IV, Thorns III, Unbreaking III, Mending, plus Respiration/Aqua Affinity, Swift Sneak, Feather Falling/Depth Strider/Soul Speed per piece). Also in the player right-click menu.
- **Max ench.** toggle on the Give row: max-enchants swords, spears, axes, tools, bows, crossbows, tridents, maces, fishing rods, armor, elytra, shields and more (Java).

## [1.5.1] - 2026-11-23
### Changed
- Server list shows each server's egg (e.g. Paper, Bedrock) instead of the node; the left column is slightly wider to fit it. The egg also feeds the Java/Bedrock auto-detection.
- Releases are published automatically on every push to `main` (version from the top CHANGELOG entry).

## [1.5.0] - 2026-11-19
### Added
- Right-click a player in Online Players: Teleport, Summon, Gamemode, Give / Effect (uses the current Player Actions selection), Clear effects / inventory, Kick… / Ban… with a reason, Op / Deop, Copy name, Copy XUID.
- Player history per server (`players.json`): XUID, first / last seen, session count, current / last session length, total time. Open with History (or double-click a player); filter, kick, ban or pardon players who are offline, copy name / XUID, forget.
- Background consoles: switching servers keeps the other consoles connected and logging. The server list marks them ○ (connected), ◉ N (unread lines) and ⚠ N (errors); right-click → Disconnect console / Disconnect background consoles.
### Changed
- `list` runs once per console session (on first connect); the online list is then kept from console lines only. Bedrock joins come from `Player Spawned:` (not `Player connected:`), leaves from `Player disconnected:`.
- Lines Wings replays after a websocket reconnect are no longer duplicated in the console.

## [1.4.0] - 2026-11-19
### Added
- Auto-update from GitHub Releases: checks on launch (toggle in Settings) and via Help → Check for updates; downloads, verifies SHA-256 + ed25519 signature, swaps the exe and restarts.
- Help menu (updates, release notes, shortcuts, about).
- GitHub Actions release workflow (tag `vX.Y.Z` → build, sign, publish).
- MIT license, third-party notices.

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
