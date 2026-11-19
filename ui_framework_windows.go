//go:build windows

package main

// Window framework + theming (owner-drawn buttons/toggles, dark/light palettes, dark title bars).

import (
	"sync"
	"syscall"
	"unsafe"
)

var (
	dwmapi = syscall.NewLazyDLL("dwmapi.dll")
	ntdll  = syscall.NewLazyDLL("ntdll.dll")

	pBeginPaint      = user32.NewProc("BeginPaint")
	pEndPaint        = user32.NewProc("EndPaint")
	pFillRect        = user32.NewProc("FillRect")
	pDrawTextW       = user32.NewProc("DrawTextW")
	pDrawIconEx      = user32.NewProc("DrawIconEx")
	pLoadImageW      = user32.NewProc("LoadImageW")
	pRedrawWindow    = user32.NewProc("RedrawWindow")
	pRoundRect       = gdi32.NewProc("RoundRect")
	pCreatePen       = gdi32.NewProc("CreatePen")
	pSelectObject    = gdi32.NewProc("SelectObject")
	pDeleteObject    = gdi32.NewProc("DeleteObject")
	pMoveToEx        = gdi32.NewProc("MoveToEx")
	pLineTo          = gdi32.NewProc("LineTo")
	pGetStockObject  = gdi32.NewProc("GetStockObject")
	pDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")
	pRtlGetVersion   = ntdll.NewProc("RtlGetVersion")
	pGetProcAddress  = kernel32.NewProc("GetProcAddress")
)

const (
	WM_PAINT           = 0x000F
	WM_ERASEBKGND      = 0x0014
	WM_DRAWITEM        = 0x002B
	WM_CTLCOLOREDIT    = 0x0133
	WM_CTLCOLORLISTBOX = 0x0134
	WM_CTLCOLORBTN     = 0x0135

	BS_OWNERDRAW = 0xB
	BN_CLICKED   = 0
	LBN_DBLCLK   = 2

	ODS_SELECTED = 0x1
	ODS_DISABLED = 0x4
	ODS_FOCUS    = 0x10
	ODS_NOFOCUSRECT = 0x200

	DT_CENTER       = 0x1
	DT_VCENTER      = 0x4
	DT_SINGLELINE   = 0x20
	DT_NOPREFIX     = 0x800
	DT_END_ELLIPSIS = 0x8000
	DT_LEFT         = 0x0

	NM_CUSTOMDRAW        = -12
	CDDS_PREPAINT        = 0x1
	CDDS_ITEM            = 0x10000
	CDDS_SUBITEM         = 0x20000
	CDRF_DODEFAULT       = 0x0
	CDRF_NEWFONT         = 0x2
	CDRF_NOTIFYITEMDRAW  = 0x20
	CDRF_NOTIFYSUBITEMDRAW = 0x20

	LVM_SETBKCOLOR     = LVM_FIRST + 1
	LVM_SETTEXTCOLOR   = LVM_FIRST + 36
	LVM_SETTEXTBKCOLOR = LVM_FIRST + 38
	LVS_NOCOLUMNHEADER = 0x4000

	NULL_PEN = 8
)

type PAINTSTRUCT struct {
	Hdc         uintptr
	FErase      int32
	RcPaint     RECT
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
}
type DRAWITEMSTRUCT struct {
	CtlType    uint32
	CtlID      uint32
	ItemID     uint32
	ItemAction uint32
	ItemState  uint32
	HwndItem   uintptr
	HDC        uintptr
	RcItem     RECT
	ItemData   uintptr
}
type NMCUSTOMDRAW struct {
	Hdr        NMHDR
	DrawStage  uint32
	Hdc        uintptr
	Rc         RECT
	ItemSpec   uintptr
	ItemState  uint32
	ItemlParam uintptr
}
type NMLVCUSTOMDRAW struct {
	Nmcd      NMCUSTOMDRAW
	ClrText   uint32
	ClrTextBk uint32
	ISubItem  int32
}

// ---------------- theme ----------------

type Theme struct {
	Dark                                  bool
	Bg, Surface, Input, Border            uint32
	Text, Muted                           uint32
	Accent, AccentDim, AccentSoft         uint32
	Danger, DangerFill, Ok, Warn, Pressed uint32
}

var darkTheme = Theme{
	Dark: true,
	Bg: RGB(11, 11, 14), Surface: RGB(21, 21, 26), Input: RGB(30, 30, 37), Border: RGB(46, 46, 56),
	Text: RGB(236, 236, 241), Muted: RGB(140, 140, 152),
	Accent: RGB(200, 20, 90), AccentDim: RGB(120, 12, 55), AccentSoft: RGB(255, 170, 200),
	Danger: RGB(235, 80, 80), DangerFill: RGB(70, 20, 26), Ok: RGB(70, 205, 125), Warn: RGB(240, 190, 80),
	Pressed: RGB(52, 52, 64),
}
var lightTheme = Theme{
	Dark: false,
	Bg: RGB(241, 241, 244), Surface: RGB(255, 255, 255), Input: RGB(248, 248, 250), Border: RGB(212, 212, 220),
	Text: RGB(22, 22, 28), Muted: RGB(105, 105, 118),
	Accent: RGB(200, 20, 90), AccentDim: RGB(150, 14, 66), AccentSoft: RGB(200, 20, 90),
	Danger: RGB(200, 30, 30), DangerFill: RGB(253, 233, 233), Ok: RGB(0, 135, 65), Warn: RGB(185, 115, 0),
	Pressed: RGB(225, 225, 232),
}

var th = darkTheme
var brBg, brSurface, brInput, brBorder uintptr

func brush(c uint32) uintptr { r, _, _ := pCreateSolidBrush.Call(uintptr(c)); return r }
func delObj(h uintptr) {
	if h != 0 {
		pDeleteObject.Call(h)
	}
}

func setTheme(dark bool) {
	if dark {
		th = darkTheme
	} else {
		th = lightTheme
	}
	for _, b := range []uintptr{brBg, brSurface, brInput, brBorder} {
		delObj(b)
	}
	brBg, brSurface, brInput, brBorder = brush(th.Bg), brush(th.Surface), brush(th.Input), brush(th.Border)
	setAppDarkMode(dark)
}

// ---- undocumented-but-stable uxtheme dark mode (Win10 1809+), best effort ----

var winBuild uint32
var pSetPreferredAppMode, pAllowDarkModeForWindow, pFlushMenuThemes uintptr

func initDarkModeAPI() {
	var vi struct {
		Size, Major, Minor, Build, Platform uint32
		CSD                                 [128]uint16
	}
	vi.Size = uint32(unsafe.Sizeof(vi))
	pRtlGetVersion.Call(uintptr(unsafe.Pointer(&vi)))
	winBuild = vi.Build
	if vi.Major < 10 || winBuild < 17763 {
		return
	}
	h, err := syscall.LoadLibrary("uxtheme.dll")
	if err != nil {
		return
	}
	ord := func(n uintptr) uintptr { r, _, _ := pGetProcAddress.Call(uintptr(h), n); return r }
	pSetPreferredAppMode = ord(135)
	pAllowDarkModeForWindow = ord(133)
	pFlushMenuThemes = ord(136)
}

func setAppDarkMode(dark bool) {
	if pSetPreferredAppMode == 0 {
		return
	}
	mode := uintptr(3) // ForceLight
	if dark {
		mode = 2 // ForceDark
	}
	syscall.SyscallN(pSetPreferredAppMode, mode)
	if pFlushMenuThemes != 0 {
		syscall.SyscallN(pFlushMenuThemes)
	}
}

func allowDark(h uintptr, on bool) {
	if pAllowDarkModeForWindow == 0 {
		return
	}
	v := uintptr(0)
	if on {
		v = 1
	}
	syscall.SyscallN(pAllowDarkModeForWindow, h, v)
}

func setDarkTitle(h uintptr, on bool) {
	v := int32(0)
	if on {
		v = 1
	}
	if pDwmSetWindowAttribute.Find() != nil {
		return
	}
	r, _, _ := pDwmSetWindowAttribute.Call(h, 20, uintptr(unsafe.Pointer(&v)), 4)
	if r != 0 {
		pDwmSetWindowAttribute.Call(h, 19, uintptr(unsafe.Pointer(&v)), 4)
	}
}

func setWinTheme(h uintptr, name string) {
	if pSetWindowTheme.Find() != nil {
		return
	}
	if name == "" {
		pSetWindowTheme.Call(h, 0, 0)
		return
	}
	n := u16(name)
	pSetWindowTheme.Call(h, uintptr(unsafe.Pointer(n)), 0)
}

func initCommonControls() {
	icc := INITCOMMONCONTROLSEX{DwICC: 0x1 | 0x4 | 0x4000 | 0xFF}
	icc.DwSize = uint32(unsafe.Sizeof(icc))
	pInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc)))
	syscall.LoadLibrary("Msftedit.dll")
}

// ---------------- control registry ----------------

type role int

const (
	roleStatic role = iota
	roleButton
	roleEdit
	roleMultiEdit
	roleCombo
	roleListBox
	roleListView
	roleRich
)

type btnKind int

const (
	kNormal btnKind = iota
	kPrimary
	kDanger
	kToggle
	kSegment // grouped/segmented buttons
)

type ctlInfo struct {
	h    uintptr
	role role
}

var (
	btnKinds   = map[uintptr]btnKind{}
	toggles    = map[uintptr]bool{}
	onSurface  = map[uintptr]bool{}
	labelColor = map[uintptr]func() uint32{}
	ctlFont    = map[uintptr]uintptr{}
)

func IsOn(h uintptr) bool { return toggles[h] }
func SetOn(h uintptr, on bool) {
	toggles[h] = on
	pInvalidateRect.Call(h, 0, 0)
}
func Muted(h uintptr) { labelColor[h] = func() uint32 { return th.Muted } }

// ---------------- window framework ----------------

type Win struct {
	Hwnd       uintptr
	OnCommand  func(id, code int, ctl uintptr)
	OnNotify   func(nm *NMHDR) (uintptr, bool)
	OnSize     func(w, h int)
	OnClose    func()
	OnDestroy  func()
	OnPaint    func(hdc uintptr, cw, ch int)
	MinW, MinH int
	font       uintptr
	ctls       []ctlInfo
	modalDone  bool
}

var (
	wins       = map[uintptr]*Win{}
	creating   *Win
	hInstance  uintptr
	wndProcCB  = syscall.NewCallback(wndProc)
	classesReg = map[string]bool{}

	invokeMu sync.Mutex
	invokeQ  []func()
	mainHwnd uintptr
)

const wmInvoke = WM_APP + 1

// Post runs fn on the UI thread (safe from any goroutine).
func Post(fn func()) {
	invokeMu.Lock()
	invokeQ = append(invokeQ, fn)
	invokeMu.Unlock()
	PostMessage(mainHwnd, wmInvoke, 0, 0)
}

func runInvokes() {
	invokeMu.Lock()
	q := invokeQ
	invokeQ = nil
	invokeMu.Unlock()
	for _, f := range q {
		f()
	}
}

func ctlBrush(h uintptr) uintptr {
	if onSurface[h] {
		return brSurface
	}
	return brBg
}
func ctlBgColor(h uintptr) uint32 {
	if onSurface[h] {
		return th.Surface
	}
	return th.Bg
}

func wndProc(hwnd, msgp, wp, lp uintptr) uintptr {
	msg := uint32(msgp)
	w := wins[hwnd]
	if w == nil && creating != nil {
		w = creating
		w.Hwnd = hwnd
		wins[hwnd] = w
		creating = nil
	}
	if w != nil {
		switch msg {
		case wmInvoke:
			runInvokes()
			return 0
		case WM_COMMAND:
			if lp != 0 && (wp>>16)&0xFFFF == BN_CLICKED {
				if _, ok := toggles[lp]; ok {
					SetOn(lp, !toggles[lp])
				}
			}
			if w.OnCommand != nil {
				w.OnCommand(int(wp&0xFFFF), int((wp>>16)&0xFFFF), lp)
				return 0
			}
		case WM_NOTIFY:
			if w.OnNotify != nil {
				if r, ok := w.OnNotify((*NMHDR)(unsafe.Pointer(lp))); ok {
					return r
				}
			}
		case WM_SIZE:
			if w.OnSize != nil {
				w.OnSize(int(lp&0xFFFF), int((lp>>16)&0xFFFF))
				return 0
			}
		case WM_GETMINMAXINFO:
			if w.MinW > 0 {
				mmi := (*MINMAXINFO)(unsafe.Pointer(lp))
				mmi.MinTrackSize = POINT{int32(w.MinW), int32(w.MinH)}
				return 0
			}
		case WM_ERASEBKGND:
			if w.OnPaint == nil {
				r := ClientRect(hwnd)
				pFillRect.Call(wp, uintptr(unsafe.Pointer(&r)), brBg)
			}
			return 1
		case WM_PAINT:
			var ps PAINTSTRUCT
			hdc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
			r := ClientRect(hwnd)
			pFillRect.Call(hdc, uintptr(unsafe.Pointer(&r)), brBg)
			if w.OnPaint != nil {
				w.OnPaint(hdc, int(r.Right), int(r.Bottom))
			}
			pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
			return 0
		case WM_CTLCOLORSTATIC:
			c := th.Text
			if f := labelColor[lp]; f != nil {
				c = f()
			}
			pSetTextColor.Call(wp, uintptr(c))
			pSetBkColor.Call(wp, uintptr(ctlBgColor(lp)))
			return ctlBrush(lp)
		case WM_CTLCOLOREDIT, WM_CTLCOLORLISTBOX:
			pSetTextColor.Call(wp, uintptr(th.Text))
			pSetBkColor.Call(wp, uintptr(th.Input))
			return brInput
		case WM_CTLCOLORBTN:
			return ctlBrush(lp)
		case WM_DRAWITEM:
			dis := (*DRAWITEMSTRUCT)(unsafe.Pointer(lp))
			if dis.CtlType == 4 { // ODT_BUTTON
				drawButton(dis)
				return 1
			}
		case WM_CLOSE:
			if w.OnClose != nil {
				w.OnClose()
				return 0
			}
		case WM_DESTROY:
			if w.OnDestroy != nil {
				w.OnDestroy()
			}
			for _, c := range w.ctls {
				delete(btnKinds, c.h)
				delete(toggles, c.h)
				delete(onSurface, c.h)
				delete(labelColor, c.h)
				delete(ctlFont, c.h)
			}
			delete(wins, hwnd)
		}
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, uintptr(msg), wp, lp)
	return r
}

func registerClass(name string, iconBig, iconSmall uintptr) {
	if classesReg[name] {
		return
	}
	cur, _, _ := pLoadCursorW.Call(0, 32512) // IDC_ARROW
	wc := WNDCLASSEXW{
		Style:         0x3, // CS_HREDRAW|CS_VREDRAW
		LpfnWndProc:   wndProcCB,
		HInstance:     hInstance,
		HCursor:       cur,
		LpszClassName: u16(name),
		HIcon:         iconBig,
		HIconSm:       iconSmall,
	}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	classesReg[name] = true
}

func NewTopWindow(w *Win, class, title string, style, exStyle uint32, width, height int, owner uintptr) uintptr {
	creating = w
	t := u16(title)
	c := u16(class)
	x := uintptr(0x80000000) // CW_USEDEFAULT
	y := uintptr(0x80000000)
	h, _, _ := pCreateWindowExW.Call(uintptr(exStyle), uintptr(unsafe.Pointer(c)), uintptr(unsafe.Pointer(t)),
		uintptr(style), x, y, uintptr(width), uintptr(height), owner, 0, hInstance, 0)
	creating = nil
	w.Hwnd = h
	return h
}

func (w *Win) Ctl(class, text string, style, exStyle uint32, id int, r role) uintptr {
	c := u16(class)
	t := u16(text)
	h, _, _ := pCreateWindowExW.Call(uintptr(exStyle), uintptr(unsafe.Pointer(c)), uintptr(unsafe.Pointer(t)),
		uintptr(WS_CHILD|WS_VISIBLE|style), 0, 0, 10, 10, w.Hwnd, uintptr(id), hInstance, 0)
	if w.font != 0 {
		SetFont(h, w.font)
	}
	w.ctls = append(w.ctls, ctlInfo{h, r})
	return h
}

func (w *Win) Btn(text string, id int, k btnKind) uintptr {
	h := w.Ctl("BUTTON", text, WS_TABSTOP|BS_OWNERDRAW, 0, id, roleButton)
	btnKinds[h] = k
	return h
}
func (w *Win) Toggle(text string, id int, on bool) uintptr {
	h := w.Btn(text, id, kToggle)
	toggles[h] = on
	return h
}
func (w *Win) Label(text string, style uint32) uintptr {
	return w.Ctl("STATIC", text, SS_NOPREFIX|SS_CENTERIMAGE|style, 0, 0, roleStatic)
}
func (w *Win) Edit(text string, style uint32, id int) uintptr {
	return w.Ctl("EDIT", text, WS_TABSTOP|ES_AUTOHSCROLL|style, WS_EX_CLIENTEDGE, id, roleEdit)
}
func (w *Win) SetCtlFont(h, f uintptr) {
	SetFont(h, f)
	ctlFont[h] = f
}

// ApplyTheme re-themes a window and all of its controls for the current palette.
func (w *Win) ApplyTheme() {
	setDarkTitle(w.Hwnd, th.Dark)
	allowDark(w.Hwnd, th.Dark)
	for _, c := range w.ctls {
		switch c.role {
		case roleEdit, roleCombo:
			allowDark(c.h, th.Dark)
			if th.Dark {
				setWinTheme(c.h, "DarkMode_CFD")
			} else {
				setWinTheme(c.h, "")
			}
		case roleMultiEdit, roleListBox, roleRich:
			allowDark(c.h, th.Dark)
			if th.Dark {
				setWinTheme(c.h, "DarkMode_Explorer")
			} else {
				setWinTheme(c.h, "")
			}
		case roleListView:
			allowDark(c.h, th.Dark)
			if th.Dark {
				setWinTheme(c.h, "DarkMode_Explorer")
			} else {
				setWinTheme(c.h, "Explorer")
			}
			SendMessage(c.h, LVM_SETBKCOLOR, 0, uintptr(th.Input))
			SendMessage(c.h, LVM_SETTEXTBKCOLOR, 0, uintptr(th.Input))
			SendMessage(c.h, LVM_SETTEXTCOLOR, 0, uintptr(th.Text))
		}
	}
	pRedrawWindow.Call(w.Hwnd, 0, 0, 0x1|0x4|0x80|0x400) // INVALIDATE|ERASE|ALLCHILDREN|FRAME
}

// RunModal shows w as a modal dialog owned by owner and pumps messages until EndModal.
func (w *Win) RunModal(owner uintptr) {
	Enable(owner, false)
	Show(w.Hwnd, SW_SHOW)
	pUpdateWindow.Call(w.Hwnd)
	var m MSG
	for !w.modalDone {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			pPostQuitMessage.Call(0)
			break
		}
		if d, _, _ := pIsDialogMessageW.Call(w.Hwnd, uintptr(unsafe.Pointer(&m))); d != 0 {
			continue
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
	Enable(owner, true)
	pSetForegroundWindow.Call(owner)
	Destroy(w.Hwnd)
}
func (w *Win) EndModal() { w.modalDone = true; PostMessage(w.Hwnd, wmInvoke, 0, 0) }

// ---------------- drawing ----------------

func fillRound(hdc uintptr, r RECT, fill, border uint32, radius int) {
	b := brush(fill)
	p, _, _ := pCreatePen.Call(0, 1, uintptr(border))
	ob, _, _ := pSelectObject.Call(hdc, b)
	op, _, _ := pSelectObject.Call(hdc, p)
	pRoundRect.Call(hdc, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right), uintptr(r.Bottom), uintptr(radius), uintptr(radius))
	pSelectObject.Call(hdc, ob)
	pSelectObject.Call(hdc, op)
	delObj(b)
	delObj(p)
}

func drawText(hdc uintptr, s string, r RECT, color uint32, font uintptr, flags uint32) {
	pSetBkMode.Call(hdc, TRANSPARENT)
	pSetTextColor.Call(hdc, uintptr(color))
	of, _, _ := pSelectObject.Call(hdc, font)
	t := u16(s)
	pDrawTextW.Call(hdc, uintptr(unsafe.Pointer(t)), ^uintptr(0), uintptr(unsafe.Pointer(&r)), uintptr(flags|DT_NOPREFIX))
	pSelectObject.Call(hdc, of)
}

func line(hdc uintptr, x1, y1, x2, y2 int, color uint32, width int) {
	p, _, _ := pCreatePen.Call(0, uintptr(width), uintptr(color))
	op, _, _ := pSelectObject.Call(hdc, p)
	pMoveToEx.Call(hdc, uintptr(x1), uintptr(y1), 0)
	pLineTo.Call(hdc, uintptr(x2), uintptr(y2))
	pSelectObject.Call(hdc, op)
	delObj(p)
}

func drawButton(d *DRAWITEMSTRUCT) {
	h, hdc, rc := d.HwndItem, d.HDC, d.RcItem
	pFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), ctlBrush(h))
	kind := btnKinds[h]
	disabled := d.ItemState&ODS_DISABLED != 0
	pressed := d.ItemState&ODS_SELECTED != 0
	focus := d.ItemState&ODS_FOCUS != 0 && d.ItemState&ODS_NOFOCUSRECT == 0
	text := GetText(h)
	font := uiFont
	if f := ctlFont[h]; f != 0 {
		font = f
	}
	rad := S(8)

	if kind == kToggle {
		on := toggles[h]
		box := S(16)
		by := int(rc.Top) + (int(rc.Bottom-rc.Top)-box)/2
		br := RECT{rc.Left + int32(S(2)), int32(by), rc.Left + int32(S(2)+box), int32(by + box)}
		fill, border := th.Input, th.Border
		if on {
			fill, border = th.Accent, th.Accent
		}
		if focus {
			border = th.AccentSoft
		}
		fillRound(hdc, br, fill, border, S(5))
		if on {
			x, y := int(br.Left), int(br.Top)
			line(hdc, x+box*25/100, y+box*52/100, x+box*43/100, y+box*70/100, RGB(255, 255, 255), max(2, S(2)))
			line(hdc, x+box*43/100, y+box*70/100, x+box*76/100, y+box*32/100, RGB(255, 255, 255), max(2, S(2)))
		}
		tc := th.Text
		if disabled {
			tc = th.Muted
		}
		tr := RECT{br.Right + int32(S(8)), rc.Top, rc.Right, rc.Bottom}
		drawText(hdc, text, tr, tc, font, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
		return
	}

	fill, border, tc := th.Input, th.Border, th.Text
	switch kind {
	case kPrimary:
		fill, border, tc = th.Accent, th.Accent, RGB(255, 255, 255)
		if pressed {
			fill, border = th.AccentDim, th.AccentDim
		}
		if f := ctlFont[h]; f == 0 {
			font = boldFont
		}
	case kDanger:
		fill, border, tc = th.DangerFill, th.Danger, th.Danger
		if pressed {
			fill = th.Danger
			tc = RGB(255, 255, 255)
		}
	default:
		if pressed {
			fill = th.Pressed
		}
	}
	if disabled {
		fill, border, tc = ctlBgColor(h), th.Border, th.Muted
		if th.Dark {
			fill = th.Input
			tc = RGB(88, 88, 98)
		}
	}
	if focus && !disabled {
		border = th.AccentSoft
	}
	fillRound(hdc, rc, fill, border, rad)
	tr := rc
	tr.Left += int32(S(6))
	tr.Right -= int32(S(6))
	drawText(hdc, text, tr, tc, font, DT_CENTER|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
}

// card draws a rounded surface panel with an optional caption.
func card(hdc uintptr, r RECT, caption string) {
	fillRound(hdc, r, th.Surface, th.Border, S(14))
	if caption != "" {
		tr := RECT{r.Left + int32(S(14)), r.Top + int32(S(8)), r.Right - int32(S(14)), r.Top + int32(S(30))}
		drawText(hdc, caption, tr, th.Muted, capFont, DT_LEFT|DT_VCENTER|DT_SINGLELINE|DT_END_ELLIPSIS)
	}
}

// outline draws a subtle rounded group frame (inside a card).
func outline(hdc uintptr, r RECT) {
	fillRound(hdc, r, th.Surface, th.Border, S(10))
}

func loadAppIcon(size int) uintptr {
	h, _, _ := pLoadImageW.Call(hInstance, 1, 1 /*IMAGE_ICON*/, uintptr(size), uintptr(size), 0)
	return h
}

func drawIcon(hdc, icon uintptr, x, y, size int) {
	pDrawIconEx.Call(hdc, uintptr(x), uintptr(y), icon, uintptr(size), uintptr(size), 0, 0, 3)
}
