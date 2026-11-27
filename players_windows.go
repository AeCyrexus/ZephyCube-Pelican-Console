//go:build windows

package main

import (
	"fmt"
	"strings"
	"time"
	"unsafe"

	"zephycube/pelicanconsole/pelican"
)

const (
	idPmTP = 4001 + iota
	idPmSummon
	idPmGive
	idPmEffect
	idPmEffClear
	idPmClearInv
	idPmKick
	idPmBan
	idPmPardon
	idPmOp
	idPmDeop
	idPmCopyName
	idPmCopyXUID
	idPmHistory
	idPmUse
	idPmForget
	idPmArmor
	idPmMode // + index into modes
)

const enChange = 0x0300

func (a *App) isOnline(name string) bool {
	for _, p := range a.curPlayers() {
		if strings.EqualFold(p, name) {
			return true
		}
	}
	return false
}

// onContextMenu handles right-clicks on the Online Players list.
func (a *App) onContextMenu(ctl uintptr, x, y int32) bool {
	if ctl != a.lbPlayers {
		return false
	}
	i := lbSel(a.lbPlayers)
	if x != -1 || y != -1 {
		i = lbItemAt(a.lbPlayers, x, y)
	}
	pl := a.curPlayers()
	if i < 0 || i >= len(pl) {
		return true
	}
	SendMessage(a.lbPlayers, LB_SETCURSEL, uintptr(i), 0)
	SetText(a.cbPlayer, pl[i])
	a.playerMenu(a.w.Hwnd, pl[i])
	return true
}

func popupAt(owner, menu uintptr) int {
	var pt POINT
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	cmd, _, _ := pTrackPopupMenu.Call(menu, TPM_RETURNCMD|TPM_RIGHTBUTTON, uintptr(pt.X), uintptr(pt.Y), 0, owner, 0)
	return int(cmd)
}

func (a *App) giveLabel() string {
	id := strings.TrimPrefix(strings.TrimSpace(GetText(a.cbItem)), "minecraft:")
	if id == "" {
		return ""
	}
	return fmt.Sprintf("Give %s × %s", strings.TrimSpace(GetText(a.eGiveQty)), id)
}

func (a *App) effectLabel() string {
	i := comboSel(a.cbEffect)
	if i < 0 || i >= len(a.effView) {
		return ""
	}
	dur := strings.TrimSpace(GetText(a.eEffDur))
	if dur == "" || strings.EqualFold(dur, "infinite") {
		dur = "∞"
	} else {
		dur += "s"
	}
	return fmt.Sprintf("Effect: %s %s (%s)", a.effView[i].Name, strings.TrimSpace(GetText(a.eEffLvl)), dur)
}

// playerMenu shows the action menu for a player (online or from history).
func (a *App) playerMenu(owner uintptr, name string) {
	if a.sess == nil {
		return
	}
	online := a.isOnline(name)
	rec := a.sess.hist.Get(name)
	menu, _, _ := pCreatePopupMenu.Call()
	defer pDestroyMenu.Call(menu)

	title := name
	if online {
		if rec != nil && rec.Online {
			title += "  ·  online " + pelican.HumanDuration(rec.SessionSec(time.Now()))
		} else {
			title += "  ·  online"
		}
	} else if rec != nil {
		title += "  ·  last seen " + rec.LastSeen.Local().Format("Jan 2 15:04")
	}
	appendMenu(menu, MF_GRAYED, 0, title)
	appendMenu(menu, MF_SEPARATOR, 0, "")
	gray := func(on bool) uint32 {
		if on {
			return MF_STRING
		}
		return MF_GRAYED
	}
	if online {
		appendMenu(menu, gray(a.cfg.IGN != ""), idPmTP, "Teleport to player")
		appendMenu(menu, gray(a.cfg.IGN != ""), idPmSummon, "Summon to me")
		sub, _, _ := pCreatePopupMenu.Call()
		for i, m := range modes {
			appendMenu(sub, MF_STRING, uintptr(idPmMode+i), strings.ToUpper(m[:1])+m[1:])
		}
		appendMenu(menu, MF_POPUP, sub, "Gamemode")
		if g := a.giveLabel(); g != "" {
			appendMenu(menu, MF_STRING, idPmGive, g)
		} else {
			appendMenu(menu, MF_GRAYED, idPmGive, "Give… (pick an item under Player Actions)")
		}
		appendMenu(menu, MF_STRING, idPmArmor, "Give "+a.armorLabel())
		if e := a.effectLabel(); e != "" {
			appendMenu(menu, MF_STRING, idPmEffect, e)
		}
		appendMenu(menu, MF_STRING, idPmEffClear, "Clear effects")
		appendMenu(menu, MF_STRING, idPmClearInv, "Clear inventory")
		appendMenu(menu, MF_SEPARATOR, 0, "")
		appendMenu(menu, MF_STRING, idPmKick, "Kick…")
	}
	appendMenu(menu, MF_STRING, idPmBan, "Ban…")
	if !online {
		appendMenu(menu, MF_STRING, idPmPardon, "Pardon (unban)")
	}
	appendMenu(menu, MF_STRING, idPmOp, "Op")
	appendMenu(menu, MF_STRING, idPmDeop, "Deop")
	appendMenu(menu, MF_SEPARATOR, 0, "")
	appendMenu(menu, MF_STRING, idPmCopyName, "Copy name")
	xuid := ""
	if rec != nil {
		xuid = rec.XUID
	}
	if xuid != "" {
		appendMenu(menu, MF_STRING, idPmCopyXUID, "Copy XUID  ("+xuid+")")
	} else {
		appendMenu(menu, MF_GRAYED, idPmCopyXUID, "Copy XUID (unknown)")
	}
	if owner == a.w.Hwnd {
		appendMenu(menu, MF_SEPARATOR, 0, "")
		appendMenu(menu, MF_STRING, idPmHistory, "Player history…")
	}
	a.playerAction(owner, name, popupAt(owner, menu))
}

func (a *App) playerAction(owner uintptr, name string, cmd int) {
	if cmd == 0 || a.sess == nil {
		return
	}
	SetText(a.cbPlayer, name)
	switch {
	case cmd >= idPmMode && cmd < idPmMode+len(modes):
		m := modes[cmd-idPmMode]
		a.runTemplate("Gamemode "+m+" → {player}", "gamemode "+m+" {player}")
		return
	}
	switch cmd {
	case idPmTP:
		a.runTemplate("TP → {player}", "tp {me} {player}")
	case idPmSummon:
		a.runTemplate("Summon {player}", "tp {player} {me}")
	case idPmGive:
		a.give()
	case idPmArmor:
		a.giveArmor()
	case idPmEffect:
		a.applyEffect()
	case idPmEffClear:
		if a.bedrock {
			a.runTemplate("Clear effects of {player}", "effect {player} clear")
		} else {
			a.runTemplate("Clear effects of {player}", "effect clear {player}")
		}
	case idPmClearInv:
		a.runTemplate("Clear inventory of {player}", "clear {player}")
	case idPmKick:
		r, ok := promptText(owner, "Kick "+name, "Kick "+name+" from "+a.cur.Name+".\nReason (optional):", "")
		if ok {
			a.runTemplate("Kick {player}", strings.TrimSpace("kick {player} "+oneLine(r)))
		}
	case idPmBan:
		note := ""
		if a.bedrock {
			note = "\nBedrock Dedicated Server has no built-in /ban — this needs a plugin (e.g. Endstone)."
		}
		r, ok := promptText(owner, "Ban "+name, "Ban "+name+" from "+a.cur.Name+"."+note+"\nReason (optional):", "")
		if ok {
			a.runTemplate("Ban {player}", strings.TrimSpace("ban {player} "+oneLine(r)))
		}
	case idPmPardon:
		a.runTemplate("Pardon {player}", "pardon {player}")
	case idPmOp:
		a.runTemplate("Op {player}", "op {player}")
	case idPmDeop:
		a.runTemplate("Deop {player}", "deop {player}")
	case idPmCopyName:
		if setClipboard(owner, name) {
			a.feedback("✔ Copied “"+name+"”", fbInfo)
		}
	case idPmCopyXUID:
		if r := a.sess.hist.Get(name); r != nil && r.XUID != "" && setClipboard(owner, r.XUID) {
			a.feedback("✔ Copied XUID "+r.XUID+" ("+name+")", fbInfo)
		}
	case idPmHistory:
		a.openHistory(name)
	}
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(strings.NewReplacer("\r", " ", "\n", " ").Replace(s)), " ")
}

// ---------------- player history dialog ----------------

func (a *App) openHistory(focus string) {
	if a.sess == nil {
		a.feedback("Connect to a server first.", fbErr)
		return
	}
	s := a.sess
	cw, ch := S(820), S(460)
	d := newDialog(a.w.Hwnd, "Player history — "+s.srv.Name, cw, ch)
	m := S(14)
	lInfo := d.Label("", SS_ENDELLIPSIS)
	Muted(lInfo)
	eFilter := d.Edit("", 0, 100)
	SetCue(eFilter, "Filter by name or XUID")
	lv := d.Ctl("SysListView32", "", WS_TABSTOP|LVS_REPORT|LVS_SINGLESEL|LVS_SHOWSELALWAYS|LVS_NOSORTHEADER, 0, 101, roleListView)
	SendMessage(lv, LVM_SETEXTENDEDLISTVIEWSTYLE, LVS_EX_FULLROWSELECT|LVS_EX_DOUBLEBUFFER, LVS_EX_FULLROWSELECT|LVS_EX_DOUBLEBUFFER)
	for i, c := range []struct {
		t string
		w int
	}{{"Player", 150}, {"Status", 120}, {"XUID", 130}, {"Sessions", 62}, {"Total time", 80}, {"First seen", 100}, {"Last seen", 100}} {
		lvAddColumn(lv, i, c.t, S(c.w))
	}
	btns := []struct {
		label string
		id    int
		k     btnKind
		w     int
	}{
		{"Use as player", idPmUse, kPrimary, 104},
		{"Kick…", idPmKick, kNormal, 64},
		{"Ban…", idPmBan, kDanger, 64},
		{"Pardon", idPmPardon, kNormal, 70},
		{"Copy name", idPmCopyName, kNormal, 84},
		{"Copy XUID", idPmCopyXUID, kNormal, 84},
		{"Forget", idPmForget, kNormal, 64},
	}
	x := m
	for _, b := range btns {
		h := d.Btn(b.label, b.id, b.k)
		Move(h, x, ch-m-S(30), S(b.w), S(30))
		x += S(b.w) + S(6)
	}
	bClose := d.Btn("Close", IDCANCEL, kNormal)
	Move(bClose, cw-m-S(84), ch-m-S(30), S(84), S(30))
	Move(lInfo, m, m, cw-2*m-S(240), S(26))
	Move(eFilter, cw-m-S(230), m, S(230), S(26))
	Move(lv, m, m+S(34), cw-2*m, ch-2*m-S(34)-S(30)-S(10))

	var rows []*pelican.PlayerRecord
	fill := func(selName string) {
		now := time.Now()
		f := strings.ToLower(strings.TrimSpace(GetText(eFilter)))
		all := s.hist.Sorted()
		rows = rows[:0]
		online := 0
		for _, r := range all {
			if r.Online {
				online++
			}
			if f == "" || strings.Contains(strings.ToLower(r.Name), f) || strings.Contains(r.XUID, f) {
				rows = append(rows, r)
			}
		}
		SetText(lInfo, fmt.Sprintf("%d players seen · %d online · right-click a player for actions", len(all), online))
		SendMessage(lv, WM_SETREDRAW, 0, 0)
		SendMessage(lv, LVM_DELETEALLITEMS, 0, 0)
		sel := -1
		for i, r := range rows {
			status := "Last session " + pelican.HumanDuration(r.LastSessSec)
			if r.Online {
				d := pelican.HumanDuration(r.SessionSec(now))
				if r.Approx {
					d = "≥ " + d
				}
				status = "● Online " + d
			}
			total := r.TotalSec + r.SessionSec(now)
			xuid := r.XUID
			if xuid == "" {
				xuid = "—"
			}
			lvAddRow(lv, i, r.Name, status, xuid, fmt.Sprint(r.Sessions), pelican.HumanDuration(total),
				r.FirstSeen.Local().Format("Jan 2 15:04"), r.LastSeen.Local().Format("Jan 2 15:04"))
			if strings.EqualFold(r.Name, selName) {
				sel = i
			}
		}
		if sel < 0 && len(rows) > 0 {
			sel = 0
		}
		if sel >= 0 {
			lvSelect(lv, sel)
		}
		SendMessage(lv, WM_SETREDRAW, 1, 0)
		pInvalidateRect.Call(lv, 0, 1)
	}
	selected := func() *pelican.PlayerRecord {
		if i := lvSelected(lv); i >= 0 && i < len(rows) {
			return rows[i]
		}
		return nil
	}
	refreshLater := func() {
		time.AfterFunc(2*time.Second, func() {
			Post(func() {
				if !d.modalDone {
					cur := ""
					if r := selected(); r != nil {
						cur = r.Name
					}
					fill(cur)
				}
			})
		})
	}
	act := func(id int) {
		r := selected()
		if r == nil {
			return
		}
		switch id {
		case idPmUse:
			SetText(a.cbPlayer, r.Name)
			d.EndModal()
		case idPmForget:
			if r.Online {
				a.feedback("✖ "+r.Name+" is online — can't forget an online player.", fbErr)
				return
			}
			if MsgBox(d.Hwnd, "Forget player", "Remove "+r.Name+" from the history of "+s.srv.Name+"?", MB_YESNO|MB_ICONWARNING) != IDYES {
				return
			}
			for k, v := range s.hist.Players {
				if v == r {
					delete(s.hist.Players, k)
				}
			}
			a.saveHistory()
			fill("")
		case idPmKick:
			if !r.Online {
				a.feedback("✖ "+r.Name+" isn't online — use Ban instead.", fbErr)
				return
			}
			a.playerAction(d.Hwnd, r.Name, id)
			refreshLater()
		default:
			a.playerAction(d.Hwnd, r.Name, id)
		}
	}
	d.OnCommand = func(id, code int, ctl uintptr) {
		if ctl == eFilter {
			if code == enChange {
				fill("")
			}
			return
		}
		if code != 0 {
			return
		}
		switch id {
		case IDCANCEL:
			d.EndModal()
		case IDOK:
			act(idPmUse)
		default:
			act(id)
		}
	}
	d.OnNotify = func(nm *NMHDR) (uintptr, bool) {
		if nm.HwndFrom != lv {
			return 0, false
		}
		switch int32(nm.Code) {
		case NM_DBLCLK:
			act(idPmUse)
			return 0, true
		case NM_RCLICK:
			if r := selected(); r != nil {
				a.playerMenu(d.Hwnd, r.Name)
				refreshLater()
			}
			return 0, true
		case NM_CUSTOMDRAW:
			cd := (*NMLVCUSTOMDRAW)(unsafe.Pointer(nm))
			switch cd.Nmcd.DrawStage {
			case CDDS_PREPAINT:
				return CDRF_NOTIFYITEMDRAW, true
			case CDDS_ITEM | CDDS_PREPAINT:
				row := int(cd.Nmcd.ItemSpec)
				cd.ClrText = th.Text
				if row >= 0 && row < len(rows) && rows[row].Online {
					cd.ClrText = th.Ok
				}
				return CDRF_NEWFONT, true
			}
		}
		return 0, false
	}
	fill(focus)
	runModalFocus(d, a.w.Hwnd, lv)
}
