# ZephyCube Pelican Console v1.2.0

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
| Look | ZephyCube logo (exe + window icon), dark mode by default, light mode via header toggle or Settings. |
| Server list | All servers (paginated), state colour-coded, node. F5 refreshes. Double-click / Enter / Connect. Last server auto-connects. Right-click → **Hide server** / Unhide / Show hidden / Unhide all. **Server list** header toggle (Ctrl+B) hides the whole list; Online Players then fills the column. |
| Console | Live Wings output, auto-reconnect, token refresh. Red = error, yellow = warn, blue = daemon, pink = your commands, green = joins/leaves. Auto-scroll toggle, Ctrl+L clears. |
| Commands | Enter to send, ↑/↓ history. Feedback line: ✔ green reply OK / ✖ red error detected / HTTP error. |
| Online players | `list` runs automatically after connecting; joins/leaves are tracked live from the console (Java/Paper and Bedrock BDS). Click a name to fill the Player box. ↻ re-syncs. |
| Player actions | Player box, **Gamemode** group (mode + Set), **Teleport** group: **TP** (you → player) and **Summon** (player → you). |
| Power | Start only when offline, Stop when not offline, Restart when running. **Kill** appears only if Stop/Restart hasn't finished after 15 s, or if you press Stop again while stopping. |
| Backups | Named backup, completion/failure reported. |
| Quick commands | Custom buttons, multi-line, placeholders `{player}` `{me}` `{input}` `{server}`. |

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
Icon/manifest/version resources: `go run ./tools/genres 1.2.0` (writes `rsrc_windows_amd64.syso`, picked up by `go build`).
Windows: `build.bat`.
