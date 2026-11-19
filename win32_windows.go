//go:build windows

package main

// Thin raw-Win32 layer (no cgo, no third-party deps).

import (
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	comctl32 = syscall.NewLazyDLL("comctl32.dll")
	uxtheme  = syscall.NewLazyDLL("uxtheme.dll")

	pRegisterClassExW        = user32.NewProc("RegisterClassExW")
	pCreateWindowExW         = user32.NewProc("CreateWindowExW")
	pDefWindowProcW          = user32.NewProc("DefWindowProcW")
	pDestroyWindow           = user32.NewProc("DestroyWindow")
	pShowWindow              = user32.NewProc("ShowWindow")
	pUpdateWindow            = user32.NewProc("UpdateWindow")
	pGetMessageW             = user32.NewProc("GetMessageW")
	pTranslateMessage        = user32.NewProc("TranslateMessage")
	pDispatchMessageW        = user32.NewProc("DispatchMessageW")
	pIsDialogMessageW        = user32.NewProc("IsDialogMessageW")
	pPostMessageW            = user32.NewProc("PostMessageW")
	pSendMessageW            = user32.NewProc("SendMessageW")
	pPostQuitMessage         = user32.NewProc("PostQuitMessage")
	pMoveWindow              = user32.NewProc("MoveWindow")
	pSetWindowTextW          = user32.NewProc("SetWindowTextW")
	pGetWindowTextW          = user32.NewProc("GetWindowTextW")
	pGetWindowTextLengthW    = user32.NewProc("GetWindowTextLengthW")
	pEnableWindow            = user32.NewProc("EnableWindow")
	pGetClientRect           = user32.NewProc("GetClientRect")
	pGetWindowRect           = user32.NewProc("GetWindowRect")
	pLoadCursorW             = user32.NewProc("LoadCursorW")
	pMessageBoxW             = user32.NewProc("MessageBoxW")
	pCreateMenu              = user32.NewProc("CreateMenu")
	pCreatePopupMenu         = user32.NewProc("CreatePopupMenu")
	pAppendMenuW             = user32.NewProc("AppendMenuW")
	pSetMenu                 = user32.NewProc("SetMenu")
	pSetFocus                = user32.NewProc("SetFocus")
	pGetFocus                = user32.NewProc("GetFocus")
	pSetForegroundWindow     = user32.NewProc("SetForegroundWindow")
	pGetSysColorBrush        = user32.NewProc("GetSysColorBrush")
	pGetSysColor             = user32.NewProc("GetSysColor")
	pGetDC                   = user32.NewProc("GetDC")
	pReleaseDC               = user32.NewProc("ReleaseDC")
	pSetWindowPos            = user32.NewProc("SetWindowPos")
	pInvalidateRect          = user32.NewProc("InvalidateRect")
	pSetProcessDPIAware      = user32.NewProc("SetProcessDPIAware")
	pGetParent               = user32.NewProc("GetParent")
	pIsChild                 = user32.NewProc("IsChild")
	pGetSystemMetrics        = user32.NewProc("GetSystemMetrics")
	pCreateIconFromResourceEx = user32.NewProc("CreateIconFromResourceEx")
	pGetKeyState             = user32.NewProc("GetKeyState")
	pTrackPopupMenu          = user32.NewProc("TrackPopupMenu")
	pDestroyMenu             = user32.NewProc("DestroyMenu")
	pGetCursorPos            = user32.NewProc("GetCursorPos")

	pCreateFontW   = gdi32.NewProc("CreateFontW")
	pSetTextColor  = gdi32.NewProc("SetTextColor")
	pSetBkMode     = gdi32.NewProc("SetBkMode")
	pSetBkColor    = gdi32.NewProc("SetBkColor")
	pGetDeviceCaps = gdi32.NewProc("GetDeviceCaps")
	pCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")

	pGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	pCreateActCtxW    = kernel32.NewProc("CreateActCtxW")
	pActivateActCtx   = kernel32.NewProc("ActivateActCtx")

	pInitCommonControlsEx = comctl32.NewProc("InitCommonControlsEx")
	pSetWindowTheme       = uxtheme.NewProc("SetWindowTheme")
)

const (
	WS_OVERLAPPED       = 0x00000000
	WS_OVERLAPPEDWINDOW = 0x00CF0000
	WS_CAPTION          = 0x00C00000
	WS_SYSMENU          = 0x00080000
	WS_CHILD            = 0x40000000
	WS_VISIBLE          = 0x10000000
	WS_TABSTOP          = 0x00010000
	WS_GROUP            = 0x00020000
	WS_VSCROLL          = 0x00200000
	WS_HSCROLL          = 0x00100000
	WS_BORDER           = 0x00800000
	WS_CLIPCHILDREN     = 0x02000000
	WS_CLIPSIBLINGS     = 0x04000000

	WS_EX_CLIENTEDGE     = 0x00000200
	WS_EX_CONTROLPARENT  = 0x00010000
	WS_EX_DLGMODALFRAME  = 0x00000001

	WM_CREATE         = 0x0001
	WM_DESTROY        = 0x0002
	WM_SIZE           = 0x0005
	WM_SETREDRAW      = 0x000B
	WM_CLOSE          = 0x0010
	WM_GETMINMAXINFO  = 0x0024
	WM_SETFONT        = 0x0030
	WM_NOTIFY         = 0x004E
	WM_SETICON        = 0x0080
	WM_KEYDOWN        = 0x0100
	WM_COMMAND        = 0x0111
	WM_VSCROLL        = 0x0115
	WM_CTLCOLORSTATIC = 0x0138
	WM_USER           = 0x0400
	WM_APP            = 0x8000

	SW_SHOW = 5

	BS_PUSHBUTTON    = 0x0
	BS_DEFPUSHBUTTON = 0x1
	BS_AUTOCHECKBOX  = 0x3
	BM_GETCHECK      = 0x00F0
	BM_SETCHECK      = 0x00F1

	SS_LEFT        = 0x0
	SS_RIGHT       = 0x2
	SS_NOPREFIX    = 0x80
	SS_CENTERIMAGE = 0x200
	SS_ENDELLIPSIS = 0x4000
	SS_ETCHEDHORZ  = 0x10

	ES_MULTILINE   = 0x4
	ES_PASSWORD    = 0x20
	ES_AUTOVSCROLL = 0x40
	ES_AUTOHSCROLL = 0x80
	ES_READONLY    = 0x800
	ES_WANTRETURN  = 0x1000

	EM_SETSEL       = 0x00B1
	EM_GETLINECOUNT = 0x00BA
	EM_LINEINDEX    = 0x00BB
	EM_REPLACESEL   = 0x00C2
	EM_SETLIMITTEXT = 0x00C5
	EM_SETCUEBANNER = 0x1501

	EM_EXGETSEL        = WM_USER + 52
	EM_EXSETSEL        = WM_USER + 55
	EM_HIDESELECTION   = WM_USER + 63
	EM_SETBKGNDCOLOR   = WM_USER + 67
	EM_SETCHARFORMAT   = WM_USER + 68
	EM_EXLIMITTEXT     = WM_USER + 53
	EM_GETTEXTLENGTHEX = WM_USER + 95
	EM_GETSCROLLPOS    = WM_USER + 221
	EM_SETSCROLLPOS    = WM_USER + 222

	SCF_SELECTION = 0x1
	SCF_ALL       = 0x4
	CFM_BOLD      = 0x1
	CFM_COLOR     = 0x40000000
	CFM_FACE      = 0x20000000
	CFM_SIZE      = 0x80000000

	CBS_DROPDOWN     = 0x2
	CBS_DROPDOWNLIST = 0x3
	CBS_AUTOHSCROLL  = 0x40
	CB_ADDSTRING     = 0x0143
	CB_GETCURSEL     = 0x0147
	CB_RESETCONTENT  = 0x014B
	CB_SETCURSEL     = 0x014E
	CBN_SELCHANGE    = 1

	LBS_NOTIFY           = 0x1
	LBS_NOINTEGRALHEIGHT = 0x100
	LB_ADDSTRING         = 0x0180
	LB_INSERTSTRING      = 0x0181
	LB_DELETESTRING      = 0x0182
	LB_RESETCONTENT      = 0x0184
	LB_SETCURSEL         = 0x0186
	LB_GETCURSEL         = 0x0188
	LBN_SELCHANGE        = 1

	LVM_FIRST                   = 0x1000
	LVM_GETITEMCOUNT            = LVM_FIRST + 4
	LVM_DELETEALLITEMS          = LVM_FIRST + 9
	LVM_GETNEXTITEM             = LVM_FIRST + 12
	LVM_ENSUREVISIBLE           = LVM_FIRST + 19
	LVM_SETCOLUMNWIDTH          = LVM_FIRST + 30
	LVM_SETITEMSTATE            = LVM_FIRST + 43
	LVM_SETEXTENDEDLISTVIEWSTYLE = LVM_FIRST + 54
	LVM_INSERTITEMW             = LVM_FIRST + 77
	LVM_INSERTCOLUMNW           = LVM_FIRST + 97
	LVM_SETITEMTEXTW            = LVM_FIRST + 116
	LVS_REPORT                  = 0x1
	LVS_SINGLESEL               = 0x4
	LVS_SHOWSELALWAYS           = 0x8
	LVS_NOSORTHEADER            = 0x8000
	LVS_EX_FULLROWSELECT        = 0x20
	LVS_EX_DOUBLEBUFFER         = 0x10000
	LVCF_FMT                    = 0x1
	LVCF_WIDTH                  = 0x2
	LVCF_TEXT                   = 0x4
	LVCF_SUBITEM                = 0x8
	LVIF_TEXT                   = 0x1
	LVIF_STATE                  = 0x8
	LVIS_FOCUSED                = 0x1
	LVIS_SELECTED               = 0x2
	LVNI_SELECTED               = 0x2

	NM_DBLCLK = -3
	NM_RETURN = -4

	SB_SETTEXTW = WM_USER + 11
	SB_SETPARTS = WM_USER + 4
	SBARS_SIZEGRIP = 0x100

	MF_STRING    = 0x0
	MF_POPUP     = 0x10
	MF_SEPARATOR = 0x800
	MF_CHECKED   = 0x8
	MF_GRAYED    = 0x1
	TPM_RETURNCMD   = 0x100
	TPM_RIGHTBUTTON = 0x2
	NM_RCLICK       = -5

	MB_OK              = 0x0
	MB_YESNO           = 0x4
	MB_ICONERROR       = 0x10
	MB_ICONQUESTION    = 0x20
	MB_ICONWARNING     = 0x30
	MB_ICONINFORMATION = 0x40
	IDOK               = 1
	IDCANCEL           = 2
	IDYES              = 6

	VK_RETURN = 0x0D
	VK_UP     = 0x26
	VK_DOWN   = 0x28
	VK_F5     = 0x74
	VK_CONTROL = 0x11
	VK_L      = 0x4C

	COLOR_BTNFACE = 15
	COLOR_WINDOW  = 5
	TRANSPARENT   = 1
	SB_BOTTOM     = 7
	LOGPIXELSY    = 90
	FW_NORMAL     = 400
	FW_BOLD       = 700

	SWP_NOZORDER   = 0x4
	SWP_NOACTIVATE = 0x10
)

type POINT struct{ X, Y int32 }
type RECT struct{ Left, Top, Right, Bottom int32 }
type MSG struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      POINT
	Private uint32
}
type WNDCLASSEXW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}
type MINMAXINFO struct {
	Reserved, MaxSize, MaxPosition, MinTrackSize, MaxTrackSize POINT
}
type NMHDR struct {
	HwndFrom uintptr
	IdFrom   uintptr
	Code     uint32
}
type LVCOLUMNW struct {
	Mask       uint32
	Fmt        int32
	Cx         int32
	PszText    *uint16
	CchTextMax int32
	ISubItem   int32
	IImage     int32
	IOrder     int32
	CxMin      int32
	CxDefault  int32
	CxIdeal    int32
}
type LVITEMW struct {
	Mask       uint32
	IItem      int32
	ISubItem   int32
	State      uint32
	StateMask  uint32
	PszText    *uint16
	CchTextMax int32
	IImage     int32
	LParam     uintptr
	IIndent    int32
	IGroupId   int32
	CColumns   uint32
	PuColumns  uintptr
	PiColFmt   uintptr
	IGroup     int32
}
type CHARFORMATW struct {
	CbSize          uint32
	DwMask          uint32
	DwEffects       uint32
	YHeight         int32
	YOffset         int32
	CrTextColor     uint32
	BCharSet        byte
	BPitchAndFamily byte
	SzFaceName      [32]uint16
}
type CHARRANGE struct{ CpMin, CpMax int32 }
type GETTEXTLENGTHEX struct{ Flags, Codepage uint32 }
type INITCOMMONCONTROLSEX struct{ DwSize, DwICC uint32 }
type ACTCTXW struct {
	CbSize                 uint32
	DwFlags                uint32
	LpSource               *uint16
	WProcessorArchitecture uint16
	WLangId                uint16
	LpAssemblyDirectory    *uint16
	LpResourceName         *uint16
	LpApplicationName      *uint16
	HModule                uintptr
}

func RGB(r, g, b byte) uint32 { return uint32(r) | uint32(g)<<8 | uint32(b)<<16 }

func u16(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		p, _ = syscall.UTF16PtrFromString("?")
	}
	return p
}
func up(p *uint16) uintptr { return uintptr(unsafe.Pointer(p)) }

func SendMessage(h uintptr, msg uint32, wp, lp uintptr) uintptr {
	r, _, _ := pSendMessageW.Call(h, uintptr(msg), wp, lp)
	return r
}
func PostMessage(h uintptr, msg uint32, wp, lp uintptr) {
	pPostMessageW.Call(h, uintptr(msg), wp, lp)
}
func SendMessageStr(h uintptr, msg uint32, wp uintptr, s string) uintptr {
	p := u16(s)
	r, _, _ := pSendMessageW.Call(h, uintptr(msg), wp, uintptr(unsafe.Pointer(p)))
	return r
}

func SetText(h uintptr, s string) {
	p := u16(s)
	pSetWindowTextW.Call(h, uintptr(unsafe.Pointer(p)))
}
func GetText(h uintptr) string {
	n, _, _ := pGetWindowTextLengthW.Call(h)
	buf := make([]uint16, n+1)
	pGetWindowTextW.Call(h, uintptr(unsafe.Pointer(&buf[0])), n+1)
	return syscall.UTF16ToString(buf)
}
func Move(h uintptr, x, y, w, hh int) {
	if w < 0 {
		w = 0
	}
	if hh < 0 {
		hh = 0
	}
	pMoveWindow.Call(h, uintptr(x), uintptr(y), uintptr(w), uintptr(hh), 1)
}
func Enable(h uintptr, on bool) {
	v := uintptr(0)
	if on {
		v = 1
	}
	pEnableWindow.Call(h, v)
}
func Show(h uintptr, cmd int) { pShowWindow.Call(h, uintptr(cmd)) }
func Destroy(h uintptr)       { pDestroyWindow.Call(h) }
func Focus(h uintptr)         { pSetFocus.Call(h) }
func GetFocusHwnd() uintptr   { r, _, _ := pGetFocus.Call(); return r }
func ClientRect(h uintptr) RECT {
	var r RECT
	pGetClientRect.Call(h, uintptr(unsafe.Pointer(&r)))
	return r
}
func WindowRect(h uintptr) RECT {
	var r RECT
	pGetWindowRect.Call(h, uintptr(unsafe.Pointer(&r)))
	return r
}
func IsChecked(h uintptr) bool { return SendMessage(h, BM_GETCHECK, 0, 0) == 1 }
func SetChecked(h uintptr, on bool) {
	v := uintptr(0)
	if on {
		v = 1
	}
	SendMessage(h, BM_SETCHECK, v, 0)
}
func SetCue(h uintptr, s string) { SendMessageStr(h, EM_SETCUEBANNER, 1, s) }

func MsgBox(owner uintptr, title, text string, flags uint32) int {
	t, c := u16(text), u16(title)
	r, _, _ := pMessageBoxW.Call(owner, uintptr(unsafe.Pointer(t)), uintptr(unsafe.Pointer(c)), uintptr(flags))
	return int(r)
}

// ---- DPI / fonts ----

var dpi = 96

func initDPI() {
	pSetProcessDPIAware.Call()
	dc, _, _ := pGetDC.Call(0)
	if dc != 0 {
		v, _, _ := pGetDeviceCaps.Call(dc, LOGPIXELSY)
		if v > 0 {
			dpi = int(v)
		}
		pReleaseDC.Call(0, dc)
	}
}
func S(v int) int { return v * dpi / 96 }

func makeFont(face string, pt int, weight int) uintptr {
	h := -(pt * dpi / 72)
	f := u16(face)
	r, _, _ := pCreateFontW.Call(uintptr(int32ToUintptr(int32(h))), 0, 0, 0, uintptr(weight), 0, 0, 0,
		1 /*DEFAULT_CHARSET*/, 0, 0, 5 /*CLEARTYPE*/, 0, uintptr(unsafe.Pointer(f)))
	return r
}
func int32ToUintptr(v int32) uintptr { return uintptr(uint32(v)) }

func SetFont(h, font uintptr) { SendMessage(h, WM_SETFONT, font, 1) }

// ---- ListView helpers ----

func lvAddColumn(lv uintptr, idx int, title string, width int) {
	col := LVCOLUMNW{Mask: LVCF_TEXT | LVCF_WIDTH | LVCF_SUBITEM, Cx: int32(width), PszText: u16(title), ISubItem: int32(idx)}
	SendMessage(lv, LVM_INSERTCOLUMNW, uintptr(idx), uintptr(unsafe.Pointer(&col)))
}
func lvAddRow(lv uintptr, row int, cols ...string) {
	it := LVITEMW{Mask: LVIF_TEXT, IItem: int32(row), PszText: u16(cols[0])}
	SendMessage(lv, LVM_INSERTITEMW, 0, uintptr(unsafe.Pointer(&it)))
	for i := 1; i < len(cols); i++ {
		lvSetText(lv, row, i, cols[i])
	}
}
func lvSetText(lv uintptr, row, col int, s string) {
	it := LVITEMW{ISubItem: int32(col), PszText: u16(s)}
	SendMessage(lv, LVM_SETITEMTEXTW, uintptr(row), uintptr(unsafe.Pointer(&it)))
}
func lvSelected(lv uintptr) int {
	r := SendMessage(lv, LVM_GETNEXTITEM, ^uintptr(0), LVNI_SELECTED)
	return int(int32(r))
}
func lvSelect(lv uintptr, row int) {
	it := LVITEMW{State: LVIS_SELECTED | LVIS_FOCUSED, StateMask: LVIS_SELECTED | LVIS_FOCUSED}
	SendMessage(lv, LVM_SETITEMSTATE, uintptr(row), uintptr(unsafe.Pointer(&it)))
	SendMessage(lv, LVM_ENSUREVISIBLE, uintptr(row), 0)
}

// ---- combo / listbox helpers ----

func comboSet(h uintptr, items []string) {
	SendMessage(h, CB_RESETCONTENT, 0, 0)
	for _, s := range items {
		SendMessageStr(h, CB_ADDSTRING, 0, s)
	}
}
func comboSel(h uintptr) int { return int(int32(SendMessage(h, CB_GETCURSEL, 0, 0))) }
func lbSet(h uintptr, items []string) {
	SendMessage(h, LB_RESETCONTENT, 0, 0)
	for _, s := range items {
		SendMessageStr(h, LB_ADDSTRING, 0, s)
	}
}
func lbSel(h uintptr) int { return int(int32(SendMessage(h, LB_GETCURSEL, 0, 0))) }
