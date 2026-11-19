//go:build windows

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unsafe"

	"zephycube/pelicanconsole/pelican"
)

// session is one server console. Sessions stay open in the background after
// switching away, keep their log and player list, and count unread lines.
type session struct {
	srv       pelican.Server
	con       *pelican.Console
	connected bool
	everConn  bool
	closed    bool
	state     string
	stats     string
	connMsg   string
	log       []pendLine
	players   []string
	unread    int
	errs      int
	bedrock   bool
	listDone  bool
	hist      *pelican.History

	plParser *pelican.PlayerListParser
	plSilent bool
	plGen    int

	killVisible bool
	stopGen     int

	// Wings replays recent console lines after every (re)auth.
	replayUntil time.Time
	replaySeen  map[string]int
	replayFirst bool
}

type pendLine struct {
	s    *session
	text string
	kind pelican.LineKind
}

const (
	sessLogMax  = 6000
	sessLogKeep = 4000
	replayWin   = 1200 * time.Millisecond
)

const lineInfo pelican.LineKind = 100
const lineEcho pelican.LineKind = 101
const lineJoin pelican.LineKind = 102

func (a *App) curState() string {
	if a.sess == nil {
		return ""
	}
	return a.sess.state
}

func (a *App) curPlayers() []string {
	if a.sess == nil {
		return nil
	}
	return a.sess.players
}

func (a *App) isCur(s *session) bool { return s != nil && s == a.sess }

// ---------------- connect / switch / disconnect ----------------

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

// connect switches to the server's session, opening its console if needed.
func (a *App) connect(srv pelican.Server) {
	s := a.sessions[srv.Identifier]
	if s == nil {
		s = &session{srv: srv, state: a.states[srv.Identifier], hist: a.historyFor(srv.Identifier)}
		switch a.cfg.Edition[srv.Identifier] {
		case "bedrock":
			s.bedrock = true
		case "java":
		default:
			s.bedrock = pelican.GuessBedrock(srv)
		}
		a.sessions[srv.Identifier] = s
		a.addLines(s, []pendLine{{s, fmt.Sprintf("── Connecting to %s (%s) on %s ──", srv.Name, srv.Identifier, srv.Node), lineInfo}})
		s.connMsg = "◌ Connecting to console…"
		a.openConsole(s)
	}
	a.switchTo(s)
}

func (a *App) switchTo(s *session) {
	a.sess = s
	a.cur = &s.srv
	a.capActive = false
	s.unread, s.errs = 0, 0
	a.cfg.LastServer = s.srv.Identifier
	_ = a.cfg.Save()

	a.setEdition(s.bedrock)
	SetText(a.lName, s.srv.Name)
	SetText(a.lStats, s.stats)
	a.showState()
	a.showPlayers()
	SetText(a.lConn, s.connMsg)
	a.feedback("", fbNone)

	a.clearConsole()
	a.appendConsole(s.log)
	a.fillList()
	a.updateButtons()
}

func (a *App) openConsole(s *session) {
	s.con = a.client.OpenConsole(s.srv.Identifier, pelican.ConsoleHandlers{
		OnLine: func(l string, k pelican.LineKind) { a.queueLine(s, l, k) },
		OnStatus: func(st string) {
			Post(func() {
				if !s.closed {
					a.setSessState(s, st)
				}
			})
		},
		OnStats: func(st pelican.Stats) {
			Post(func() {
				if !s.closed {
					a.showStats(s, st)
				}
			})
		},
		OnConn: func(ok bool, err error) {
			Post(func() {
				if !s.closed {
					a.onConn(s, ok, err)
				}
			})
		},
		OnEvent: func(ev string, args []string) {
			Post(func() {
				if !s.closed {
					a.onWSEvent(s, ev, args)
				}
			})
		},
	})
}

func (a *App) onConn(s *session, ok bool, err error) {
	s.connected = ok
	if ok {
		s.connMsg = "● Console connected"
		// Wings replays recent output after auth: on the first connect it is old
		// history (shown, not counted); on a reconnect drop what we already have.
		s.replayUntil = time.Now().Add(replayWin)
		s.replayFirst = !s.everConn
		s.replaySeen = nil
		if s.everConn {
			s.replaySeen = map[string]int{}
			for _, l := range s.log[max(0, len(s.log)-400):] {
				s.replaySeen[l.text]++
			}
		}
		s.everConn = true
		// one silent `list` per session; after that joins/leaves come from the console
		if !s.listDone {
			s.listDone = true
			time.AfterFunc(1500*time.Millisecond, func() {
				Post(func() {
					if !s.closed && s.connected && s.state == "running" {
						a.refreshPlayers(s, true)
					}
				})
			})
		}
	} else {
		s.connMsg = "○ Console disconnected — retrying"
		if err != nil {
			s.connMsg += ": " + err.Error()
			a.addLines(s, []pendLine{{s, "── " + err.Error() + " — reconnecting… ──", pelican.LineError}})
		}
	}
	if a.isCur(s) {
		SetText(a.lConn, s.connMsg)
	}
}

// disconnect closes a session's console (the current one switches to another open session).
func (a *App) disconnect(s *session) {
	if s == nil || s.closed {
		return
	}
	s.closed = true
	if s.con != nil {
		s.con.Close()
	}
	s.hist.EndAll(time.Now())
	a.saveHistory()
	delete(a.sessions, s.srv.Identifier)
	if a.isCur(s) {
		var next *session
		for _, o := range a.sessions {
			if next == nil || strings.ToLower(o.srv.Name) < strings.ToLower(next.srv.Name) {
				next = o
			}
		}
		if next != nil {
			a.switchTo(next)
		} else {
			a.sess, a.cur = nil, nil
			a.capActive = false
			a.clearConsole()
			SetText(a.lName, "No server connected")
			SetText(a.lStats, "")
			SetText(a.lState, "")
			SetText(a.lConn, "○ Disconnected")
			a.showPlayers()
			a.updateButtons()
		}
	}
	a.fillList()
}

func (a *App) closeAll() {
	for _, s := range a.sessions {
		s.closed = true
		if s.con != nil {
			s.con.Close()
		}
		s.hist.EndAll(time.Now())
	}
	a.saveHistory()
}

// ---------------- console lines ----------------

func (a *App) queueLine(s *session, text string, k pelican.LineKind) {
	a.pendMu.Lock()
	a.pending = append(a.pending, pendLine{s, text, k})
	sched := !a.pendSched
	a.pendSched = true
	a.pendMu.Unlock()
	if sched {
		time.AfterFunc(40*time.Millisecond, func() { Post(a.flushPending) })
	}
}

// addLines appends to a session's log, and to the console if it is showing.
func (a *App) addLines(s *session, lines []pendLine) {
	if s == nil || len(lines) == 0 {
		return
	}
	s.log = append(s.log, lines...)
	if len(s.log) > sessLogMax {
		s.log = append([]pendLine(nil), s.log[len(s.log)-sessLogKeep:]...)
	}
	if a.isCur(s) {
		a.appendConsole(lines)
	}
}

func (a *App) flushPending() {
	a.pendMu.Lock()
	lines := a.pending
	a.pending = nil
	a.pendSched = false
	a.pendMu.Unlock()

	now := time.Now()
	by := map[*session][]pendLine{}
	var order []*session
	badges := false
	for _, l := range lines {
		s := l.s
		if s.closed {
			continue
		}
		replay := now.Before(s.replayUntil)
		if replay && s.replaySeen != nil {
			if s.replaySeen[l.text] > 0 {
				s.replaySeen[l.text]--
				continue
			}
		}
		if l.kind == pelican.LineConsole && !s.bedrock && s.srv.Identifier != "" && a.cfg.Edition[s.srv.Identifier] == "" &&
			(strings.Contains(l.text, "Player Spawned:") || strings.Contains(l.text, "Player connected:") ||
				strings.Contains(l.text, "Bedrock Edition") || strings.Contains(l.text, "bedrock_server")) {
			s.bedrock = true
			if a.isCur(s) {
				a.setEdition(true)
			}
		}
		if l.kind == pelican.LineConsole {
			if ev, ok := pelican.ParsePlayerEvent(l.text); ok {
				l.kind = lineJoin
				// old history replayed on first connect: the one-time `list` establishes who's online
				if !(replay && s.replayFirst) {
					a.playerEvent(s, ev)
				}
			}
		}
		if _, seen := by[s]; !seen {
			order = append(order, s)
		}
		by[s] = append(by[s], l)

		if !a.isCur(s) && !replay && strings.TrimSpace(l.text) != "" {
			s.unread++
			if l.kind == pelican.LineError || (l.kind == pelican.LineConsole && pelican.IsErrorLine(l.text)) {
				s.errs++
			}
			badges = true
		}
	}
	for _, s := range order {
		ls := by[s]
		a.addLines(s, ls)
		for _, l := range ls {
			if (l.kind != pelican.LineConsole && l.kind != lineJoin) || strings.TrimSpace(l.text) == "" {
				continue
			}
			if a.isCur(s) && a.capActive {
				a.capLines = append(a.capLines, l.text)
			}
			if s.plParser != nil && s.plParser.Feed(l.text) {
				a.playersDone(s)
			}
		}
	}
	if badges {
		a.refreshBadges()
	}
}

// refreshBadges updates the server list names (unread / error markers).
func (a *App) refreshBadges() {
	for i, r := range a.rows {
		lvSetText(a.lv, i, 0, a.rowName(r))
	}
}

func (a *App) rowName(s pelican.Server) string {
	name := s.Name
	if ss := a.sessions[s.Identifier]; ss != nil {
		switch {
		case a.isCur(ss):
			name = "● " + name
		case ss.errs > 0:
			name = fmt.Sprintf("⚠ %s  (%d)", name, ss.unread)
		case ss.unread > 0:
			name = fmt.Sprintf("◉ %s  (%d)", name, ss.unread)
		default:
			name = "○ " + name
		}
	}
	if a.isHidden(s.Identifier) {
		name += "  (hidden)"
	}
	return name
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
	if len(lines) == 0 {
		return
	}
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

	if n := int(SendMessage(h, EM_GETLINECOUNT, 0, 0)); n > sessLogMax {
		cut := int32(SendMessage(h, EM_LINEINDEX, uintptr(n-sessLogKeep), 0))
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

// clearConsole clears the view and the current session's log.
func (a *App) clearConsole() {
	SetText(a.console, "")
	a.setDefaultCharFormat()
}

func (a *App) clearSessionLog() {
	if a.sess != nil {
		a.sess.log = nil
	}
	a.clearConsole()
}

// ---------------- state / stats / events ----------------

func (a *App) setSessState(s *session, st string) {
	if st == "" {
		st = "unknown"
	}
	prev := s.state
	s.state = st
	a.setRowState(s.srv.Identifier, st)
	switch st {
	case "offline":
		s.stats = ""
		s.killVisible = false
		s.stopGen++
		a.setPlayers(s, nil)
	case "starting":
		a.setPlayers(s, nil)
	case "running":
		if s.killVisible { // restart got through
			s.killVisible = false
			s.stopGen++
		}
	}
	if (st == "offline" || st == "starting") && prev != st {
		s.hist.EndAll(time.Now())
		a.saveHistory()
	}
	if a.isCur(s) {
		a.showState()
		if st == "offline" {
			SetText(a.lStats, "")
		}
		a.updateButtons()
	}
}

func (a *App) showState() {
	st := a.curState()
	if a.sess == nil {
		SetText(a.lState, "")
	} else {
		SetText(a.lState, "●  "+strings.ToUpper(st))
	}
	pInvalidateRect.Call(a.lState, 0, 1)
}

func (a *App) showStats(s *session, st pelican.Stats) {
	mem := pelican.HumanBytes(st.MemoryBytes)
	if st.MemoryLimitBytes > 0 {
		mem += " / " + pelican.HumanBytes(st.MemoryLimitBytes)
	}
	t := fmt.Sprintf("CPU %.1f%%    RAM %s    Disk %s", st.CPUAbsolute, mem, pelican.HumanBytes(st.DiskBytes))
	if st.Uptime > 0 {
		t += "    Up " + pelican.HumanUptime(st.Uptime)
	}
	s.stats = t
	if a.isCur(s) {
		SetText(a.lStats, t)
	}
}

func (a *App) onWSEvent(s *session, ev string, args []string) {
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
		if !a.isCur(s) {
			msg += " — " + s.srv.Name
		}
		a.feedback(msg, fbOK)
		a.addLines(s, []pendLine{{s, "── " + msg + " ──", lineInfo}})
	} else {
		a.feedback("✖ Backup failed on "+s.srv.Name+" — check the panel's Backups tab", fbErr)
		a.addLines(s, []pendLine{{s, "── Backup failed ──", pelican.LineError}})
	}
}

// ---------------- players ----------------

func (a *App) playerEvent(s *session, ev pelican.PlayerEvent) {
	now := time.Now()
	switch ev.Kind {
	case pelican.PlayerJoined:
		s.hist.Join(ev.Name, ev.XUID, now, false)
		a.addPlayer(s, ev.Name)
	case pelican.PlayerLeft:
		s.hist.Leave(ev.Name, ev.XUID, now)
		a.removePlayer(s, ev.Name)
	case pelican.PlayerConnected:
		return // wait for "Player Spawned"
	}
	a.saveHistory()
}

func (a *App) setPlayers(s *session, list []string) {
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
	s.players = out
	if a.isCur(s) {
		a.showPlayers()
	}
}

func (a *App) showPlayers() {
	out := a.curPlayers()
	lbSet(a.lbPlayers, out)
	text := GetText(a.cbPlayer)
	comboSet(a.cbPlayer, out)
	SetText(a.cbPlayer, text)
	a.invalidatePlayersCaption()
}

func (a *App) addPlayer(s *session, n string) {
	for _, p := range s.players {
		if strings.EqualFold(p, n) {
			return
		}
	}
	a.setPlayers(s, append(append([]string(nil), s.players...), n))
}

func (a *App) removePlayer(s *session, n string) {
	var out []string
	for _, p := range s.players {
		if !strings.EqualFold(p, n) {
			out = append(out, p)
		}
	}
	a.setPlayers(s, out)
}

func (a *App) refreshPlayers(s *session, silent bool) {
	if s == nil {
		return
	}
	if !s.connected {
		if !silent {
			a.feedback("✖ Console not connected — can't read the player list.", fbErr)
		}
		return
	}
	s.plParser = &pelican.PlayerListParser{}
	s.plSilent = silent
	s.plGen++
	g := s.plGen
	if !silent {
		a.addLines(s, []pendLine{{s, "> list", lineEcho}})
		a.feedback("… fetching online players", fbBusy)
	}
	c, id := a.client, s.srv.Identifier
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err := c.SendCommand(ctx, id, "list")
		cancel()
		Post(func() {
			if err != nil && g == s.plGen {
				s.plParser = nil
				if !silent {
					a.feedback("✖ list: "+err.Error(), fbErr)
				}
			}
		})
		time.Sleep(4 * time.Second)
		Post(func() {
			if g == s.plGen && s.plParser != nil {
				s.plParser = nil
				if !silent {
					a.feedback("✖ Couldn't read a player list from the console reply.", fbErr)
				}
			}
		})
	}()
}

func (a *App) playersDone(s *session) {
	p := s.plParser
	s.plParser = nil
	a.setPlayers(s, p.Players)
	// reconcile history with the authoritative list
	now := time.Now()
	in := map[string]bool{}
	for _, n := range p.Players {
		in[strings.ToLower(n)] = true
		s.hist.Join(n, "", now, true)
	}
	for _, r := range s.hist.Players {
		if r.Online && !in[strings.ToLower(r.Name)] {
			s.hist.Leave(r.Name, "", now)
		}
	}
	a.saveHistory()
	if s.plSilent || !a.isCur(s) {
		return
	}
	if len(p.Players) == 0 {
		a.feedback("✔ No players online", fbInfo)
	} else {
		a.feedback(fmt.Sprintf("✔ %d online: %s", len(p.Players), strings.Join(p.Players, ", ")), fbOK)
	}
}

// ---------------- player history store ----------------

func historyPath() string { return filepath.Join(filepath.Dir(configPath()), "players.json") }

func (a *App) loadHistory() {
	a.hist = map[string]*pelican.History{}
	if b, err := os.ReadFile(historyPath()); err == nil {
		_ = json.Unmarshal(b, &a.hist)
	}
}

func (a *App) historyFor(id string) *pelican.History {
	h := a.hist[id]
	if h == nil {
		h = &pelican.History{Players: map[string]*pelican.PlayerRecord{}}
		a.hist[id] = h
	}
	if h.Players == nil {
		h.Players = map[string]*pelican.PlayerRecord{}
	}
	return h
}

func (a *App) saveHistory() {
	now := time.Now()
	for _, h := range a.hist {
		h.Touch(now)
	}
	b, err := json.MarshalIndent(a.hist, "", " ")
	if err != nil {
		return
	}
	p := historyPath()
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	tmp := p + ".tmp"
	if os.WriteFile(tmp, b, 0o600) == nil {
		_ = os.Rename(tmp, p)
	}
}
