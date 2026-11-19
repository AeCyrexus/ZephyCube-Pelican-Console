# ZephyCube Pelican Console v1.0.0

Native Windows RCON-style client for Pelican panel servers. Single `.exe`, no browser, no runtime, no installer.
Talks to the Pelican **Client API** (REST) for commands/power/backups and to the **Wings console websocket** for live output.

## Setup
1. Panel → your account → **API Keys** → create a client key (allow your PC's IP if you restrict it).
2. Run `PelicanConsole.exe` → Settings opens on first launch:
   - **Panel URL** — exactly the URL Wings is configured with (e.g. `https://pelican.zephycube.com`). Wings checks the websocket `Origin` against it.
   - **Client API key** — stored encrypted with Windows DPAPI (current Windows user only).
   - **In-game name** — used by `{me}`, *TP me → player* and *Summon player → me*.
   - **List every server** — admin keys only (`type=admin-all`).
3. **Test connection** → **Save**.

Config: `%APPDATA%\ZephyCube\PelicanConsole\config.json`

## Features
| Area | What it does |
|---|---|
| Server list | All servers (paginated) with live state + node. F5 refreshes. Double-click / Enter / Connect opens the console. Last server auto-connects on launch. |
| Console | Live Wings output (history on connect), auto-reconnect, token refresh. Colour: red = error, yellow = warn, blue = daemon, pink = your commands. Auto-scroll toggle, Ctrl+L clears. |
| Commands | Enter to send, ↑/↓ history. Sent via `POST /servers/{id}/command`. |
| Feedback line | After each command it reads the console reply for ~1.5 s: **✔ green** = reply OK, **✖ red** = error text detected (unknown command, no targets matched, syntax error…), or the HTTP error (e.g. server offline). |
| Player actions | Player box (type or pick) + **Online ↻** (runs `list`, parses Java/Paper & Bedrock output). Set gamemode, TP me → player (`tp {me} {player}`), Summon player → me (`tp {player} {me}`). Gamertags with spaces are quoted automatically. |
| Quick commands | Custom buttons (Quick Commands → Manage). Multi-line = several commands in order. Placeholders: `{player}` `{me}` `{input}` (prompts) `{server}`. |
| Power | Start / Stop / Restart / Kill (confirm on destructive ones). Live stats: CPU, RAM, disk, uptime. |
| Backups | Create backup (named, defaults to timestamp). Completion/failure shown when Wings reports `backup completed`. |

## Troubleshooting
- **Websocket HTTP 403** → Panel URL in Settings doesn't match the panel URL Wings was configured with (scheme + host).
- **Console never connects but commands work** → node websocket port (8080/443) not reachable from your PC / not proxied through your tunnel.
- **SmartScreen warning** → unsigned exe; *More info → Run anyway*.

## Build (from source)
Pure Go stdlib, no cgo, no dependencies (raw Win32 + own websocket client).
```
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "-H windowsgui -s -w" -o PelicanConsole.exe .
go test ./pelican
```
Windows: `build.bat`.
