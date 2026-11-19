//go:build windows

package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unsafe"

	"zephycube/pelicanconsole/pelican"
)

//go:embed icon.png
var iconPNG []byte

const appTitle = "ZephyCube Pelican Console"
const mainClass = "ZCPelicanMain"

// control IDs
const (
	idList = 200 + iota
	idRefresh
	idConnect
	idStart
	idStop
	idRestart
	idKill
	idBackup
	idClear
	idAutoScroll
	idCmd
	idSend
	idPlayer
	idPlayers
	idMode
	idGamemode
	idTpMe
	idBring
	idQuickEdit

	idMenuSettings = 100
	idMenuExit     = 101
	idMenuQuick    = 102
	idMenuRefresh  = 103
	idMenuHelp     = 104
	idQuickBase    = 1000
)

var (
	uiFont, boldFont, headFont, monoFont uintptr
	btnFaceBrush                        uintptr
)

type App struct {
	cfg    *Config
	client *pelican.Client
	w      *Win

	// controls
	lv, bRefresh, bConnect                          uintptr
	lName, lState, lStats                           uintptr
	bStart, bStop, bRestart, bKill, bBackup, bClear uintptr
	chkAuto                                         uintptr
	console                                         uintptr
	eCmd, bSend, lFeedback                          uintptr
	lPlayersHdr, lPlayer, cbPlayer, bPlayers        uintptr
	lMode, cbMode, bGamemode, bTpMe, bBring         uintptr
	lQuickHdr, bQuickEdit, lQuickEmpty, sep1, sep2  uintptr
	status                                          uintptr
	quickBtns                                       []uintptr

	servers []pelican.Server
	states  map[string]string

	cur       *pelican.Server
	con       *pelican.Console
	gen       int
	connected bool
	curState  string

	feedbackColor uint32
	stateColor    uint32

	history []string
	histIdx int

	// command result capture
	capGen    int
	capActive bool
	capLabel  string
	capLines  []string

	// player list parsing
	plParser *pelican.PlayerListParser
	plGen    int

	// pending console lines from the websocket goroutine
	pendMu    sync.Mutex
	pending   []pendLine
	pendSched bool
}

type pendLine struct {
	gen  int
	text string
	kind pelican.LineKind
}

var modes = []string{"survival", "creative", "adventure", "spectator"}

func init() { runtime.LockOSThread() }

func main() {
	a := &App{cfg: loadConfig(), states: map[string]string{}, feedbackColor: RGB(90, 90, 90), stateColor: RGB(90, 90, 90)}
	enableVisualStyles()
	initDPI()
	initCommonControls()
	r, _, _ := pGetModuleHandleW.Call(0)
	hInstance = r
	r, _, _ = pGetSysColorBrush.Call(COLOR_BTNFACE)
	btnFaceBrush = r

	uiFont = makeFont("Segoe UI", 9, FW_NORMAL)
	boldFont = makeFont("Segoe UI", 9, FW_BOLD)
	headFont = makeFont("Segoe UI Semibold", 12, FW_BOLD)
	monoFont = makeFont("Consolas", 10, FW_NORMAL)

	icon := loadPNGIcon(iconPNG, 32)
	registerClass(mainClass, btnFaceBrush, icon)
	registerClass(dlgClass, btnFaceBrush, icon)

	a.build()
	if icon != 0 {
		SendMessage(a.w.Hwnd, WM_SETICON, 1, icon)
		if small := loadPNGIcon(iconPNG, 16); small != 0 {
			SendMessage(a.w.Hwnd, WM_SETICON, 0, small)
		}
	}
	Show(a.w.Hwnd, SW_SHOW)
	pUpdateWindow.Call(a.w.Hwnd)

	Post(func() {
		if a.cfg.PanelURL == "" || a.cfg.apiKey == "" {
			a.openSettings()
		} else {
			a.applyConfig()
		}
	})
	a.loop()
}

func (a *App) loop() {
	var m MSG
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return
		}
		if m.Message == WM_KEYDOWN {
			if a.handleKey(m.Hwnd, m.WParam) {
				continue
			}
		}
		if d, _, _ := pIsDialogMessageW.Call(a.w.Hwnd, uintptr(unsafe.Pointer(&m))); d != 0 {
			continue
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func ctrlDown() bool {
	r, _, _ := pGetKeyState.Call(VK_CONTROL)
	return int16(r) < 0
}

func (a *App) handleKey(h, vk uintptr) bool {
	if vk == VK_F5 {
		a.refreshServers()
		return true
	}
	if h == a.eCmd {
		switch vk {
		case VK_RETURN:
			a.sendFromInput()
			return true
		case VK_UP:
			if len(a.history) > 0 {
				if a.histIdx > 0 {
					a.histIdx--
				}
				a.setCmdText(a.history[a.histIdx])
			}
			return true
		case VK_DOWN:
			if a.histIdx < len(a.history)-1 {
				a.histIdx++
				a.setCmdText(a.history[a.histIdx])
			} else {
				a.histIdx = len(a.history)
				a.setCmdText("")
			}
			return true
		case VK_L:
			if ctrlDown() {
				a.clearConsole()
				return true
			}
		}
	}
	if h == a.lv && vk == VK_RETURN {
		a.connectSelected()
		return true
	}
	return false
}

func (a *App) setCmdText(s string) {
	SetText(a.eCmd, s)
	n := uintptr(len(utf16.Encode([]rune(s))))
	SendMessage(a.eCmd, EM_SETSEL, n, n)
}
// ---------------- UI construction ----------------

func (a *App) build() {
	a.w = &Win{font: uiFont, MinW: S(1000), MinH: S(640)}
	NewTopWindow(a.w, mainClass, appTitle, WS_OVERLAPPEDWINDOW|WS_CLIPCHILDREN, WS_EX_CONTROLPARENT, S(1280), S(820), 0)
	mainHwnd = a.w.Hwnd
	w := a.w

	// menu
	menu, _, _ := pCreateMenu.Call()
	file, _, _ := pCreatePopupMenu.Call()
	appendMenu(file, MF_STRING, idMenuSettings, "&Settings…")
	appendMenu(file, MF_STRING, idMenuRefresh, "&Refresh servers\tF5")
	appendMenu(file, MF_SEPARATOR, 0, "")
	appendMenu(file, MF_STRING, idMenuExit, "E&xit")
	appendMenu(menu, MF_POPUP, file, "&File")
	quick, _, _ := pCreatePopupMenu.Call()
	appendMenu(quick, MF_STRING, idMenuQuick, "&Manage quick commands…")
	appendMenu(menu, MF_POPUP, quick, "&Quick Commands")
	help, _, _ := pCreatePopupMenu.Call()
	appendMenu(help, MF_STRING, idMenuHelp, "&Shortcuts && placeholders")
	appendMenu(menu, MF_POPUP, help, "&Help")
	pSetMenu.Call(w.Hwnd, menu)

	// left: servers
	a.lv = w.Ctl("SysListView32", "", WS_TABSTOP|LVS_REPORT|LVS_SINGLESEL|LVS_SHOWSELALWAYS|LVS_NOSORTHEADER, WS_EX_CLIENTEDGE, idList)
	SendMessage(a.lv, LVM_SETEXTENDEDLISTVIEWSTYLE, LVS_EX_FULLROWSELECT|LVS_EX_DOUBLEBUFFER, LVS_EX_FULLROWSELECT|LVS_EX_DOUBLEBUFFER)
	setExplorerTheme(a.lv)
	lvAddColumn(a.lv, 0, "Server", S(150))
	lvAddColumn(a.lv, 1, "State", S(64))
	lvAddColumn(a.lv, 2, "Node", S(60))
	a.bRefresh = w.Button("Refresh  (F5)", idRefresh)
	a.bConnect = w.Button("Connect", idConnect)
	SetFont(a.bConnect, boldFont)

	// right: header
	a.lName = w.Label("No server connected", SS_ENDELLIPSIS)
	SetFont(a.lName, headFont)
	a.lState = w.Label("", 0)
	SetFont(a.lState, boldFont)
	a.lStats = w.Label("", SS_RIGHT|SS_ENDELLIPSIS)

	a.bStart = w.Button("▶  Start", idStart)
	a.bStop = w.Button("■  Stop", idStop)
	a.bRestart = w.Button("⟳  Restart", idRestart)
	a.bKill = w.Button("✖  Kill", idKill)
	a.bBackup = w.Button("Create backup…", idBackup)
	a.bClear = w.Button("Clear", idClear)
	a.chkAuto = w.Ctl("BUTTON", "Auto-scroll", WS_TABSTOP|BS_AUTOCHECKBOX, 0, idAutoScroll)
	SetChecked(a.chkAuto, true)

	// console (RichEdit 4.1)
	a.console = w.Ctl("RICHEDIT50W", "", WS_TABSTOP|WS_VSCROLL|ES_MULTILINE|ES_READONLY|ES_AUTOVSCROLL, WS_EX_CLIENTEDGE, 0)
	SendMessage(a.console, EM_EXLIMITTEXT, 0, 64<<20)
	SendMessage(a.console, EM_SETBKGNDCOLOR, 0, uintptr(colBg))
	a.setDefaultCharFormat()

	a.eCmd = w.Edit("", 0, idCmd)
	SetFont(a.eCmd, monoFont)
	SetCue(a.eCmd, "Type a console command and press Enter  (↑/↓ history, no leading slash)")
	a.bSend = w.Ctl("BUTTON", "Send", WS_TABSTOP|BS_PUSHBUTTON, 0, idSend)
	a.lFeedback = w.Label("", SS_ENDELLIPSIS)
	SetFont(a.lFeedback, boldFont)

	a.sep1 = w.Ctl("STATIC", "", SS_ETCHEDHORZ, 0, 0)
	a.lPlayersHdr = w.Label("Player actions", 0)
	SetFont(a.lPlayersHdr, boldFont)
	a.lPlayer = w.Label("Player:", SS_RIGHT)
	a.cbPlayer = w.Ctl("COMBOBOX", "", WS_TABSTOP|WS_VSCROLL|CBS_DROPDOWN|CBS_AUTOHSCROLL, 0, idPlayer)
	a.bPlayers = w.Button("Online ↻", idPlayers)
	a.lMode = w.Label("Mode:", SS_RIGHT)
	a.cbMode = w.Ctl("COMBOBOX", "", WS_TABSTOP|WS_VSCROLL|CBS_DROPDOWNLIST, 0, idMode)
	comboSet(a.cbMode, modes)
	SendMessage(a.cbMode, CB_SETCURSEL, 1, 0)
	a.bGamemode = w.Button("Set gamemode", idGamemode)
	a.bTpMe = w.Button("TP me → player", idTpMe)
	a.bBring = w.Button("Summon player → me", idBring)

	a.sep2 = w.Ctl("STATIC", "", SS_ETCHEDHORZ, 0, 0)
	a.lQuickHdr = w.Label("Quick commands", 0)
	SetFont(a.lQuickHdr, boldFont)
	a.bQuickEdit = w.Button("Edit buttons…", idQuickEdit)
	a.lQuickEmpty = w.Label("No quick commands — click “Edit buttons…” to add some.", 0)

	a.status = w.Ctl("msctls_statusbar32", "", SBARS_SIZEGRIP, 0, 0)

	w.OnSize = func(cw, ch int) { a.layout(cw, ch) }
	w.OnCommand = a.onCommand
	w.OnNotify = a.onNotify
	w.OnColor = a.onColor
	w.OnClose = func() {
		if a.con != nil {
			a.con.Close()
		}
		Destroy(w.Hwnd)
	}
	w.OnDestroy = func() { pPostQuitMessage.Call(0) }

	a.rebuildQuick()
	a.updateButtons()
	a.setStatus(0, "Disconnected")
}

func appendMenu(menu uintptr, flags uint32, id uintptr, text string) {
	t := u16(text)
	pAppendMenuW.Call(menu, uintptr(flags), id, uintptr(unsafe.Pointer(t)))
}

var (
	colBg      = RGB(10, 10, 12)
	colText    = RGB(222, 222, 222)
	colError   = RGB(255, 99, 99)
	colWarn    = RGB(240, 196, 80)
	colDaemon  = RGB(110, 180, 255)
	colInstall = RGB(150, 150, 160)
	colEcho    = RGB(255, 170, 200)
	colInfo    = RGB(232, 70, 130)
)

func (a *App) setDefaultCharFormat() {
	cf := CHARFORMATW{DwMask: CFM_COLOR | CFM_FACE | CFM_SIZE, YHeight: 200, CrTextColor: colText}
	cf.CbSize = uint32(unsafe.Sizeof(cf))
	copy(cf.SzFaceName[:], utf16.Encode([]rune("Consolas")))
	SendMessage(a.console, EM_SETCHARFORMAT, SCF_ALL, uintptr(unsafe.Pointer(&cf)))
	SendMessage(a.console, EM_SETCHARFORMAT, 0, uintptr(unsafe.Pointer(&cf)))
}

type flowItem struct {
	h uintptr
	w int
}

// flow lays out items left-to-right, wrapping; returns height used.
func flow(items []flowItem, x, y, width, rowH, gap int, apply bool) int {
	cx, cy := x, y
	for _, it := range items {
		if cx > x && cx+it.w > x+width {
			cx = x
			cy += rowH + gap
		}
		if apply {
			Move(it.h, cx, cy, it.w, rowH)
		}
		cx += it.w + gap
	}
	return cy + rowH - y
}

func (a *App) layout(cw, ch int) {
	if cw == 0 || ch == 0 {
		return
	}
	SendMessage(a.status, WM_SIZE, 0, 0)
	sr := WindowRect(a.status)
	sbH := int(sr.Bottom - sr.Top)
	parts := []int32{int32(S(260)), int32(cw - S(170)), -1}
	SendMessage(a.status, SB_SETPARTS, 3, uintptr(unsafe.Pointer(&parts[0])))

	m := S(10)
	g := S(6)
	bh := S(28)
	bottom := ch - sbH - m

	// left column
	lw := S(290)
	Move(a.lv, m, m, lw, bottom-m-bh-g)
	half := (lw - g) / 2
	Move(a.bRefresh, m, bottom-bh, half, bh)
	Move(a.bConnect, m+half+g, bottom-bh, lw-half-g, bh)
	SendMessage(a.lv, LVM_SETCOLUMNWIDTH, 0, uintptr(lw-S(64)-S(64)-S(24)))
	SendMessage(a.lv, LVM_SETCOLUMNWIDTH, 1, uintptr(S(64)))
	SendMessage(a.lv, LVM_SETCOLUMNWIDTH, 2, uintptr(S(64)))

	// right column
	rx := m + lw + S(12)
	rw := cw - rx - m
	y := m
	Move(a.lName, rx, y, rw*45/100, S(28))
	Move(a.lState, rx+rw*45/100+g, y, S(110), S(28))
	Move(a.lStats, rx+rw*45/100+g+S(110), y, rw-(rw*45/100+g+S(110)), S(28))
	y += S(32)
	x := rx
	for _, b := range []uintptr{a.bStart, a.bStop, a.bRestart, a.bKill} {
		Move(b, x, y, S(88), bh)
		x += S(88) + g
	}
	Move(a.bBackup, x+S(10), y, S(130), bh)
	Move(a.chkAuto, rx+rw-S(96), y, S(96), bh)
	Move(a.bClear, rx+rw-S(96)-g-S(70), y, S(70), bh)
	consoleTop := y + bh + g

	// bottom-up: quick buttons
	qItems := make([]flowItem, len(a.quickBtns))
	for i, b := range a.quickBtns {
		qItems[i] = flowItem{b, S(140)}
	}
	qh := bh
	if len(qItems) > 0 {
		qh = flow(qItems, rx, 0, rw, bh, g, false)
	}
	qy := bottom - qh
	if len(qItems) > 0 {
		flow(qItems, rx, qy, rw, bh, g, true)
		Show(a.lQuickEmpty, 0)
	} else {
		Move(a.lQuickEmpty, rx, qy, rw, bh)
		Show(a.lQuickEmpty, SW_SHOW)
	}
	hy := qy - g - S(26)
	Move(a.lQuickHdr, rx, hy, S(200), S(26))
	Move(a.bQuickEdit, rx+rw-S(120), hy, S(120), S(26))
	Move(a.sep2, rx, hy-g, rw, 2)

	// player actions
	pItems := []flowItem{
		{a.lPlayer, S(50)}, {a.cbPlayer, S(180)}, {a.bPlayers, S(84)},
		{a.lMode, S(44)}, {a.cbMode, S(110)}, {a.bGamemode, S(112)},
		{a.bTpMe, S(126)}, {a.bBring, S(156)},
	}
	ph := flow(pItems, rx, 0, rw, bh, g, false)
	py := hy - S(10) - ph
	flow(pItems, rx, py, rw, bh, g, true)
	a.fixComboHeights(pItems, rx, py, rw, bh, g)
	Move(a.lPlayersHdr, rx, py-S(26), S(200), S(24))
	Move(a.sep1, rx, py-S(30), rw, 2)

	// command row + feedback
	fy := py - S(36) - S(24)
	Move(a.lFeedback, rx, fy, rw, S(24))
	cy := fy - g - S(28)
	Move(a.eCmd, rx, cy, rw-S(90)-g, S(28))
	Move(a.bSend, rx+rw-S(90), cy, S(90), S(28))
	Move(a.console, rx, consoleTop, rw, cy-g-consoleTop)
}

// Combo boxes need a tall window height for their drop-down list; re-move them with it.
func (a *App) fixComboHeights(items []flowItem, x, y, width, rowH, gap int) {
	cx, cy := x, y
	for _, it := range items {
		if cx > x && cx+it.w > x+width {
			cx = x
			cy += rowH + gap
		}
		if it.h == a.cbPlayer || it.h == a.cbMode {
			Move(it.h, cx, cy+S(2), it.w, S(260))
		}
		cx += it.w + gap
	}
}

func (a *App) setStatus(part int, s string) { SendMessageStr(a.status, SB_SETTEXTW, uintptr(part), s) }

func (a *App) onColor(hdc, ctl uintptr) uintptr {
	var c uint32
	switch ctl {
	case a.lFeedback:
		c = a.feedbackColor
	case a.lState:
		c = a.stateColor
	default:
		return 0
	}
	pSetTextColor.Call(hdc, uintptr(c))
	pSetBkMode.Call(hdc, TRANSPARENT)
	return btnFaceBrush
}

func (a *App) feedback(s string, c uint32) {
	a.feedbackColor = c
	SetText(a.lFeedback, s)
	pInvalidateRect.Call(a.lFeedback, 0, 1)
}

var (
	fbOK   = RGB(0, 128, 40)
	fbErr  = RGB(200, 0, 0)
	fbInfo = RGB(80, 80, 90)
	fbBusy = RGB(0, 90, 170)
)

// ---------------- events ----------------

func (a *App) onCommand(id, code int, ctl uintptr) {
	if id >= idQuickBase && id < idQuickBase+len(a.cfg.QuickCommands) {
		if code == 0 {
			a.runQuick(a.cfg.QuickCommands[id-idQuickBase])
		}
		return
	}
	if code != 0 && ctl != 0 { // only button clicks / menu items
		return
	}
	switch id {
	case idMenuSettings:
		a.openSettings()
	case idMenuExit:
		PostMessage(a.w.Hwnd, WM_CLOSE, 0, 0)
	case idMenuQuick, idQuickEdit:
		a.manageQuick()
	case idMenuRefresh, idRefresh:
		a.refreshServers()
	case idMenuHelp:
		MsgBox(a.w.Hwnd, "Shortcuts & placeholders",
			"Double-click / Enter on a server  → connect\r\n"+
				"F5  → refresh server list\r\n"+
				"Enter in command box  → send,  ↑/↓  → history,  Ctrl+L → clear console\r\n\r\n"+
				"Quick command placeholders:\r\n"+
				"  {player}  value of the Player box\r\n"+
				"  {me}  your in-game name (Settings)\r\n"+
				"  {input}  asks you when clicked\r\n"+
				"  {server}  connected server name\r\n"+
				"Multiple lines = multiple commands, sent in order.\r\n\r\n"+
				"Feedback line: green ✔ = server replied OK, red ✖ = error text found in the reply.", MB_ICONINFORMATION)
	case idConnect:
		a.connectSelected()
	case idStart:
		a.power("start")
	case idStop:
		a.power("stop")
	case idRestart:
		a.power("restart")
	case idKill:
		a.power("kill")
	case idBackup:
		a.backup()
	case idClear:
		a.clearConsole()
	case idSend:
		a.sendFromInput()
	case idPlayers:
		a.refreshPlayers()
	case idGamemode:
		a.runTemplate("Gamemode", "gamemode "+modes[max(0, comboSel(a.cbMode))]+" {player}")
	case idTpMe:
		a.runTemplate("TP me → player", "tp {me} {player}")
	case idBring:
		a.runTemplate("Summon player", "tp {player} {me}")
	}
}

func (a *App) onNotify(nm *NMHDR) (uintptr, bool) {
	if nm.HwndFrom == a.lv && int32(nm.Code) == NM_DBLCLK {
		a.connectSelected()
		return 0, true
	}
	return 0, false
}

func (a *App) updateButtons() {
	on := a.cur != nil
	for _, h := range []uintptr{a.bStart, a.bStop, a.bRestart, a.bKill, a.bBackup, a.bSend, a.bPlayers, a.bGamemode, a.bTpMe, a.bBring} {
		Enable(h, on)
	}
	for _, h := range a.quickBtns {
		Enable(h, on)
	}
}

// ---------------- config / servers ----------------

func (a *App) applyConfig() {
	a.client = pelican.New(a.cfg.PanelURL, a.cfg.apiKey)
	a.setStatus(2, strings.TrimPrefix(strings.TrimPrefix(a.cfg.PanelURL, "https://"), "http://"))
	a.refreshServers()
}

func (a *App) refreshServers() {
	if a.client == nil {
		a.feedback("Set the panel URL and API key in File → Settings.", fbErr)
		return
	}
	c, admin := a.client, a.cfg.AdminAll
	a.setStatus(1, "Loading servers…")
	Enable(a.bRefresh, false)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		list, err := c.ListServers(ctx, admin)
		states := make([]string, len(list))
		if err == nil {
			var wg sync.WaitGroup
			sem := make(chan struct{}, 8)
			for i := range list {
				wg.Add(1)
				go func(i int) {
					defer wg.Done()
					sem <- struct{}{}
					defer func() { <-sem }()
					s := list[i]
					switch {
					case s.IsSuspended:
						states[i] = "suspended"
						return
					case s.IsInstalling || s.Status == "installing":
						states[i] = "installing"
						return
					}
					rctx, rc := context.WithTimeout(ctx, 10*time.Second)
					defer rc()
					if r, e := c.Resources(rctx, s.Identifier); e == nil {
						states[i] = r.CurrentState
					} else {
						states[i] = "?"
					}
				}(i)
			}
			wg.Wait()
		}
		Post(func() {
			Enable(a.bRefresh, true)
			if err != nil {
				a.setStatus(1, "Server list failed")
				a.feedback("✖ Server list: "+err.Error(), fbErr)
				return
			}
			st := map[string]string{}
			for i, s := range list {
				st[s.Identifier] = states[i]
			}
			sort.SliceStable(list, func(i, j int) bool { return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name) })
			a.servers, a.states = list, st
			if a.cur != nil && a.curState != "" {
				a.states[a.cur.Identifier] = a.curState
			}
			a.fillList()
			a.setStatus(1, fmt.Sprintf("%d servers", len(list)))
			if a.cur == nil && a.cfg.LastServer != "" {
				for i, s := range a.servers {
					if s.Identifier == a.cfg.LastServer {
						lvSelect(a.lv, i)
						a.connect(s)
						break
					}
				}
			}
		})
	}()
}

func (a *App) fillList() {
	sel := ""
	if i := lvSelected(a.lv); i >= 0 && i < len(a.servers) {
		sel = a.servers[i].Identifier
	} else if a.cur != nil {
		sel = a.cur.Identifier
	}
	SendMessage(a.lv, WM_SETREDRAW, 0, 0)
	SendMessage(a.lv, LVM_DELETEALLITEMS, 0, 0)
	for i, s := range a.servers {
		name := s.Name
		if a.cur != nil && a.cur.Identifier == s.Identifier {
			name = "● " + name
		}
		lvAddRow(a.lv, i, name, a.states[s.Identifier], s.Node)
		if s.Identifier == sel {
			lvSelect(a.lv, i)
		}
	}
	SendMessage(a.lv, WM_SETREDRAW, 1, 0)
	pInvalidateRect.Call(a.lv, 0, 1)
}

func (a *App) setRowState(id, state string) {
	a.states[id] = state
	for i, s := range a.servers {
		if s.Identifier == id {
			lvSetText(a.lv, i, 1, state)
		}
	}
}

// ---------------- console connection ----------------

func (a *App) connectSelected() {
	i := lvSelected(a.lv)
	if i < 0 || i >= len(a.servers) {
		a.feedback("Select a server in the list first.", fbErr)
		return
	}
	if a.client == nil {
		return
	}
	a.connect(a.servers[i])
}

func (a *App) connect(s pelican.Server) {
	if a.con != nil {
		a.con.Close()
		a.con = nil
	}
	a.gen++
	gen := a.gen
	sc := s
	a.cur = &sc
	a.connected = false
	a.curState = a.states[s.Identifier]
	a.capActive = false
	a.plParser = nil
	a.cfg.LastServer = s.Identifier
	_ = a.cfg.Save()

	a.clearConsole()
	SetText(a.lName, s.Name)
	SetText(a.lStats, "")
	a.setState(a.curState)
	a.setStatus(0, "Connecting to console…")
	a.appendConsole([]pendLine{{gen, fmt.Sprintf("── Connecting to %s (%s) on %s ──", s.Name, s.Identifier, s.Node), lineInfo}})
	a.feedback("", fbInfo)
	a.fillList()
	a.updateButtons()
	comboSet(a.cbPlayer, nil)

	a.con = a.client.OpenConsole(s.Identifier, pelican.ConsoleHandlers{
		OnLine: func(l string, k pelican.LineKind) { a.queueLine(gen, l, k) },
		OnStatus: func(st string) {
			Post(func() {
				if gen == a.gen {
					a.setState(st)
				}
			})
		},
		OnStats: func(st pelican.Stats) {
			Post(func() {
				if gen == a.gen {
					a.showStats(st)
				}
			})
		},
		OnConn: func(ok bool, err error) {
			Post(func() {
				if gen != a.gen {
					return
				}
				a.connected = ok
				if ok {
					a.setStatus(0, "● Console connected")
				} else {
					msg := "Console disconnected — retrying"
					if err != nil {
						msg += ": " + err.Error()
						a.appendConsole([]pendLine{{gen, "── " + err.Error() + " — reconnecting… ──", pelican.LineError}})
					}
					a.setStatus(0, msg)
				}
			})
		},
		OnEvent: func(ev string, args []string) {
			Post(func() {
				if gen == a.gen {
					a.onWSEvent(ev, args)
				}
			})
		},
	})
}

const lineInfo pelican.LineKind = 100
const lineEcho pelican.LineKind = 101

func (a *App) queueLine(gen int, text string, k pelican.LineKind) {
	a.pendMu.Lock()
	a.pending = append(a.pending, pendLine{gen, text, k})
	sched := !a.pendSched
	a.pendSched = true
	a.pendMu.Unlock()
	if sched {
		time.AfterFunc(40*time.Millisecond, func() { Post(a.flushPending) })
	}
}

func (a *App) flushPending() {
	a.pendMu.Lock()
	lines := a.pending
	a.pending = nil
	a.pendSched = false
	a.pendMu.Unlock()
	var cur []pendLine
	for _, l := range lines {
		if l.gen == a.gen {
			cur = append(cur, l)
		}
	}
	if len(cur) == 0 {
		return
	}
	a.appendConsole(cur)
	for _, l := range cur {
		if l.kind != pelican.LineConsole || strings.TrimSpace(l.text) == "" {
			continue
		}
		if a.capActive {
			a.capLines = append(a.capLines, l.text)
		}
		if a.plParser != nil && a.plParser.Feed(l.text) {
			a.playersDone()
		}
	}
}

func colorFor(l pendLine) uint32 {
	switch l.kind {
	case lineInfo:
		return colInfo
	case lineEcho:
		return colEcho
	case pelican.LineDaemon:
		return colDaemon
	case pelican.LineInstall:
		return colInstall
	case pelican.LineError:
		return colError
	}
	if pelican.IsErrorLine(l.text) {
		return colError
	}
	if pelican.IsWarnLine(l.text) {
		return colWarn
	}
	return colText
}

func (a *App) textLen() int32 {
	g := GETTEXTLENGTHEX{Flags: 8 | 2, Codepage: 1200} // GTL_NUMCHARS|GTL_PRECISE
	return int32(SendMessage(a.console, EM_GETTEXTLENGTHEX, uintptr(unsafe.Pointer(&g)), 0))
}

func (a *App) appendConsole(lines []pendLine) {
	h := a.console
	auto := IsChecked(a.chkAuto)
	var sel CHARRANGE
	var scroll POINT
	SendMessage(h, EM_EXGETSEL, 0, uintptr(unsafe.Pointer(&sel)))
	SendMessage(h, EM_GETSCROLLPOS, 0, uintptr(unsafe.Pointer(&scroll)))
	SendMessage(h, WM_SETREDRAW, 0, 0)
	SendMessage(h, EM_HIDESELECTION, 1, 0)

	// group consecutive lines with the same colour
	i := 0
	for i < len(lines) {
		c := colorFor(lines[i])
		var sb strings.Builder
		j := i
		for j < len(lines) && colorFor(lines[j]) == c {
			sb.WriteString(lines[j].text)
			sb.WriteString("\r\n")
			j++
		}
		end := a.textLen()
		cr := CHARRANGE{end, end}
		SendMessage(h, EM_EXSETSEL, 0, uintptr(unsafe.Pointer(&cr)))
		cf := CHARFORMATW{DwMask: CFM_COLOR, CrTextColor: c}
		cf.CbSize = uint32(unsafe.Sizeof(cf))
		SendMessage(h, EM_SETCHARFORMAT, SCF_SELECTION, uintptr(unsafe.Pointer(&cf)))
		SendMessageStr(h, EM_REPLACESEL, 0, sb.String())
		i = j
	}

	// trim to keep the control fast
	if n := int(SendMessage(h, EM_GETLINECOUNT, 0, 0)); n > 6000 {
		cut := int32(SendMessage(h, EM_LINEINDEX, uintptr(n-4000), 0))
		cr := CHARRANGE{0, cut}
		SendMessage(h, EM_EXSETSEL, 0, uintptr(unsafe.Pointer(&cr)))
		SendMessageStr(h, EM_REPLACESEL, 0, "")
		sel.CpMin = max(0, sel.CpMin-cut)
		sel.CpMax = max(0, sel.CpMax-cut)
	}

	SendMessage(h, EM_EXSETSEL, 0, uintptr(unsafe.Pointer(&sel)))
	SendMessage(h, EM_HIDESELECTION, 0, 0)
	if !auto {
		SendMessage(h, EM_SETSCROLLPOS, 0, uintptr(unsafe.Pointer(&scroll)))
	}
	SendMessage(h, WM_SETREDRAW, 1, 0)
	if auto {
		SendMessage(h, WM_VSCROLL, SB_BOTTOM, 0)
	}
	pInvalidateRect.Call(h, 0, 1)
}

func (a *App) clearConsole() {
	SetText(a.console, "")
	a.setDefaultCharFormat()
}

func (a *App) setState(st string) {
	if st == "" {
		st = "unknown"
	}
	a.curState = st
	switch st {
	case "running":
		a.stateColor = RGB(0, 140, 60)
	case "offline":
		a.stateColor = RGB(200, 0, 0)
	case "starting", "stopping":
		a.stateColor = RGB(210, 120, 0)
	default:
		a.stateColor = RGB(90, 90, 90)
	}
	SetText(a.lState, "● "+strings.ToUpper(st))
	pInvalidateRect.Call(a.lState, 0, 1)
	if a.cur != nil {
		a.setRowState(a.cur.Identifier, st)
	}
	if st == "offline" {
		SetText(a.lStats, "")
	}
}

func (a *App) showStats(st pelican.Stats) {
	mem := pelican.HumanBytes(st.MemoryBytes)
	if st.MemoryLimitBytes > 0 {
		mem += " / " + pelican.HumanBytes(st.MemoryLimitBytes)
	}
	s := fmt.Sprintf("CPU %.1f%%   RAM %s   Disk %s", st.CPUAbsolute, mem, pelican.HumanBytes(st.DiskBytes))
	if st.Uptime > 0 {
		s += "   Up " + pelican.HumanUptime(st.Uptime)
	}
	SetText(a.lStats, s)
}

func (a *App) onWSEvent(ev string, args []string) {
	switch ev {
	case "backup completed":
		var b struct {
			IsSuccessful bool  `json:"is_successful"`
			FileSize     int64 `json:"file_size"`
		}
		if len(args) > 0 {
			_ = json.Unmarshal([]byte(args[0]), &b)
		}
		if b.IsSuccessful {
			msg := "✔ Backup completed"
			if b.FileSize > 0 {
				msg += " (" + pelican.HumanBytes(b.FileSize) + ")"
			}
			a.feedback(msg, fbOK)
			a.appendConsole([]pendLine{{a.gen, "── " + msg + " ──", lineInfo}})
		} else {
			a.feedback("✖ Backup failed — check the panel's Backups tab", fbErr)
			a.appendConsole([]pendLine{{a.gen, "── Backup failed ──", pelican.LineError}})
		}
	}
}

// ---------------- commands ----------------

func (a *App) sendFromInput() {
	cmd := strings.TrimSpace(GetText(a.eCmd))
	cmd = strings.TrimPrefix(cmd, "/")
	if cmd == "" {
		return
	}
	if len(a.history) == 0 || a.history[len(a.history)-1] != cmd {
		a.history = append(a.history, cmd)
		if len(a.history) > 200 {
			a.history = a.history[1:]
		}
	}
	a.histIdx = len(a.history)
	SetText(a.eCmd, "")
	a.sendCommands([]string{cmd}, cmd)
}

func (a *App) runQuick(q QuickCmd) { a.runTemplate(q.Name, q.Command) }

// runTemplate expands placeholders and sends each line as a command.
func (a *App) runTemplate(label, tmpl string) {
	if a.cur == nil {
		a.feedback("Connect to a server first.", fbErr)
		return
	}
	text := tmpl
	if strings.Contains(text, "{player}") {
		p := strings.TrimSpace(GetText(a.cbPlayer))
		if p == "" {
			a.feedback("✖ "+label+": enter or pick a player in the Player box first.", fbErr)
			Focus(a.cbPlayer)
			return
		}
		text = strings.ReplaceAll(text, "{player}", quoteName(p))
	}
	if strings.Contains(text, "{me}") {
		if strings.TrimSpace(a.cfg.IGN) == "" {
			a.feedback("✖ "+label+": set your in-game name in File → Settings.", fbErr)
			return
		}
		text = strings.ReplaceAll(text, "{me}", quoteName(a.cfg.IGN))
	}
	text = strings.ReplaceAll(text, "{server}", a.cur.Name)
	if strings.Contains(text, "{input}") {
		v, ok := promptText(a.w.Hwnd, label, label+" — value for {input}:", "")
		if !ok {
			return
		}
		text = strings.ReplaceAll(text, "{input}", v)
	}
	var cmds []string
	for _, ln := range strings.Split(text, "\n") {
		ln = strings.TrimPrefix(strings.TrimSpace(ln), "/")
		if ln != "" {
			cmds = append(cmds, ln)
		}
	}
	if len(cmds) == 0 {
		return
	}
	a.sendCommands(cmds, label)
}

// quoteName wraps names containing spaces (Bedrock gamertags) in quotes.
func quoteName(n string) string {
	n = strings.TrimSpace(n)
	if strings.ContainsAny(n, " ") && !strings.HasPrefix(n, "\"") {
		return "\"" + n + "\""
	}
	return n
}

func (a *App) sendCommands(cmds []string, label string) {
	if a.cur == nil {
		a.feedback("Connect to a server first.", fbErr)
		return
	}
	echo := make([]pendLine, len(cmds))
	for i, c := range cmds {
		echo[i] = pendLine{a.gen, "> " + c, lineEcho}
	}
	a.appendConsole(echo)

	a.capGen++
	capGen := a.capGen
	a.capActive = true
	a.capLabel = label
	a.capLines = nil
	a.feedback("… "+label, fbBusy)

	c, id := a.client, a.cur.Identifier
	go func() {
		var err error
		for i, cmd := range cmds {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			err = c.SendCommand(ctx, id, cmd)
			cancel()
			if err != nil {
				if len(cmds) > 1 {
					err = fmt.Errorf("command %d (%s): %w", i+1, cmd, err)
				}
				break
			}
			if i < len(cmds)-1 {
				time.Sleep(200 * time.Millisecond)
			}
		}
		Post(func() {
			if capGen != a.capGen {
				return
			}
			if err != nil {
				a.capActive = false
				a.feedback("✖ "+label+": "+err.Error(), fbErr)
				return
			}
			if !a.connected {
				a.capActive = false
				a.feedback("✔ Sent: "+label+"  (console not connected — no output to check)", fbOK)
				return
			}
			time.AfterFunc(1500*time.Millisecond, func() {
				Post(func() {
					if capGen == a.capGen {
						a.finishCapture()
					}
				})
			})
		})
	}()
}

func (a *App) finishCapture() {
	a.capActive = false
	label := a.capLabel
	for _, l := range a.capLines {
		if pelican.IsErrorLine(l) {
			a.feedback("✖ "+label+": "+cleanLine(l), fbErr)
			return
		}
	}
	if len(a.capLines) > 0 {
		a.feedback("✔ "+label+": "+cleanLine(a.capLines[0]), fbOK)
		return
	}
	a.feedback("✔ Sent: "+label+"  (no console reply)", fbInfo)
}

func cleanLine(s string) string {
	s = strings.TrimSpace(s)
	for strings.HasPrefix(s, "[") {
		i := strings.Index(s, "]")
		if i < 0 {
			break
		}
		s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s[i+1:]), ":"))
	}
	if len(s) > 220 {
		s = s[:220] + "…"
	}
	return s
}

func (a *App) refreshPlayers() {
	if a.cur == nil {
		return
	}
	if !a.connected {
		a.feedback("✖ Console not connected — can't read the player list.", fbErr)
		return
	}
	a.plParser = &pelican.PlayerListParser{}
	a.plGen++
	g := a.plGen
	a.appendConsole([]pendLine{{a.gen, "> list", lineEcho}})
	a.feedback("… fetching online players", fbBusy)
	c, id := a.client, a.cur.Identifier
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err := c.SendCommand(ctx, id, "list")
		cancel()
		Post(func() {
			if err != nil && g == a.plGen {
				a.plParser = nil
				a.feedback("✖ list: "+err.Error(), fbErr)
			}
		})
		time.Sleep(4 * time.Second)
		Post(func() {
			if g == a.plGen && a.plParser != nil {
				a.plParser = nil
				a.feedback("✖ Couldn't read a player list from the console reply.", fbErr)
			}
		})
	}()
}

func (a *App) playersDone() {
	p := a.plParser
	a.plParser = nil
	text := GetText(a.cbPlayer)
	comboSet(a.cbPlayer, p.Players)
	SetText(a.cbPlayer, text)
	if text == "" && len(p.Players) > 0 {
		SendMessage(a.cbPlayer, CB_SETCURSEL, 0, 0)
	}
	if len(p.Players) == 0 {
		a.feedback("✔ No players online", fbInfo)
	} else {
		a.feedback(fmt.Sprintf("✔ %d online: %s", len(p.Players), strings.Join(p.Players, ", ")), fbOK)
	}
}

// ---------------- power / backup ----------------

func (a *App) power(sig string) {
	if a.cur == nil {
		return
	}
	if sig != "start" {
		warn := map[string]string{
			"stop":    "Stop %s?",
			"restart": "Restart %s?",
			"kill":    "Force-kill %s?\r\n\r\nThis does not save the world — data since the last save can be lost.",
		}[sig]
		if MsgBox(a.w.Hwnd, "Confirm "+sig, fmt.Sprintf(warn, a.cur.Name), MB_YESNO|MB_ICONWARNING) != IDYES {
			return
		}
	}
	a.feedback("… "+sig+" signal", fbBusy)
	c, id, name := a.client, a.cur.Identifier, a.cur.Name
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		err := c.Power(ctx, id, sig)
		cancel()
		Post(func() {
			if err != nil {
				a.feedback("✖ "+sig+": "+err.Error(), fbErr)
				return
			}
			a.feedback("✔ "+strings.ToUpper(sig[:1])+sig[1:]+" signal sent to "+name, fbOK)
		})
	}()
}

func (a *App) backup() {
	if a.cur == nil {
		return
	}
	def := "Console backup " + time.Now().Format("2006-01-02 15:04")
	name, ok := promptText(a.w.Hwnd, "Create backup", "Backup name for "+a.cur.Name+" (optional):", def)
	if !ok {
		return
	}
	a.feedback("… creating backup", fbBusy)
	c, id := a.client, a.cur.Identifier
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		b, err := c.CreateBackup(ctx, id, name)
		cancel()
		Post(func() {
			if err != nil {
				a.feedback("✖ Backup: "+err.Error(), fbErr)
				return
			}
			n := b.Name
			if n == "" {
				n = name
			}
			a.feedback("… Backup “"+n+"” started — waiting for Wings to finish", fbBusy)
			a.appendConsole([]pendLine{{a.gen, "── Backup started: " + n + " ──", lineInfo}})
		})
	}()
}

// ---------------- quick buttons ----------------

func (a *App) rebuildQuick() {
	for _, h := range a.quickBtns {
		Destroy(h)
	}
	a.quickBtns = nil
	for i, q := range a.cfg.QuickCommands {
		h := a.w.Button(q.Name, idQuickBase+i)
		a.quickBtns = append(a.quickBtns, h)
	}
	a.updateButtons()
	r := ClientRect(a.w.Hwnd)
	a.layout(int(r.Right), int(r.Bottom))
}
