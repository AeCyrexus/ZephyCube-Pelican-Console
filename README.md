<p align="center"><img src="assets/icon128.png" width="96" alt=""></p>

# ZephyCube Pelican Console

Native Windows RCON-style client for [Pelican](https://pelican.dev) panel game servers (Minecraft Java & Bedrock).
Single `.exe` — no browser, no runtime, no installer. Talks to the Pelican **Client API** and the **Wings console websocket**.

**[⬇ Download the latest release](https://github.com/AeCyrexus/ZephyCube-Pelican-Console/releases/latest)** — the app updates itself after that.

## Features
| Area | What it does |
|---|---|
| Servers | All servers (paginated), live state, egg. Hide servers (right-click), hide the whole list (Ctrl+B). Last server auto-connects. |
| Background consoles | Switching servers keeps the other consoles connected and logging; unread ◉ and error ⚠ badges in the server list. |
| Console | Live output with colours (errors, warnings, daemon, joins/leaves, your commands), auto-reconnect, history, per-command ✔/✖ feedback. |
| Power | Start / Stop / Restart enabled by state; **Kill** only appears when a stop hangs. Live CPU / RAM / disk / uptime. |
| Online players | One `list` on connect, then tracked live from join/leave lines (Bedrock: `Player Spawned` / `Player disconnected`). Right-click a player for TP, Summon, gamemode, give, effect, kick/ban with reason, op, copy name or XUID. |
| Player history | Per server: XUID, first/last seen, sessions, session and total time. Kick, ban or pardon players who are offline. |
| Player actions | Clear inventory, Gamemode, Teleport (TP / Summon), Effect (all effects, duration, level, clear), Give (searchable item list, stack-size default). Java/Bedrock syntax auto-detected. |
| Quick commands | Grouped buttons (Time, Weather by default), fully customizable with placeholders `{player}` `{me}` `{input}` `{server}`. |
| Backups | Named backups; rotates the oldest unlocked backup when the limit is reached. |
| Look | Dark (default) / light theme, compact layout for small screens. |
| Updates | Checks GitHub Releases, verifies SHA-256 + ed25519 signature before installing. |

## Setup
1. Panel → your account → **API Keys** → create a **client** key.
2. Run `PelicanConsole.exe` → Settings: panel URL (exactly as Wings knows it), API key, your in-game name.
3. **Test connection** → **Save**.

The API key is encrypted with Windows DPAPI in `%APPDATA%\ZephyCube\PelicanConsole\config.json` (current Windows user only).

## Troubleshooting
- **Websocket HTTP 403** — the panel URL in Settings doesn't match the URL Wings is configured with.
- **Commands work but no console** — the node's websocket port isn't reachable from your PC.
- **SmartScreen warning** — the exe isn't code-signed; *More info → Run anyway*. Updates are still signature-checked by the app itself.
- **Update says “no write access”** — keep the exe in a folder you own (not Program Files).

## Building
Pure Go stdlib, no cgo, no third-party modules.
```
go test ./pelican ./update
go run ./tools/genres 1.4.0            # icon / manifest / version resources
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-H windowsgui -s -w -X main.appVersion=1.4.0" -o PelicanConsole.exe .
```
On Windows: `build.bat` (dev build) or `build.bat 1.4.0`.

## Releasing (maintainer)
1. Add a `## [X.Y.Z] - date` section to `CHANGELOG.md`.
2. `git tag vX.Y.Z && git push origin vX.Y.Z`
3. GitHub Actions builds the exe, signs `update.json` with the `ZC_SIGNING_KEY` secret and publishes the release. Installed copies pick it up on next launch.

Rotate the signing key: `go run ./tools/sign keygen` → commit the new `update_pubkey.txt`, replace the secret. (Clients only trust the key compiled into them, so ship one release signed with the old key that contains the new public key first.)

## License
[MIT](LICENSE) for the code. The ZephyCube name and logo are not covered — forks must replace them.
Bundled item/effect data from PrismarineJS minecraft-data (MIT) — see [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
Not affiliated with Mojang, Microsoft or the Pelican project.
