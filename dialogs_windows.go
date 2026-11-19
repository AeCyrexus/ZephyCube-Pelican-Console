//go:build windows

package main

import (
	"context"
	"strings"
	"time"

	"zephycube/pelicanconsole/pelican"
)

const dlgClass = "ZCPelicanDialog"

func newDialog(owner uintptr, title string, cw, ch int) *Win {
	d := &Win{font: uiFont}
	NewTopWindow(d, dlgClass, title, WS_CAPTION|WS_SYSMENU|WS_CLIPCHILDREN, WS_EX_DLGMODALFRAME|WS_EX_CONTROLPARENT, cw, ch, owner)
	setClientSize(d.Hwnd, cw, ch, owner)
	d.OnClose = d.EndModal
	return d
}

// setClientSize resizes a window so its client area is cw x ch and centers it on owner.
func setClientSize(h uintptr, cw, ch int, owner uintptr) {
	wr, cr := WindowRect(h), ClientRect(h)
	fw := int(wr.Right-wr.Left) - int(cr.Right)
	fh := int(wr.Bottom-wr.Top) - int(cr.Bottom)
	w, hh := cw+fw, ch+fh
	x, y := int(wr.Left), int(wr.Top)
	if owner != 0 {
		o := WindowRect(owner)
		x = int(o.Left) + (int(o.Right-o.Left)-w)/2
		y = int(o.Top) + (int(o.Bottom-o.Top)-hh)/3
	}
	pSetWindowPos.Call(h, 0, uintptr(uint32(int32(x))), uintptr(uint32(int32(y))), uintptr(w), uintptr(hh), SWP_NOZORDER|SWP_NOACTIVATE)
}

func runModalFocus(d *Win, owner, focus uintptr) {
	Post(func() {
		if focus != 0 {
			Focus(focus)
		}
	})
	d.RunModal(owner)
}

// ---------- prompt ----------

func promptText(owner uintptr, title, label, def string) (string, bool) {
	cw, ch := S(420), S(112)
	d := newDialog(owner, title, cw, ch)
	lbl := d.Label(label, 0)
	ed := d.Edit(def, 0, 100)
	ok := d.Ctl("BUTTON", "OK", WS_TABSTOP|BS_DEFPUSHBUTTON, 0, IDOK)
	cancel := d.Button("Cancel", IDCANCEL)
	m := S(12)
	Move(lbl, m, S(10), cw-2*m, S(20))
	Move(ed, m, S(34), cw-2*m, S(24))
	Move(ok, cw-m-S(180)-S(8), ch-m-S(28), S(90), S(28))
	Move(cancel, cw-m-S(90), ch-m-S(28), S(90), S(28))
	SendMessage(ed, EM_SETSEL, 0, ^uintptr(0))
	var out string
	accepted := false
	d.OnCommand = func(id, code int, ctl uintptr) {
		if code != 0 {
			return
		}
		switch id {
		case IDOK:
			out, accepted = GetText(ed), true
			d.EndModal()
		case IDCANCEL:
			d.EndModal()
		}
	}
	runModalFocus(d, owner, ed)
	return out, accepted
}

// ---------- settings ----------

func (a *App) openSettings() {
	cw, ch := S(520), S(290)
	d := newDialog(a.w.Hwnd, "Settings — Pelican connection", cw, ch)
	m, lw := S(14), S(120)
	ew := cw - 2*m - lw
	row := func(i int) int { return m + i*S(36) }

	l1 := d.Label("Panel URL:", 0)
	eURL := d.Edit(a.cfg.PanelURL, 0, 101)
	SetCue(eURL, "https://pelican.example.com")
	l2 := d.Label("Client API key:", 0)
	eKey := d.Edit("", ES_PASSWORD, 102)
	if a.cfg.apiKey != "" {
		SetCue(eKey, "(saved — leave blank to keep)")
	} else {
		SetCue(eKey, "Account → API Keys on the panel")
	}
	l3 := d.Label("Your in-game name:", 0)
	eIGN := d.Edit(a.cfg.IGN, 0, 103)
	SetCue(eIGN, "used for {me} / TP / Summon")
	chkAdmin := d.Ctl("BUTTON", "List every server on the panel (admin keys: type=admin-all)", WS_TABSTOP|BS_AUTOCHECKBOX, 0, 104)
	SetChecked(chkAdmin, a.cfg.AdminAll)
	hint := d.Label("Key is stored encrypted (Windows DPAPI, current user only).", 0)
	bTest := d.Button("Test connection", 105)
	bOK := d.Ctl("BUTTON", "Save", WS_TABSTOP|BS_DEFPUSHBUTTON, 0, IDOK)
	bCancel := d.Button("Cancel", IDCANCEL)

	for i, p := range [][2]uintptr{{l1, eURL}, {l2, eKey}, {l3, eIGN}} {
		Move(p[0], m, row(i), lw, S(24))
		Move(p[1], m+lw, row(i), ew, S(24))
	}
	Move(chkAdmin, m+lw, row(3), ew, S(22))
	Move(hint, m+lw, row(3)+S(26), ew, S(20))
	by := ch - m - S(30)
	Move(bTest, m, by, S(130), S(30))
	Move(bCancel, cw-m-S(96), by, S(96), S(30))
	Move(bOK, cw-m-S(96)*2-S(8), by, S(96), S(30))

	key := func() string {
		k := strings.TrimSpace(GetText(eKey))
		if k == "" {
			k = a.cfg.apiKey
		}
		return k
	}
	d.OnCommand = func(id, code int, ctl uintptr) {
		if code != 0 {
			return
		}
		switch id {
		case 105:
			c := pelican.New(GetText(eURL), key())
			SetText(bTest, "Testing…")
			Enable(bTest, false)
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
				defer cancel()
				acc, err := c.Account(ctx)
				Post(func() {
					SetText(bTest, "Test connection")
					Enable(bTest, true)
					if err != nil {
						MsgBox(d.Hwnd, "Connection failed", err.Error(), MB_ICONERROR)
						return
					}
					role := ""
					if acc.RootAdmin || acc.Admin {
						role = " (admin)"
					}
					MsgBox(d.Hwnd, "Connected", "Authenticated as "+acc.Username+role+".", MB_ICONINFORMATION)
				})
			}()
		case IDOK:
			a.cfg.PanelURL = pelican.NormalizeURL(GetText(eURL))
			a.cfg.apiKey = key()
			a.cfg.IGN = strings.TrimSpace(GetText(eIGN))
			a.cfg.AdminAll = IsChecked(chkAdmin)
			if err := a.cfg.Save(); err != nil {
				MsgBox(d.Hwnd, "Save failed", err.Error(), MB_ICONERROR)
				return
			}
			d.EndModal()
			a.applyConfig()
		case IDCANCEL:
			d.EndModal()
		}
	}
	runModalFocus(d, a.w.Hwnd, eURL)
}

// ---------- quick commands manager ----------

func (a *App) manageQuick() {
	cw, ch := S(700), S(430)
	d := newDialog(a.w.Hwnd, "Quick command buttons", cw, ch)
	items := append([]QuickCmd(nil), a.cfg.QuickCommands...)
	cur := -1

	lb := d.Ctl("LISTBOX", "", WS_TABSTOP|WS_VSCROLL|LBS_NOTIFY|LBS_NOINTEGRALHEIGHT, WS_EX_CLIENTEDGE, 300)
	bNew := d.Button("New", 301)
	bDel := d.Button("Delete", 302)
	bUp := d.Button("▲ Up", 303)
	bDown := d.Button("▼ Down", 304)
	lName := d.Label("Button label:", 0)
	eName := d.Edit("", 0, 305)
	lCmd := d.Label("Command(s) — one per line, sent in order:", 0)
	eCmd := d.Ctl("EDIT", "", WS_TABSTOP|WS_VSCROLL|ES_MULTILINE|ES_AUTOVSCROLL|ES_WANTRETURN, WS_EX_CLIENTEDGE, 306)
	SetFont(eCmd, monoFont)
	help := d.Ctl("STATIC", "Placeholders:  {player} = Player box   {me} = your IGN   {input} = ask when clicked   {server} = server name\r\nNo leading slash needed. Example:  tp {player} {me}", SS_NOPREFIX, 0, 0)
	bOK := d.Ctl("BUTTON", "Save", WS_TABSTOP|BS_DEFPUSHBUTTON, 0, IDOK)
	bCancel := d.Button("Cancel", IDCANCEL)
	bReset := d.Button("Restore defaults", 307)

	m := S(12)
	lw := S(210)
	Move(lb, m, m, lw, ch-2*m-S(76))
	by := ch - m - S(70)
	Move(bNew, m, by, S(100), S(28))
	Move(bDel, m+S(110), by, S(100), S(28))
	Move(bUp, m, by+S(34), S(100), S(28))
	Move(bDown, m+S(110), by+S(34), S(100), S(28))
	rx := m + lw + S(14)
	rw := cw - rx - m
	Move(lName, rx, m, rw, S(20))
	Move(eName, rx, m+S(22), rw, S(24))
	Move(lCmd, rx, m+S(56), rw, S(20))
	Move(eCmd, rx, m+S(78), rw, S(190))
	Move(help, rx, m+S(276), rw, S(40))
	Move(bReset, rx, ch-m-S(30), S(130), S(30))
	Move(bCancel, cw-m-S(96), ch-m-S(30), S(96), S(30))
	Move(bOK, cw-m-S(96)*2-S(8), ch-m-S(30), S(96), S(30))

	names := func() []string {
		out := make([]string, len(items))
		for i, q := range items {
			out[i] = q.Name
		}
		return out
	}
	enableEditors := func(on bool) {
		Enable(eName, on)
		Enable(eCmd, on)
		Enable(bDel, on)
		Enable(bUp, on)
		Enable(bDown, on)
	}
	store := func() {
		if cur < 0 || cur >= len(items) {
			return
		}
		n := strings.TrimSpace(GetText(eName))
		if n == "" {
			n = "Unnamed"
		}
		cmd := strings.ReplaceAll(GetText(eCmd), "\r\n", "\n")
		cmd = strings.TrimSpace(cmd)
		if items[cur].Name != n {
			items[cur].Name = n
			SendMessage(lb, LB_DELETESTRING, uintptr(cur), 0)
			SendMessageStr(lb, LB_INSERTSTRING, uintptr(cur), n)
			SendMessage(lb, LB_SETCURSEL, uintptr(cur), 0)
		}
		items[cur].Command = cmd
	}
	load := func(i int) {
		cur = i
		if i < 0 || i >= len(items) {
			cur = -1
			SetText(eName, "")
			SetText(eCmd, "")
			enableEditors(false)
			return
		}
		SendMessage(lb, LB_SETCURSEL, uintptr(i), 0)
		SetText(eName, items[i].Name)
		SetText(eCmd, strings.ReplaceAll(items[i].Command, "\n", "\r\n"))
		enableEditors(true)
	}
	refill := func(sel int) {
		lbSet(lb, names())
		load(sel)
	}
	refill(0)

	d.OnCommand = func(id, code int, ctl uintptr) {
		switch id {
		case 300:
			if code == LBN_SELCHANGE {
				store()
				load(lbSel(lb))
			}
			return
		}
		if code != 0 {
			return
		}
		switch id {
		case 301:
			store()
			items = append(items, QuickCmd{Name: "New command", Command: ""})
			refill(len(items) - 1)
			Focus(eName)
			SendMessage(eName, EM_SETSEL, 0, ^uintptr(0))
		case 302:
			if cur >= 0 {
				items = append(items[:cur], items[cur+1:]...)
				n := cur
				if n >= len(items) {
					n = len(items) - 1
				}
				cur = -1
				refill(n)
			}
		case 303, 304:
			store()
			j := cur - 1
			if id == 304 {
				j = cur + 1
			}
			if cur >= 0 && j >= 0 && j < len(items) {
				items[cur], items[j] = items[j], items[cur]
				refill(j)
			}
		case 307:
			if MsgBox(d.Hwnd, "Restore defaults", "Replace all quick commands with the defaults?", MB_YESNO|MB_ICONQUESTION) == IDYES {
				items = append([]QuickCmd(nil), defaultQuick...)
				cur = -1
				refill(0)
			}
		case IDOK:
			if GetFocusHwnd() == eCmd { // Enter inside the multi-line box shouldn't close
				return
			}
			store()
			var clean []QuickCmd
			for _, q := range items {
				if strings.TrimSpace(q.Command) != "" {
					clean = append(clean, q)
				}
			}
			a.cfg.QuickCommands = clean
			if err := a.cfg.Save(); err != nil {
				MsgBox(d.Hwnd, "Save failed", err.Error(), MB_ICONERROR)
				return
			}
			d.EndModal()
			a.rebuildQuick()
		case IDCANCEL:
			d.EndModal()
		}
	}
	runModalFocus(d, a.w.Hwnd, lb)
}
