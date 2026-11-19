//go:build windows

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unsafe"

	"zephycube/pelicanconsole/pelican"
	"zephycube/pelicanconsole/update"
)

// appVersion is overridden at build time: -ldflags "-X main.appVersion=1.4.0"
var appVersion = "1.4.0"

const appTitle = "ZephyCube Pelican Console"
const mainClass = "ZCPelicanMain"

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
	idPlayersList
	idPlayersRefresh
	idMode
	idGamemode
	idTP
	idSummon
	idQuickEdit
	idTheme
	idSettings
	idHelp
	idListToggle
	idShowHidden
	idClearInv
	idUpdate
	idAutoUpdate

	idHelpUpdate = 3101 + iota
	idHelpReleases
	idHelpKeys
	idHelpAbout
	idEdition
	idEffect
	idEffApply
	idEffClear
	idItem
	idGive
	idEffDur
	idEffLvl
	idGiveQty

	idCtxConnect = 3001 + iota
	idCtxHide
	idCtxUnhide
	idCtxShowHidden
	idCtxUnhideAll

	idQuickBase = 1000
)

var uiFont, boldFont, headFont, titleFont, capFont, monoFont uintptr

type cardDef struct {
	r       RECT
	caption func() string
}

type App struct {
	cfg    *Config
	client *pelican.Client
	w      *Win
	logo   uintptr

	// header
	bTheme, bSettings, bHelp, bList uintptr
	bShowHidden                     uintptr
	bUpdate                         uintptr
	pendingRel                      *update.Release
	pendingMan                      *update.Manifest
	rows                            []pelican.Server // visible rows in the list view
	showHidden                      bool
	// left
	lv, bRefresh, bConnect     uintptr
	lbPlayers, bPlayersRefresh uintptr
	// right
	lName, lState, lStats                           uintptr
	bStart, bStop, bRestart, bKill, bBackup, bClear uintptr
	tAuto                                           uintptr
	console                                         uintptr
	eCmd, bSend, lFeedback                          uintptr
	lPlayer, cbPlayer                               uintptr
	lMode, cbMode, bGamemode                        uintptr
	lTele, bTP, bSummon                             uintptr
	bClearInv, cbEdition                            uintptr
	lEffect, cbEffect, lFor, eEffDur, lLvl, eEffLvl uintptr
	bEffApply, bEffClear                            uintptr
	lGive, cbItem, lQty, eGiveQty, bGive            uintptr
	quickLabels                                     []uintptr
	bedrock                                         bool
	effView                                         []mcEffect
	itemView                                        []mcItem
	itemFilterBusy                                  bool
	bQuickEdit, lQuickEmpty                         uintptr
	lConn, lCount, lHost                            uintptr
	quickBtns                                       []uintptr

	cards       []cardDef
	groups      []RECT
	playersCard RECT

	servers []pelican.Server
	states  map[string]string

	cur       *pelican.Server
	con       *pelican.Console
	gen       int
	connected bool
	curState  string

	fbKind  int
	players []string

	killVisible bool
	stopGen     int

	history []string
	histIdx int

	capGen    int
	capActive bool
	capLabel  string
	capLines  []string

	plParser *pelican.PlayerListParser
	plSilent bool
	plGen    int

	pendMu    sync.Mutex
	pending   []pendLine
	pendSched bool
}

type pendLine struct {
	gen  int
	text string
	kind pelican.LineKind
}

const (
	fbNone = iota
	fbOK
	fbErr
	fbInfo
	fbBusy
	fbWarn
)

var modes = []string{"survival", "creative", "adventure", "spectator"}

func init() { runtime.LockOSThread() }

func main() {
	a := &App{cfg: loadConfig(), states: map[string]string{}}
	initDPI()
	initDarkModeAPI()
	initCommonControls()
	r, _, _ := pGetModuleHandleW.Call(0)
	hInstance = r
	setTheme(!a.cfg.LightMode)

	uiFont = makeFont("Segoe UI", 9, FW_NORMAL)
	boldFont = makeFont("Segoe UI Semibold", 9, FW_BOLD)
	headFont = makeFont("Segoe UI Semibold", 11, FW_BOLD)
	titleFont = makeFont("Segoe UI Semibold", 10, FW_BOLD)
	capFont = makeFont("Segoe UI Semibold", 8, FW_BOLD)
	monoFont = makeFont("Consolas", 10, FW_NORMAL)

	cx, _, _ := pGetSystemMetrics.Call(11)  // SM_CXICON
	csx, _, _ := pGetSystemMetrics.Call(49) // SM_CXSMICON
	big, small := loadAppIcon(int(cx)), loadAppIcon(int(csx))
	registerClass(mainClass, big, small)
	registerClass(dlgClass, big, small)
	a.logo = loadAppIcon(S(24))

	a.build()
	a.w.ApplyTheme()
	Show(a.w.Hwnd, SW_SHOW)
	pUpdateWindow.Call(a.w.Hwnd)

	cleanupOldExe()
	if from := updatedFromArg(); from != "" {
		Post(func() { a.feedback("✔ Updated from v"+from+" to v"+appVersion, fbOK) })
	}
	if !a.cfg.NoUpdateCheck {
		time.AfterFunc(4*time.Second, func() { Post(func() { a.checkUpdates(false) }) })
	}
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
		if m.Message == WM_KEYDOWN && a.handleKey(m.Hwnd, m.WParam) {
			continue
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
	if vk == 'B' && ctrlDown() {
		a.setListVisible(a.cfg.ListHidden)
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

func surf(hs ...uintptr) {
	for _, h := range hs {
		onSurface[h] = true
	}
}

func (a *App) build() {
	a.w = &Win{font: uiFont, MinW: S(820), MinH: S(560)}
	sw, _, _ := pGetSystemMetrics.Call(16) // SM_CXFULLSCREEN
	shh, _, _ := pGetSystemMetrics.Call(17)
	NewTopWindow(a.w, mainClass, appTitle, WS_OVERLAPPEDWINDOW|WS_CLIPCHILDREN, WS_EX_CONTROLPARENT, min(S(1200), int(sw)*94/100), min(S(760), int(shh)*94/100), 0)
	mainHwnd = a.w.Hwnd
	w := a.w

	// header
	a.bTheme = w.Toggle("Dark mode", idTheme, !a.cfg.LightMode)
	a.bSettings = w.Btn("Settings", idSettings, kNormal)
	a.bHelp = w.Btn("Help", idHelp, kNormal)
	a.bList = w.Toggle("Server list", idListToggle, !a.cfg.ListHidden)
	a.bUpdate = w.Btn("Update", idUpdate, kPrimary)
	Show(a.bUpdate, 0)

	// servers card
	a.lv = w.Ctl("SysListView32", "", WS_TABSTOP|LVS_REPORT|LVS_SINGLESEL|LVS_SHOWSELALWAYS|LVS_NOSORTHEADER|LVS_NOCOLUMNHEADER, 0, idList, roleListView)
	SendMessage(a.lv, LVM_SETEXTENDEDLISTVIEWSTYLE, LVS_EX_FULLROWSELECT|LVS_EX_DOUBLEBUFFER, LVS_EX_FULLROWSELECT|LVS_EX_DOUBLEBUFFER)
	lvAddColumn(a.lv, 0, "Server", S(150))
	lvAddColumn(a.lv, 1, "State", S(70))
	lvAddColumn(a.lv, 2, "Node", S(60))
	a.bRefresh = w.Btn("Refresh", idRefresh, kNormal)
	a.bConnect = w.Btn("Connect", idConnect, kPrimary)
	a.bShowHidden = w.Btn("", idShowHidden, kNormal)
	surf(a.bRefresh, a.bConnect, a.bShowHidden)

	// players card
	a.lbPlayers = w.Ctl("LISTBOX", "", WS_TABSTOP|WS_VSCROLL|LBS_NOTIFY|LBS_NOINTEGRALHEIGHT, 0, idPlayersList, roleListBox)
	a.bPlayersRefresh = w.Btn("↻", idPlayersRefresh, kNormal)
	surf(a.bPlayersRefresh)

	// server header card
	a.lName = w.Label("No server connected", SS_ENDELLIPSIS)
	w.SetCtlFont(a.lName, headFont)
	a.lState = w.Label("", 0)
	w.SetCtlFont(a.lState, boldFont)
	labelColor[a.lState] = a.stateColor
	a.lStats = w.Label("", SS_RIGHT|SS_ENDELLIPSIS)
	Muted(a.lStats)
	a.bStart = w.Btn("▶ Start", idStart, kNormal)
	a.bStop = w.Btn("■ Stop", idStop, kNormal)
	a.bRestart = w.Btn("↻ Restart", idRestart, kNormal)
	a.bKill = w.Btn("✖ Kill", idKill, kDanger)
	Show(a.bKill, 0)
	a.bBackup = w.Btn("Backup…", idBackup, kNormal)
	a.bClear = w.Btn("Clear", idClear, kNormal)
	a.tAuto = w.Toggle("Auto-scroll", idAutoScroll, true)
	surf(a.lName, a.lState, a.lStats, a.bStart, a.bStop, a.bRestart, a.bKill, a.bBackup, a.bClear, a.tAuto)

	// console
	a.console = w.Ctl("RICHEDIT50W", "", WS_TABSTOP|WS_VSCROLL|ES_MULTILINE|ES_READONLY|ES_AUTOVSCROLL, WS_EX_CLIENTEDGE, 0, roleRich)
	SendMessage(a.console, EM_EXLIMITTEXT, 0, 64<<20)
	SendMessage(a.console, EM_SETBKGNDCOLOR, 0, uintptr(colBg))
	a.setDefaultCharFormat()

	a.eCmd = w.Edit("", 0, idCmd)
	SetFont(a.eCmd, monoFont)
	SetCue(a.eCmd, "Console command — Enter to send, ↑/↓ history")
	a.bSend = w.Btn("Send", idSend, kPrimary)
	a.lFeedback = w.Label("", SS_ENDELLIPSIS)
	w.SetCtlFont(a.lFeedback, boldFont)
	labelColor[a.lFeedback] = a.feedbackColor

	// player actions card
	a.lPlayer = w.Label("Player", 0)
	a.cbPlayer = w.Ctl("COMBOBOX", "", WS_TABSTOP|WS_VSCROLL|CBS_DROPDOWN|CBS_AUTOHSCROLL, 0, idPlayer, roleCombo)
	a.bClearInv = w.Btn("Clear inventory", idClearInv, kNormal)
	a.cbEdition = w.Ctl("COMBOBOX", "", WS_TABSTOP|CBS_DROPDOWNLIST, 0, idEdition, roleCombo)
	comboSet(a.cbEdition, []string{"Java", "Bedrock"})
	SendMessage(a.cbEdition, CB_SETCURSEL, 0, 0)
	a.lMode = w.Label("Gamemode", 0)
	a.cbMode = w.Ctl("COMBOBOX", "", WS_TABSTOP|WS_VSCROLL|CBS_DROPDOWNLIST, 0, idMode, roleCombo)
	comboSet(a.cbMode, modes)
	SendMessage(a.cbMode, CB_SETCURSEL, 1, 0)
	a.bGamemode = w.Btn("Set", idGamemode, kSegment)
	a.lTele = w.Label("Teleport", 0)
	a.bTP = w.Btn("TP", idTP, kSegment)
	a.bSummon = w.Btn("Summon", idSummon, kSegment)
	a.lEffect = w.Label("Effect", 0)
	a.cbEffect = w.Ctl("COMBOBOX", "", WS_TABSTOP|WS_VSCROLL|CBS_DROPDOWNLIST, 0, idEffect, roleCombo)
	a.lFor = w.Label("for", SS_RIGHT)
	a.eEffDur = w.Edit("infinite", 0, idEffDur)
	SetCue(a.eEffDur, "seconds")
	a.lLvl = w.Label("lvl", SS_RIGHT)
	a.eEffLvl = w.Edit("1", 0, idEffLvl)
	a.bEffApply = w.Btn("Apply", idEffApply, kSegment)
	a.bEffClear = w.Btn("Clear", idEffClear, kSegment)
	a.lGive = w.Label("Give", 0)
	a.cbItem = w.Ctl("COMBOBOX", "", WS_TABSTOP|WS_VSCROLL|CBS_DROPDOWN|CBS_AUTOHSCROLL, 0, idItem, roleCombo)
	a.lQty = w.Label("×", SS_RIGHT)
	a.eGiveQty = w.Edit("64", 0, idGiveQty)
	a.bGive = w.Btn("Give", idGive, kSegment)
	for _, h := range []uintptr{a.lPlayer, a.lMode, a.lTele, a.lEffect, a.lFor, a.lLvl, a.lGive, a.lQty} {
		Muted(h)
	}
	surf(a.lPlayer, a.bClearInv, a.lMode, a.bGamemode, a.lTele, a.bTP, a.bSummon,
		a.lEffect, a.lFor, a.lLvl, a.bEffApply, a.bEffClear, a.lGive, a.lQty, a.bGive)
	a.setEdition(false)

	// quick card
	a.bQuickEdit = w.Btn("Edit…", idQuickEdit, kNormal)
	a.lQuickEmpty = w.Label("No quick commands — click “Edit buttons…” to add some.", 0)
	Muted(a.lQuickEmpty)
	surf(a.bQuickEdit, a.lQuickEmpty)

	// status row
	a.lConn = w.Label("○ Disconnected", 0)
	a.lCount = w.Label("", 0)
	a.lHost = w.Label("v"+appVersion, SS_RIGHT)
	for _, h := range []uintptr{a.lConn, a.lCount, a.lHost} {
		Muted(h)
	}

	w.OnSize = func(cw, ch int) { a.layout(cw, ch) }
	w.OnCommand = a.onCommand
	w.OnNotify = a.onNotify
	w.OnPaint = a.paint
	w.OnClose = func() {
		if a.con != nil {
			a.con.Close()
		}
		Destroy(w.Hwnd)
	}
	w.OnDestroy = func() { pPostQuitMessage.Call(0) }

	a.rebuildQuick()
	a.updateButtons()
}

// console palette (always dark, terminal style)
var (
	colBg      = RGB(10, 10, 12)
	colText    = RGB(222, 222, 226)
	colError   = RGB(255, 99, 99)
	colWarn    = RGB(240, 196, 80)
	colDaemon  = RGB(110, 180, 255)
	colInstall = RGB(150, 150, 160)
	colEcho    = RGB(255, 170, 200)
	colInfo    = RGB(232, 70, 130)
	colJoin    = RGB(90, 210, 140)
)

func (a *App) setDefaultCharFormat() {
	cf := CHARFORMATW{DwMask: CFM_COLOR | CFM_FACE | CFM_SIZE, YHeight: 200, CrTextColor: colText}
	cf.CbSize = uint32(unsafe.Sizeof(cf))
	copy(cf.SzFaceName[:], utf16.Encode([]rune("Consolas")))
	SendMessage(a.console, EM_SETCHARFORMAT, SCF_ALL, uintptr(unsafe.Pointer(&cf)))
	SendMessage(a.console, EM_SETCHARFORMAT, 0, uintptr(unsafe.Pointer(&cf)))
}

// ---------------- theme ----------------

func (a *App) setDark(dark, save bool) {
	setTheme(dark)
	SetOn(a.bTheme, dark)
	a.w.ApplyTheme()
	if save {
		a.cfg.LightMode = !dark
		_ = a.cfg.Save()
	}
}

func (a *App) stateColor() uint32 {
	switch a.curState {
	case "running":
		return th.Ok
	case "offline":
		return th.Danger
	case "starting", "stopping":
		return th.Warn
	}
	return th.Muted
}

func (a *App) feedbackColor() uint32 {
	switch a.fbKind {
	case fbOK:
		return th.Ok
	case fbErr:
		return th.Danger
	case fbBusy:
		return th.AccentSoft
	case fbWarn:
		return th.Warn
	}
	return th.Muted
}

func (a *App) feedback(s string, kind int) {
	a.fbKind = kind
	SetText(a.lFeedback, s)
	pInvalidateRect.Call(a.lFeedback, 0, 1)
}

// ---------------- layout & paint ----------------

type layoutItem struct {
	h     uintptr
	w     int
	combo bool
}
type block struct {
	items    []layoutItem
	outlined bool
}

func blockWidth(b block, gi, op int) int {
	w := 0
	for i, it := range b.items {
		if i > 0 {
			w += gi
		}
		w += it.w
	}
	if b.outlined {
		w += 2 * op
	}
	return w
}

// flowBlocks lays out blocks left-to-right with wrapping; returns height and outline rects.
func flowBlocks(blocks []block, x, y, width, bh, gap, gi, op int, apply bool) (int, []RECT) {
	rowH := bh + 2*op
	cx, cy := x, y
	var outs []RECT
	for _, b := range blocks {
		bw := blockWidth(b, gi, op)
		if cx > x && cx+bw > x+width {
			cx = x
			cy += rowH + gap
		}
		if apply {
			ix := cx
			if b.outlined {
				outs = append(outs, RECT{int32(cx), int32(cy), int32(cx + bw), int32(cy + rowH)})
				ix += op
			}
			for i, it := range b.items {
				if i > 0 {
					ix += gi
				}
				if it.combo {
					Move(it.h, ix, cy+op+S(3), it.w, S(320))
				} else {
					Move(it.h, ix, cy+op, it.w, bh)
				}
				ix += it.w
			}
		}
		cx += bw + gap
	}
	return cy + rowH - y, outs
}

func (a *App) layout(cw, ch int) {
	if cw == 0 || ch == 0 {
		return
	}
	m, g, bh, pad, capH := S(8), S(6), S(26), S(8), S(22)
	topH := S(38)
	statusH := S(20)
	bottom := ch - statusH
	a.cards = a.cards[:0]
	a.groups = a.groups[:0]

	// header buttons (right)
	x := cw - m
	for _, it := range []struct {
		h uintptr
		w int
	}{{a.bHelp, S(54)}, {a.bSettings, S(74)}, {a.bTheme, S(98)}, {a.bList, S(98)}, {a.bUpdate, S(120)}} {
		if it.h == a.bUpdate && a.pendingMan == nil {
			continue
		}
		x -= it.w
		Move(it.h, x, (topH-bh)/2, it.w, bh)
		x -= g
	}

	// status row
	Move(a.lConn, m+S(2), bottom, S(380), statusH)
	Move(a.lCount, m+S(390), bottom, S(200), statusH)
	Move(a.lHost, cw-m-S(200), bottom, S(200), statusH)

	// ---- left column ----
	lw := S(220)
	playersH := S(170)
	in := m + pad
	iw := lw - 2*pad
	pcTop := int32(topH)
	listCtls := []uintptr{a.lv, a.bRefresh, a.bConnect, a.bShowHidden}
	if a.cfg.ListHidden {
		for _, h := range listCtls {
			Show(h, 0)
		}
	} else {
		for _, h := range listCtls {
			Show(h, SW_SHOW)
		}
		sc := RECT{int32(m), int32(topH), int32(m + lw), int32(bottom - g - playersH)}
		a.cards = append(a.cards, cardDef{sc, func() string { return "SERVERS" }})
		if nh := a.hiddenCount(); nh == 0 {
			Show(a.bShowHidden, 0)
		} else {
			if a.showHidden {
				SetText(a.bShowHidden, "Hide hidden")
			} else {
				SetText(a.bShowHidden, fmt.Sprintf("+%d hidden", nh))
			}
			Move(a.bShowHidden, int(sc.Right)-pad-S(84), int(sc.Top)+S(3), S(84), S(18))
		}
		btnY := int(sc.Bottom) - pad - bh
		Move(a.lv, in, int(sc.Top)+capH, iw, btnY-g-(int(sc.Top)+capH))
		half := (iw - g) / 2
		Move(a.bRefresh, in, btnY, half, bh)
		Move(a.bConnect, in+half+g, btnY, iw-half-g, bh)
		SendMessage(a.lv, LVM_SETCOLUMNWIDTH, 0, uintptr(iw-S(62)-S(44)-S(4)))
		SendMessage(a.lv, LVM_SETCOLUMNWIDTH, 1, uintptr(S(62)))
		SendMessage(a.lv, LVM_SETCOLUMNWIDTH, 2, uintptr(S(44)))
		pcTop = sc.Bottom + int32(g)
	}
	pc := RECT{int32(m), pcTop, int32(m + lw), int32(bottom)}
	a.playersCard = pc
	a.cards = append(a.cards, cardDef{pc, func() string {
		if a.cur == nil {
			return "ONLINE PLAYERS"
		}
		return fmt.Sprintf("ONLINE PLAYERS · %d", len(a.players))
	}})
	Move(a.bPlayersRefresh, int(pc.Right)-pad-S(26), int(pc.Top)+S(3), S(26), S(18))
	Move(a.lbPlayers, in, int(pc.Top)+capH, iw, int(pc.Bottom)-pad-(int(pc.Top)+capH))

	// ---- right column ----
	rx := m + lw + g
	rw := cw - rx - m
	irx, irw := rx+pad, rw-2*pad

	// server header card: one row name/state/stats, one row of buttons
	hc := RECT{int32(rx), int32(topH), int32(rx + rw), int32(topH + pad + S(22) + S(4) + bh + pad)}
	a.cards = append(a.cards, cardDef{hc, nil})
	ty := int(hc.Top) + pad
	nameW := irw * 38 / 100
	Move(a.lName, irx, ty, nameW, S(22))
	Move(a.lState, irx+nameW+g, ty, S(100), S(22))
	Move(a.lStats, irx+nameW+g+S(100), ty, irw-nameW-g-S(100), S(22))
	by := int(hc.Bottom) - pad - bh
	x = irx
	for _, b := range []uintptr{a.bStart, a.bStop, a.bRestart, a.bKill} {
		Move(b, x, by, S(76), bh)
		x += S(76) + g
	}
	Move(a.bBackup, x+S(6), by, S(84), bh)
	Move(a.tAuto, irx+irw-S(92), by, S(92), bh)
	Move(a.bClear, irx+irw-S(92)-g-S(56), by, S(56), bh)
	consoleTop := int(hc.Bottom) + g

	gi, op := S(4), S(3)

	// quick card (bottom)
	qBlocks := a.quickBlocks()
	qh := bh + 2*op
	if len(qBlocks) > 0 {
		qh, _ = flowBlocks(qBlocks, irx, 0, irw, bh, g, gi, op, false)
	}
	qc := RECT{int32(rx), int32(bottom - (capH + qh + pad)), int32(rx + rw), int32(bottom)}
	a.cards = append(a.cards, cardDef{qc, func() string { return "QUICK COMMANDS" }})
	Move(a.bQuickEdit, int(qc.Right)-pad-S(56), int(qc.Top)+S(3), S(56), S(18))
	if len(qBlocks) > 0 {
		_, outs := flowBlocks(qBlocks, irx, int(qc.Top)+capH, irw, bh, g, gi, op, true)
		a.groups = append(a.groups, outs...)
		Show(a.lQuickEmpty, 0)
	} else {
		Move(a.lQuickEmpty, irx, int(qc.Top)+capH, irw, bh)
		Show(a.lQuickEmpty, SW_SHOW)
	}

	// player actions card
	pBlocks := []block{
		{items: []layoutItem{{a.lPlayer, S(40), false}, {a.cbPlayer, S(160), true}, {a.cbEdition, S(80), true}, {a.bClearInv, S(104), false}}},
		{outlined: true, items: []layoutItem{{a.lMode, S(64), false}, {a.cbMode, S(96), true}, {a.bGamemode, S(44), false}}},
		{outlined: true, items: []layoutItem{{a.lTele, S(58), false}, {a.bTP, S(44), false}, {a.bSummon, S(70), false}}},
		{outlined: true, items: []layoutItem{{a.lEffect, S(42), false}, {a.cbEffect, S(150), true}, {a.lFor, S(22), false}, {a.eEffDur, S(62), false},
			{a.lLvl, S(22), false}, {a.eEffLvl, S(32), false}, {a.bEffApply, S(52), false}, {a.bEffClear, S(50), false}}},
		{outlined: true, items: []layoutItem{{a.lGive, S(32), false}, {a.cbItem, S(190), true}, {a.lQty, S(14), false}, {a.eGiveQty, S(40), false}, {a.bGive, S(48), false}}},
	}
	ph, _ := flowBlocks(pBlocks, irx, 0, irw, bh, g, gi, op, false)
	pac := RECT{int32(rx), qc.Top - int32(g) - int32(capH+ph+pad), int32(rx + rw), qc.Top - int32(g)}
	a.cards = append(a.cards, cardDef{pac, func() string { return "PLAYER ACTIONS" }})
	_, outs := flowBlocks(pBlocks, irx, int(pac.Top)+capH, irw, bh, g, gi, op, true)
	a.groups = append(a.groups, outs...)

	// feedback + command row
	fy := int(pac.Top) - g/2 - S(20)
	Move(a.lFeedback, rx+S(2), fy, rw-S(2), S(20))
	cy := fy - S(2) - S(28)
	Move(a.eCmd, rx, cy, rw-S(76)-g, S(28))
	Move(a.bSend, rx+rw-S(76), cy, S(76), S(28))
	Move(a.console, rx, consoleTop, rw, cy-g-consoleTop)

	pInvalidateRect.Call(a.w.Hwnd, 0, 0)
}

// quickBlocks groups quick buttons by their Group (in first-seen order).
func (a *App) quickBlocks() []block {
	var out []block
	idx := map[string]int{}
	li := 0
	for i, q := range a.cfg.QuickCommands {
		if i >= len(a.quickBtns) {
			break
		}
		btn := layoutItem{a.quickBtns[i], S(64), false}
		if q.Group == "" {
			btn.w = S(104)
			out = append(out, block{items: []layoutItem{btn}})
			continue
		}
		if j, ok := idx[q.Group]; ok {
			out[j].items = append(out[j].items, btn)
			continue
		}
		if li >= len(a.quickLabels) {
			continue
		}
		idx[q.Group] = len(out)
		out = append(out, block{outlined: true, items: []layoutItem{{a.quickLabels[li], S(58), false}, btn}})
		li++
	}
	return out
}

func (a *App) paint(hdc uintptr, cw, ch int) {
	m := S(8)
	topH := S(38)
	ls := S(24)
	if a.logo != 0 {
		drawIcon(hdc, a.logo, m+S(2), (topH-ls)/2, ls)
	}
	tx := int32(m + ls + S(10))
	drawText(hdc, "ZephyCube", RECT{tx, 0, tx + int32(S(84)), int32(topH)}, th.Text, titleFont, DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	sub := "Pelican Console"
	if a.cfg.PanelURL != "" {
		sub += "  ·  " + strings.TrimPrefix(strings.TrimPrefix(a.cfg.PanelURL, "https://"), "http://")
	}
	sx := tx + int32(S(86))
	drawText(hdc, sub, RECT{sx, 0, sx + int32(S(360)), int32(topH)}, th.Muted, uiFont, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	for _, c := range a.cards {
		cap := ""
		if c.caption != nil {
			cap = c.caption()
		}
		card(hdc, c.r, cap)
	}
	for _, g := range a.groups {
		outline(hdc, g)
	}
}

func (a *App) invalidatePlayersCaption() {
	r := a.playersCard
	r.Bottom = r.Top + int32(S(22))
	pInvalidateRect.Call(a.w.Hwnd, uintptr(unsafe.Pointer(&r)), 0)
}

// ---------------- events ----------------

func (a *App) onCommand(id, code int, ctl uintptr) {
	if id >= idQuickBase && id < idQuickBase+len(a.cfg.QuickCommands) {
		if code == 0 {
			a.runQuick(a.cfg.QuickCommands[id-idQuickBase])
		}
		return
	}
	if id == idPlayersList {
		if code == LBN_SELCHANGE || code == LBN_DBLCLK {
			if i := lbSel(a.lbPlayers); i >= 0 && i < len(a.players) {
				SetText(a.cbPlayer, a.players[i])
			}
		}
		return
	}
	switch id {
	case idEdition:
		if code == CBN_SELCHANGE {
			a.setEdition(comboSel(a.cbEdition) == 1)
			if a.cur != nil {
				if a.cfg.Edition == nil {
					a.cfg.Edition = map[string]string{}
				}
				a.cfg.Edition[a.cur.Identifier] = map[bool]string{true: "bedrock", false: "java"}[a.bedrock]
				_ = a.cfg.Save()
			}
		}
		return
	case idItem:
		switch code {
		case CBN_EDITCHANGE:
			a.filterItemCombo()
		case CBN_SELCHANGE:
			if i := comboSel(a.cbItem); i >= 0 && i < len(a.itemView) {
				SetText(a.eGiveQty, fmt.Sprint(a.itemView[i].Stack))
			}
		}
		return
	}
	if code != 0 && ctl != 0 {
		return
	}
	switch id {
	case idClearInv:
		a.runTemplate("Clear inventory of {player}", "clear {player}")
	case idEffApply:
		a.applyEffect()
	case idEffClear:
		if a.bedrock {
			a.runTemplate("Clear effects of {player}", "effect {player} clear")
		} else {
			a.runTemplate("Clear effects of {player}", "effect clear {player}")
		}
	case idGive:
		a.give()
	case idTheme:
		a.setDark(IsOn(a.bTheme), true)
	case idListToggle:
		a.setListVisible(IsOn(a.bList))
	case idShowHidden:
		a.showHidden = !a.showHidden
		a.serversChanged()
	case idSettings:
		a.openSettings()
	case idQuickEdit:
		a.manageQuick()
	case idRefresh:
		a.refreshServers()
	case idHelp:
		a.helpMenu()
	case idUpdate:
		a.installUpdate()
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
	case idPlayersRefresh:
		a.refreshPlayers(false)
	case idGamemode:
		m := modes[max(0, comboSel(a.cbMode))]
		a.runTemplate("Gamemode "+m, "gamemode "+m+" {player}")
	case idTP:
		a.runTemplate("TP → {player}", "tp {me} {player}")
	case idSummon:
		a.runTemplate("Summon {player}", "tp {player} {me}")
	}
}

func (a *App) relayout() {
	r := ClientRect(a.w.Hwnd)
	a.layout(int(r.Right), int(r.Bottom))
}

func (a *App) showShortcuts() {
	MsgBox(a.w.Hwnd, "Shortcuts & placeholders",
		"Double-click / Enter on a server  → connect\r\n"+
			"F5  → refresh server list,  Ctrl+B → show/hide the server list\r\n"+
			"Right-click a server  → hide / unhide, show hidden servers\r\n"+
			"Enter in command box  → send,  ↑/↓  → history,  Ctrl+L → clear console\r\n\r\n"+
			"TP  = teleport you → player   (tp {me} {player})\r\n"+
			"Summon  = bring player → you   (tp {player} {me})\r\n"+
			"Click a name in Online Players to fill the Player box.\r\n\r\n"+
			"Quick command placeholders:\r\n"+
			"  {player}  value of the Player box\r\n"+
			"  {me}  your in-game name (Settings)\r\n"+
			"  {input}  asks you when clicked\r\n"+
			"  {server}  connected server name\r\n"+
			"Multiple lines = multiple commands, sent in order.\r\n\r\n"+
			"Kill only appears if Stop/Restart hasn't finished after 15 s\r\n(or if you press Stop again while it is stopping).", MB_ICONINFORMATION)
}

func (a *App) onNotify(nm *NMHDR) (uintptr, bool) {
	if nm.HwndFrom != a.lv {
		return 0, false
	}
	switch int32(nm.Code) {
	case NM_DBLCLK:
		a.connectSelected()
		return 0, true
	case NM_RCLICK:
		a.serverMenu()
		return 0, true
	case NM_CUSTOMDRAW:
		cd := (*NMLVCUSTOMDRAW)(unsafe.Pointer(nm))
		switch cd.Nmcd.DrawStage {
		case CDDS_PREPAINT:
			return CDRF_NOTIFYITEMDRAW, true
		case CDDS_ITEM | CDDS_PREPAINT:
			return CDRF_NOTIFYSUBITEMDRAW, true
		case CDDS_ITEM | CDDS_SUBITEM | CDDS_PREPAINT:
			row := int(cd.Nmcd.ItemSpec)
			cd.ClrText = th.Text
			if row >= 0 && row < len(a.rows) {
				if a.isHidden(a.rows[row].Identifier) {
					cd.ClrText = th.Muted
					return CDRF_NEWFONT, true
				}
				switch cd.ISubItem {
				case 1:
					switch a.states[a.rows[row].Identifier] {
					case "running":
						cd.ClrText = th.Ok
					case "offline":
						cd.ClrText = th.Danger
					case "starting", "stopping", "installing":
						cd.ClrText = th.Warn
					default:
						cd.ClrText = th.Muted
					}
				case 2:
					cd.ClrText = th.Muted
				}
			}
			return CDRF_NEWFONT, true
		}
	}
	return 0, false
}

// updateButtons enables/hides controls from the connection + server state.
func (a *App) updateButtons() {
	on := a.cur != nil
	st := a.curState
	known := st == "running" || st == "offline" || st == "starting" || st == "stopping"
	Enable(a.bStart, on && (!known || st == "offline"))
	Enable(a.bStop, on && (!known || st != "offline"))
	Enable(a.bRestart, on && (!known || st == "running" || st == "starting"))
	if on && a.killVisible && st != "offline" {
		Show(a.bKill, SW_SHOW)
	} else {
		Show(a.bKill, 0)
	}
	for _, h := range []uintptr{a.bBackup, a.bSend, a.bPlayersRefresh, a.bGamemode, a.bTP, a.bSummon, a.bClearInv, a.bEffApply, a.bEffClear, a.bGive} {
		Enable(h, on)
	}
	for _, h := range a.quickBtns {
		Enable(h, on)
	}
}

// ---------------- config / servers ----------------

func (a *App) applyConfig() {
	a.client = pelican.New(a.cfg.PanelURL, a.cfg.apiKey)
	SetOn(a.bTheme, !a.cfg.LightMode)
	pInvalidateRect.Call(a.w.Hwnd, 0, 0)
	a.refreshServers()
}

func (a *App) refreshServers() {
	if a.client == nil {
		a.feedback("Set the panel URL and API key in Settings.", fbErr)
		return
	}
	c, admin := a.client, a.cfg.AdminAll
	SetText(a.lCount, "Loading servers…")
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
				SetText(a.lCount, "Server list failed")
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
			a.updateCount()
			if a.cur == nil && a.cfg.LastServer != "" {
				for _, s := range a.servers {
					if s.Identifier == a.cfg.LastServer {
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
	if i := lvSelected(a.lv); i >= 0 && i < len(a.rows) {
		sel = a.rows[i].Identifier
	} else if a.cur != nil {
		sel = a.cur.Identifier
	}
	a.rows = a.rows[:0]
	for _, s := range a.servers {
		if a.showHidden || !a.isHidden(s.Identifier) {
			a.rows = append(a.rows, s)
		}
	}
	SendMessage(a.lv, WM_SETREDRAW, 0, 0)
	SendMessage(a.lv, LVM_DELETEALLITEMS, 0, 0)
	for i, s := range a.rows {
		name := s.Name
		if a.cur != nil && a.cur.Identifier == s.Identifier {
			name = "● " + name
		}
		if a.isHidden(s.Identifier) {
			name += "  (hidden)"
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
	for i, s := range a.rows {
		if s.Identifier == id {
			lvSetText(a.lv, i, 1, state)
		}
	}
}

// ---------------- hidden servers / list visibility ----------------

func (a *App) isHidden(id string) bool {
	for _, h := range a.cfg.HiddenServers {
		if h == id {
			return true
		}
	}
	return false
}

func (a *App) hiddenCount() int {
	n := 0
	for _, s := range a.servers {
		if a.isHidden(s.Identifier) {
			n++
		}
	}
	return n
}

func (a *App) setHidden(id string, hide bool) {
	var out []string
	for _, h := range a.cfg.HiddenServers {
		if h != id {
			out = append(out, h)
		}
	}
	if hide {
		out = append(out, id)
	}
	a.cfg.HiddenServers = out
	_ = a.cfg.Save()
	a.serversChanged()
}

func (a *App) serversChanged() {
	if a.hiddenCount() == 0 {
		a.showHidden = false
	}
	a.fillList()
	a.updateCount()
	r := ClientRect(a.w.Hwnd)
	a.layout(int(r.Right), int(r.Bottom))
}

func (a *App) updateCount() {
	t := fmt.Sprintf("%d servers", len(a.servers))
	if n := a.hiddenCount(); n > 0 {
		t += fmt.Sprintf(" · %d hidden", n)
	}
	SetText(a.lCount, t)
}

func (a *App) setListVisible(on bool) {
	a.cfg.ListHidden = !on
	_ = a.cfg.Save()
	SetOn(a.bList, on)
	r := ClientRect(a.w.Hwnd)
	a.layout(int(r.Right), int(r.Bottom))
}

func (a *App) serverMenu() {
	i := lvSelected(a.lv)
	menu, _, _ := pCreatePopupMenu.Call()
	defer pDestroyMenu.Call(menu)
	var srv *pelican.Server
	if i >= 0 && i < len(a.rows) {
		s := a.rows[i]
		srv = &s
		appendMenu(menu, MF_STRING, idCtxConnect, "Connect")
		if a.isHidden(s.Identifier) {
			appendMenu(menu, MF_STRING, idCtxUnhide, "Unhide server")
		} else {
			appendMenu(menu, MF_STRING, idCtxHide, "Hide server")
		}
		appendMenu(menu, MF_SEPARATOR, 0, "")
	}
	n := a.hiddenCount()
	flags := uint32(MF_STRING)
	if a.showHidden {
		flags |= MF_CHECKED
	}
	if n == 0 {
		flags |= MF_GRAYED
	}
	appendMenu(menu, flags, idCtxShowHidden, fmt.Sprintf("Show hidden servers (%d)", n))
	f2 := uint32(MF_STRING)
	if n == 0 {
		f2 |= MF_GRAYED
	}
	appendMenu(menu, f2, idCtxUnhideAll, "Unhide all")
	var pt POINT
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	cmd, _, _ := pTrackPopupMenu.Call(menu, TPM_RETURNCMD|TPM_RIGHTBUTTON, uintptr(pt.X), uintptr(pt.Y), 0, a.w.Hwnd, 0)
	switch int(cmd) {
	case idCtxConnect:
		if srv != nil {
			a.connect(*srv)
		}
	case idCtxHide:
		if srv != nil {
			a.setHidden(srv.Identifier, true)
			a.feedback("Hidden “"+srv.Name+"” — right-click the list → Show hidden servers to bring it back.", fbInfo)
		}
	case idCtxUnhide:
		if srv != nil {
			a.setHidden(srv.Identifier, false)
		}
	case idCtxShowHidden:
		a.showHidden = !a.showHidden
		a.serversChanged()
	case idCtxUnhideAll:
		a.cfg.HiddenServers = nil
		_ = a.cfg.Save()
		a.serversChanged()
	}
}

// ---------------- console connection ----------------

func (a *App) connectSelected() {
	i := lvSelected(a.lv)
	if i < 0 || i >= len(a.rows) {
		a.feedback("Select a server in the list first.", fbErr)
		return
	}
	if a.client == nil {
		return
	}
	a.connect(a.rows[i])
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
	a.killVisible = false
	a.stopGen++
	a.cfg.LastServer = s.Identifier
	_ = a.cfg.Save()
	switch a.cfg.Edition[s.Identifier] {
	case "bedrock":
		a.setEdition(true)
	case "java":
		a.setEdition(false)
	default:
		a.setEdition(pelican.GuessBedrock(s))
	}

	a.clearConsole()
	SetText(a.lName, s.Name)
	SetText(a.lStats, "")
	a.setState(a.curState)
	a.setPlayers(nil)
	SetText(a.lConn, "◌ Connecting to console…")
	a.appendConsole([]pendLine{{gen, fmt.Sprintf("── Connecting to %s (%s) on %s ──", s.Name, s.Identifier, s.Node), lineInfo}})
	a.feedback("", fbNone)
	a.fillList()
	a.updateButtons()

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
					SetText(a.lConn, "● Console connected")
					// sync the online list once the state is known
					time.AfterFunc(1500*time.Millisecond, func() {
						Post(func() {
							if gen == a.gen && a.connected && a.curState == "running" {
								a.refreshPlayers(true)
							}
						})
					})
				} else {
					msg := "○ Console disconnected — retrying"
					if err != nil {
						msg += ": " + err.Error()
						a.appendConsole([]pendLine{{gen, "── " + err.Error() + " — reconnecting… ──", pelican.LineError}})
					}
					SetText(a.lConn, msg)
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
const lineJoin pelican.LineKind = 102

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
		if l.gen != a.gen {
			continue
		}
		if l.kind == pelican.LineConsole && !a.bedrock && a.cur != nil && a.cfg.Edition[a.cur.Identifier] == "" &&
			(strings.Contains(l.text, "Player connected:") || strings.Contains(l.text, "Bedrock Edition") || strings.Contains(l.text, "bedrock_server")) {
			a.setEdition(true)
		}
		if l.kind == pelican.LineConsole {
			if _, _, ok := pelican.ParseJoinLeave(l.text); ok {
				l.kind = lineJoin
			}
		}
		cur = append(cur, l)
	}
	if len(cur) == 0 {
		return
	}
	a.appendConsole(cur)
	for _, l := range cur {
		if (l.kind != pelican.LineConsole && l.kind != lineJoin) || strings.TrimSpace(l.text) == "" {
			continue
		}
		if l.kind == lineJoin {
			if name, joined, ok := pelican.ParseJoinLeave(l.text); ok {
				if joined {
					a.addPlayer(name)
				} else {
					a.removePlayer(name)
				}
			}
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
	case lineJoin:
		return colJoin
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
	auto := IsOn(a.tAuto)
	var sel CHARRANGE
	var scroll POINT
	SendMessage(h, EM_EXGETSEL, 0, uintptr(unsafe.Pointer(&sel)))
	SendMessage(h, EM_GETSCROLLPOS, 0, uintptr(unsafe.Pointer(&scroll)))
	SendMessage(h, WM_SETREDRAW, 0, 0)
	SendMessage(h, EM_HIDESELECTION, 1, 0)

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
	SetText(a.lState, "●  "+strings.ToUpper(st))
	pInvalidateRect.Call(a.lState, 0, 1)
	if a.cur != nil {
		a.setRowState(a.cur.Identifier, st)
	}
	switch st {
	case "offline":
		SetText(a.lStats, "")
		a.killVisible = false
		a.stopGen++
		a.setPlayers(nil)
	case "starting":
		a.setPlayers(nil)
	case "running":
		if a.killVisible { // restart got through
			a.killVisible = false
			a.stopGen++
		}
	}
	a.updateButtons()
}

func (a *App) showStats(st pelican.Stats) {
	mem := pelican.HumanBytes(st.MemoryBytes)
	if st.MemoryLimitBytes > 0 {
		mem += " / " + pelican.HumanBytes(st.MemoryLimitBytes)
	}
	s := fmt.Sprintf("CPU %.1f%%    RAM %s    Disk %s", st.CPUAbsolute, mem, pelican.HumanBytes(st.DiskBytes))
	if st.Uptime > 0 {
		s += "    Up " + pelican.HumanUptime(st.Uptime)
	}
	SetText(a.lStats, s)
}

func (a *App) onWSEvent(ev string, args []string) {
	if ev != "backup completed" {
		return
	}
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

// ---------------- players ----------------

func (a *App) setPlayers(list []string) {
	seen := map[string]bool{}
	var out []string
	for _, p := range list {
		k := strings.ToLower(p)
		if p != "" && !seen[k] {
			seen[k] = true
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i]) < strings.ToLower(out[j]) })
	a.players = out
	lbSet(a.lbPlayers, out)
	text := GetText(a.cbPlayer)
	comboSet(a.cbPlayer, out)
	SetText(a.cbPlayer, text)
	a.invalidatePlayersCaption()
}

func (a *App) addPlayer(n string) {
	for _, p := range a.players {
		if strings.EqualFold(p, n) {
			return
		}
	}
	a.setPlayers(append(append([]string(nil), a.players...), n))
}

func (a *App) removePlayer(n string) {
	var out []string
	for _, p := range a.players {
		if !strings.EqualFold(p, n) {
			out = append(out, p)
		}
	}
	a.setPlayers(out)
}

func (a *App) refreshPlayers(silent bool) {
	if a.cur == nil {
		return
	}
	if !a.connected {
		if !silent {
			a.feedback("✖ Console not connected — can't read the player list.", fbErr)
		}
		return
	}
	a.plParser = &pelican.PlayerListParser{}
	a.plSilent = silent
	a.plGen++
	g := a.plGen
	if !silent {
		a.appendConsole([]pendLine{{a.gen, "> list", lineEcho}})
		a.feedback("… fetching online players", fbBusy)
	}
	c, id := a.client, a.cur.Identifier
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err := c.SendCommand(ctx, id, "list")
		cancel()
		Post(func() {
			if err != nil && g == a.plGen {
				a.plParser = nil
				if !silent {
					a.feedback("✖ list: "+err.Error(), fbErr)
				}
			}
		})
		time.Sleep(4 * time.Second)
		Post(func() {
			if g == a.plGen && a.plParser != nil {
				a.plParser = nil
				if !silent {
					a.feedback("✖ Couldn't read a player list from the console reply.", fbErr)
				}
			}
		})
	}()
}

func (a *App) playersDone() {
	p := a.plParser
	a.plParser = nil
	a.setPlayers(p.Players)
	if a.plSilent {
		return
	}
	if len(p.Players) == 0 {
		a.feedback("✔ No players online", fbInfo)
	} else {
		a.feedback(fmt.Sprintf("✔ %d online: %s", len(p.Players), strings.Join(p.Players, ", ")), fbOK)
	}
}

// ---------------- commands ----------------

func (a *App) sendFromInput() {
	cmd := strings.TrimPrefix(strings.TrimSpace(GetText(a.eCmd)), "/")
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

// runTemplate expands placeholders (in the label too) and sends each line as a command.
func (a *App) runTemplate(label, tmpl string) {
	if a.cur == nil {
		a.feedback("Connect to a server first.", fbErr)
		return
	}
	text := tmpl
	if strings.Contains(text, "{player}") {
		p := strings.TrimSpace(GetText(a.cbPlayer))
		if p == "" {
			a.feedback("✖ Pick a player first (Player box or Online Players list).", fbErr)
			Focus(a.cbPlayer)
			return
		}
		text = strings.ReplaceAll(text, "{player}", quoteName(p))
		label = strings.ReplaceAll(label, "{player}", p)
	}
	if strings.Contains(text, "{me}") {
		if strings.TrimSpace(a.cfg.IGN) == "" {
			a.feedback("✖ Set your in-game name in Settings first.", fbErr)
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
		if ln = strings.TrimPrefix(strings.TrimSpace(ln), "/"); ln != "" {
			cmds = append(cmds, ln)
		}
	}
	if len(cmds) > 0 {
		a.sendCommands(cmds, label)
	}
}

// quoteName wraps names containing spaces (Bedrock gamertags) in quotes.
func quoteName(n string) string {
	n = strings.TrimSpace(n)
	if strings.Contains(n, " ") && !strings.HasPrefix(n, "\"") {
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
			a.feedback("✖ "+label+": "+short(pelican.CleanPrefix(l)), fbErr)
			return
		}
	}
	if len(a.capLines) > 0 {
		a.feedback("✔ "+label+": "+short(pelican.CleanPrefix(a.capLines[0])), fbOK)
		return
	}
	a.feedback("✔ Sent: "+label+"  (no console reply)", fbInfo)
}

func short(s string) string {
	if len(s) > 220 {
		return s[:220] + "…"
	}
	return s
}

// ---------------- power / backup ----------------

func (a *App) power(sig string) {
	if a.cur == nil {
		return
	}
	if sig == "stop" && a.curState == "stopping" {
		a.killVisible = true
		a.updateButtons()
		a.feedback("Server is still stopping — use Kill to force it.", fbWarn)
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
	c, id, name, gen := a.client, a.cur.Identifier, a.cur.Name, a.gen
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
			switch sig {
			case "stop", "restart":
				a.stopGen++
				sg := a.stopGen
				time.AfterFunc(15*time.Second, func() {
					Post(func() {
						if gen != a.gen || sg != a.stopGen {
							return
						}
						stuck := a.curState == "stopping" || (sig == "stop" && a.curState == "running")
						if stuck {
							a.killVisible = true
							a.updateButtons()
							a.feedback("⚠ "+name+" hasn't stopped after 15 s — Kill is now available.", fbWarn)
						}
					})
				})
			case "start", "kill":
				a.stopGen++
				a.killVisible = false
				a.updateButtons()
			}
		})
	}()
}

func (a *App) backup() {
	if a.cur == nil {
		return
	}
	srv := *a.cur
	gen := a.gen
	a.feedback("… checking existing backups", fbBusy)
	c := a.client
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		list, err := c.ListBackups(ctx, srv.Identifier)
		cancel()
		Post(func() {
			if gen != a.gen {
				return
			}
			if err != nil {
				a.feedback("✖ Backups: "+err.Error(), fbErr)
				return
			}
			limit := srv.FeatureLimits.Backups
			var victim *pelican.Backup
			note := ""
			if limit > 0 {
				note = fmt.Sprintf("\n%d of %d backup slots used.", len(list), limit)
				if len(list) >= limit {
					victim = pelican.OldestRemovable(list)
					if victim == nil {
						a.feedback(fmt.Sprintf("✖ Backup limit (%d) reached and every backup is locked or still running.", limit), fbErr)
						return
					}
					note = fmt.Sprintf("\nLimit %d reached — the oldest backup “%s” (%s) will be deleted first.", limit, victim.Name, shortDate(victim.CreatedAt))
				}
			}
			def := "Console backup " + time.Now().Format("2006-01-02 15:04")
			name, ok := promptText(a.w.Hwnd, "Create backup", "Backup name for "+srv.Name+" (optional):"+note, def)
			if !ok {
				a.feedback("", fbNone)
				return
			}
			a.feedback("… creating backup", fbBusy)
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()
				if victim != nil {
					if err := c.DeleteBackup(ctx, srv.Identifier, victim.UUID); err != nil {
						Post(func() { a.feedback("✖ Couldn't delete oldest backup: "+err.Error(), fbErr) })
						return
					}
				}
				b, err := c.CreateBackup(ctx, srv.Identifier, name)
				Post(func() {
					if err != nil {
						a.feedback("✖ Backup: "+err.Error(), fbErr)
						return
					}
					n := b.Name
					if n == "" {
						n = name
					}
					msg := "Backup started: " + n
					if victim != nil {
						msg += "  (deleted oldest: " + victim.Name + ")"
					}
					a.feedback("… "+msg+" — waiting for Wings", fbBusy)
					if gen == a.gen {
						a.appendConsole([]pendLine{{a.gen, "── " + msg + " ──", lineInfo}})
					}
				})
			}()
		})
	}()
}

func shortDate(s string) string {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Local().Format("Jan 2 15:04")
	}
	if len(s) >= 16 {
		return s[:16]
	}
	return s
}

// ---------------- edition-specific player actions ----------------

func (a *App) setEdition(bedrock bool) {
	a.bedrock = bedrock
	if bedrock {
		SendMessage(a.cbEdition, CB_SETCURSEL, 1, 0)
	} else {
		SendMessage(a.cbEdition, CB_SETCURSEL, 0, 0)
	}
	// effects
	prev := ""
	if i := comboSel(a.cbEffect); i >= 0 && i < len(a.effView) {
		prev = a.effView[i].Name
	}
	a.effView = effectList(bedrock)
	names := make([]string, len(a.effView))
	sel := 0
	for i, e := range a.effView {
		names[i] = e.Name
		if e.Name == prev || (prev == "" && e.Name == "Speed") {
			sel = i
		}
	}
	comboSet(a.cbEffect, names)
	SendMessage(a.cbEffect, CB_SETCURSEL, uintptr(sel), 0)
	a.filterItemCombo()
}

func (a *App) filterItemCombo() {
	if a.itemFilterBusy {
		return
	}
	a.itemFilterBusy = true
	defer func() { a.itemFilterBusy = false }()
	text := GetText(a.cbItem)
	a.itemView = filterItems(itemList(a.bedrock), text, 400)
	ids := make([]string, len(a.itemView))
	for i, it := range a.itemView {
		ids[i] = it.ID
	}
	comboSet(a.cbItem, ids)
	SetText(a.cbItem, text)
	n := uintptr(len(utf16.Encode([]rune(text))))
	SendMessage(a.cbItem, CB_SETEDITSEL, 0, n|n<<16)
	if it := findItem(itemList(a.bedrock), text); it != nil {
		SetText(a.eGiveQty, fmt.Sprint(it.Stack))
	}
}

func (a *App) applyEffect() {
	i := comboSel(a.cbEffect)
	if i < 0 || i >= len(a.effView) {
		a.feedback("✖ Pick an effect.", fbErr)
		return
	}
	e := a.effView[i]
	dur := strings.ToLower(strings.TrimSpace(GetText(a.eEffDur)))
	if dur == "" || dur == "inf" || dur == "∞" {
		dur = "infinite"
	}
	if dur != "infinite" {
		if n, err := strconv.Atoi(dur); err != nil || n < 1 {
			a.feedback("✖ Duration must be seconds or “infinite”.", fbErr)
			return
		}
	}
	lvl := 1
	if t := strings.TrimSpace(GetText(a.eEffLvl)); t != "" {
		var err error
		if lvl, err = strconv.Atoi(t); err != nil || lvl < 1 || lvl > 256 {
			a.feedback("✖ Level must be 1–256.", fbErr)
			return
		}
	}
	amp := lvl - 1
	label := fmt.Sprintf("%s %d → {player}", e.Name, lvl)
	if a.bedrock {
		a.runTemplate(label, fmt.Sprintf("effect {player} %s %s %d", e.Bedrock, dur, amp))
	} else {
		a.runTemplate(label, fmt.Sprintf("effect give {player} minecraft:%s %s %d", e.Java, dur, amp))
	}
}

func (a *App) give() {
	id := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(GetText(a.cbItem)), "minecraft:")))
	if i := comboSel(a.cbItem); id == "" && i >= 0 && i < len(a.itemView) {
		id = a.itemView[i].ID
	}
	if id == "" || strings.ContainsAny(id, " \t") {
		a.feedback("✖ Pick an item to give.", fbErr)
		return
	}
	qty, err := strconv.Atoi(strings.TrimSpace(GetText(a.eGiveQty)))
	if err != nil || qty < 1 {
		a.feedback("✖ Amount must be a positive number.", fbErr)
		return
	}
	if findItem(itemList(a.bedrock), id) == nil {
		a.feedback("⚠ “"+id+"” isn't in the item list — sending anyway.", fbWarn)
	}
	label := fmt.Sprintf("Give %d %s → {player}", qty, id)
	if a.bedrock {
		a.runTemplate(label, fmt.Sprintf("give {player} %s %d", id, qty))
	} else {
		a.runTemplate(label, fmt.Sprintf("give {player} minecraft:%s %d", id, qty))
	}
}

// ---------------- quick buttons ----------------

func (a *App) rebuildQuick() {
	for _, h := range append(a.quickBtns, a.quickLabels...) {
		Destroy(h)
	}
	a.quickBtns, a.quickLabels = nil, nil
	seen := map[string]bool{}
	for i, q := range a.cfg.QuickCommands {
		h := a.w.Btn(q.Name, idQuickBase+i, kSegment)
		onSurface[h] = true
		a.quickBtns = append(a.quickBtns, h)
		if q.Group != "" && !seen[q.Group] {
			seen[q.Group] = true
			l := a.w.Label(q.Group, 0)
			Muted(l)
			onSurface[l] = true
			a.quickLabels = append(a.quickLabels, l)
		}
	}
	a.w.ApplyTheme()
	a.updateButtons()
	r := ClientRect(a.w.Hwnd)
	a.layout(int(r.Right), int(r.Bottom))
}

func appendMenu(menu uintptr, flags uint32, id uintptr, text string) {
	t := u16(text)
	pAppendMenuW.Call(menu, uintptr(flags), id, uintptr(unsafe.Pointer(t)))
}
