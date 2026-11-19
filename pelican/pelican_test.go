package pelican

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- tiny server-side websocket for tests ---
func srvRead(br *bufio.Reader) (byte, []byte, error) {
	var h [2]byte
	if _, err := io.ReadFull(br, h[:]); err != nil {
		return 0, nil, err
	}
	n := uint64(h[1] & 0x7f)
	if n == 126 {
		var b [2]byte
		io.ReadFull(br, b[:])
		n = uint64(binary.BigEndian.Uint16(b[:]))
	} else if n == 127 {
		var b [8]byte
		io.ReadFull(br, b[:])
		n = binary.BigEndian.Uint64(b[:])
	}
	var m [4]byte
	io.ReadFull(br, m[:])
	p := make([]byte, n)
	io.ReadFull(br, p)
	for i := range p {
		p[i] ^= m[i&3]
	}
	return h[0] & 0xf, p, nil
}
func srvWrite(c net.Conn, op byte, p []byte) {
	h := []byte{0x80 | op}
	switch {
	case len(p) < 126:
		h = append(h, byte(len(p)))
	case len(p) <= 0xffff:
		h = append(h, 126, byte(len(p)>>8), byte(len(p)))
	default:
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], uint64(len(p)))
		h = append(append(h, 127), b[:]...)
	}
	c.Write(append(h, p...))
}
func ev(e string, args ...string) []byte {
	b, _ := json.Marshal(map[string]any{"event": e, "args": args})
	return b
}

type fake struct {
	mu       sync.Mutex
	commands []string
	power    []string
	origin   string
	authed   bool
	srvURL   string
}

func newFake(t *testing.T) (*fake, *httptest.Server) {
	f := &fake{}
	mux := http.NewServeMux()
	auth := func(w http.ResponseWriter, r *http.Request) bool {
		if r.Header.Get("Authorization") != "Bearer KEY" {
			w.WriteHeader(401)
			w.Write([]byte(`{"errors":[{"code":"AuthenticationException","status":"401","detail":"Unauthenticated."}]}`))
			return false
		}
		return true
	}
	mux.HandleFunc("/api/client", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		page := r.URL.Query().Get("page")
		if page == "1" {
			w.Write([]byte(`{"object":"list","data":[{"object":"server","attributes":{"identifier":"abc12345","uuid":"u1","name":"ZEPHORIA SMP","node":"PHL-01","status":null,"description":null,"limits":{"memory":4096,"disk":0,"cpu":0},"feature_limits":{"backups":3}}}],"meta":{"pagination":{"total":2,"total_pages":2}}}`))
		} else {
			w.Write([]byte(`{"object":"list","data":[{"object":"server","attributes":{"identifier":"def67890","uuid":"u2","name":"Hardcore","node":"SGP-01"}}],"meta":{"pagination":{"total":2,"total_pages":2}}}`))
		}
	})
	mux.HandleFunc("/api/client/servers/abc12345/command", func(w http.ResponseWriter, r *http.Request) {
		if !auth(w, r) {
			return
		}
		var b map[string]string
		json.NewDecoder(r.Body).Decode(&b)
		f.mu.Lock()
		f.commands = append(f.commands, b["command"])
		f.mu.Unlock()
		w.WriteHeader(204)
	})
	mux.HandleFunc("/api/client/servers/def67890/command", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(502)
		w.Write([]byte(`{"errors":[{"code":"HttpException","status":"502","detail":"Server must be online in order to send commands."}]}`))
	})
	mux.HandleFunc("/api/client/servers/abc12345/power", func(w http.ResponseWriter, r *http.Request) {
		var b map[string]string
		json.NewDecoder(r.Body).Decode(&b)
		f.mu.Lock()
		f.power = append(f.power, b["signal"])
		f.mu.Unlock()
		w.WriteHeader(204)
	})
	mux.HandleFunc("/api/client/servers/abc12345/backups", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			w.Write([]byte(`{"data":[{"attributes":{"uuid":"new","name":"n","is_locked":false,"created_at":"2026-11-19T10:00:00+00:00","completed_at":"2026-11-19T10:01:00+00:00"}},{"attributes":{"uuid":"lockedold","name":"l","is_locked":true,"created_at":"2026-11-01T10:00:00+00:00","completed_at":"x"}},{"attributes":{"uuid":"old","name":"o","is_locked":false,"created_at":"2026-11-10T10:00:00+00:00","completed_at":"x"}}],"meta":{"pagination":{"total_pages":1}}}`))
			return
		}
		w.Write([]byte(`{"object":"backup","attributes":{"uuid":"b1","name":"test","is_successful":false}}`))
	})
	mux.HandleFunc("/api/client/servers/abc12345/backups/old", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.power = append(f.power, r.Method+" old")
		f.mu.Unlock()
		w.WriteHeader(204)
	})
	mux.HandleFunc("/api/client/servers/abc12345/resources", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"object":"stats","attributes":{"current_state":"running","is_suspended":false,"resources":{"memory_bytes":1073741824,"cpu_absolute":12.5,"uptime":3600000}}}`))
	})
	mux.HandleFunc("/api/client/servers/abc12345/websocket", func(w http.ResponseWriter, r *http.Request) {
		ws := "ws" + strings.TrimPrefix(f.srvURL, "http") + "/ws"
		w.Write([]byte(`{"data":{"token":"TOKEN","socket":"` + ws + `"}}`))
	})
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.origin = r.Header.Get("Origin")
		f.mu.Unlock()
		key := r.Header.Get("Sec-WebSocket-Key")
		h := sha1.Sum([]byte(key + wsGUID))
		hj, _ := w.(http.Hijacker)
		c, brw, _ := hj.Hijack()
		defer c.Close()
		brw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: " + base64.StdEncoding.EncodeToString(h[:]) + "\r\n\r\n")
		brw.Flush()
		br := brw.Reader
		for {
			op, p, err := srvRead(br)
			if err != nil {
				return
			}
			if op == opPing {
				srvWrite(c, opPong, p)
				continue
			}
			var m struct {
				Event string   `json:"event"`
				Args  []any    `json:"args"`
			}
			json.Unmarshal(p, &m)
			switch m.Event {
			case "auth":
				if m.Args[0] == "TOKEN" {
					f.mu.Lock()
					f.authed = true
					f.mu.Unlock()
					srvWrite(c, opText, ev("auth success"))
				}
			case "send logs":
				srvWrite(c, opText, ev("console output", "\x1b[33m[12:00:00 INFO]: Done (3.2s)!\x1b[0m"))
				srvWrite(c, opText, ev("console output", strings.Repeat("x", 70000))) // 64-bit length frame
				srvWrite(c, opText, ev("status", "running"))
				st, _ := json.Marshal(map[string]any{"memory_bytes": 5, "cpu_absolute": 1.5, "state": "running"})
				srvWrite(c, opText, ev("stats", string(st)))
				srvWrite(c, opText, ev("backup completed", `{"uuid":"b1","is_successful":true}`))
			}
		}
	})
	s := httptest.NewServer(mux)
	f.srvURL = s.URL
	return f, s
}

func TestAPI(t *testing.T) {
	f, s := newFake(t)
	defer s.Close()
	c := New(s.URL+"/", "KEY")
	ctx := t.Context()
	srv, err := c.ListServers(ctx, false)
	if err != nil || len(srv) != 2 || srv[0].Name != "ZEPHORIA SMP" || srv[1].Node != "SGP-01" || srv[0].FeatureLimits.Backups != 3 {
		t.Fatalf("list: %v %+v", err, srv)
	}
	if err := c.SendCommand(ctx, "abc12345", "gamemode creative Steve"); err != nil {
		t.Fatal(err)
	}
	if f.commands[0] != "gamemode creative Steve" {
		t.Fatal(f.commands)
	}
	err = c.SendCommand(ctx, "def67890", "x")
	if err == nil || !strings.Contains(err.Error(), "must be online") {
		t.Fatalf("want 502 detail, got %v", err)
	}
	if err := c.Power(ctx, "abc12345", "restart"); err != nil || f.power[0] != "restart" {
		t.Fatal(err, f.power)
	}
	b, err := c.CreateBackup(ctx, "abc12345", "test")
	if err != nil || b.UUID != "b1" {
		t.Fatal(err, b)
	}
	bl, err := c.ListBackups(ctx, "abc12345")
	if err != nil || len(bl) != 3 {
		t.Fatal(err, bl)
	}
	if v := OldestRemovable(bl); v == nil || v.UUID != "old" {
		t.Fatal("oldest", v)
	}
	if err := c.DeleteBackup(ctx, "abc12345", "old"); err != nil || f.power[len(f.power)-1] != "DELETE old" {
		t.Fatal(err, f.power)
	}
	r, err := c.Resources(ctx, "abc12345")
	if err != nil || r.CurrentState != "running" || r.Resources.CPUAbsolute != 12.5 {
		t.Fatal(err, r)
	}
	bad := New(s.URL, "NOPE")
	if _, err := bad.ListServers(ctx, false); err == nil || !strings.Contains(err.Error(), "Unauthenticated") {
		t.Fatal(err)
	}
}

func TestConsole(t *testing.T) {
	f, s := newFake(t)
	defer s.Close()
	c := New(s.URL, "KEY")
	var mu sync.Mutex
	var lines []string
	var status string
	var stats Stats
	var events []string
	connected := make(chan bool, 4)
	done := make(chan struct{})
	con := c.OpenConsole("abc12345", ConsoleHandlers{
		OnLine: func(l string, k LineKind) { mu.Lock(); lines = append(lines, l); mu.Unlock() },
		OnStatus: func(st string) { mu.Lock(); status = st; mu.Unlock() },
		OnStats:  func(st Stats) { mu.Lock(); stats = st; mu.Unlock() },
		OnConn:   func(ok bool, err error) { connected <- ok },
		OnEvent: func(e string, a []string) {
			mu.Lock()
			events = append(events, e+"|"+a[0])
			mu.Unlock()
			close(done)
		},
	})
	defer con.Close()
	if !<-connected {
		t.Fatal("not connected")
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
	mu.Lock()
	defer mu.Unlock()
	if lines[0] != "[12:00:00 INFO]: Done (3.2s)!" || len(lines[1]) != 70000 {
		t.Fatalf("lines %q", lines[0])
	}
	if status != "running" || stats.CPUAbsolute != 1.5 {
		t.Fatal(status, stats)
	}
	if f.origin != strings.TrimRight(s.URL, "/") {
		t.Fatal("origin", f.origin)
	}
	if !strings.Contains(events[0], "is_successful") {
		t.Fatal(events)
	}
	if err := con.Send("send command", "list"); err != nil {
		t.Fatal(err)
	}
}

func TestPlayerList(t *testing.T) {
	cases := []struct {
		lines []string
		want  string
	}{
		{[]string{"[12:00:00 INFO]: There are 2 of a max of 20 players online: Steve, Alex"}, "Steve,Alex"},
		{[]string{"[2026-11-19 13:00:00:123 INFO] There are 2/10 players online:", "AeCyrexus, Bob"}, "AeCyrexus,Bob"},
		{[]string{"noise", "There are 0 of a max of 20 players online:"}, ""},
		{[]string{"[12:00:00 INFO]: There are 1 of a max of 50 players online.", "[12:00:00 INFO]: default: ~Nick"}, "Nick"},
	}
	for _, c := range cases {
		p := &PlayerListParser{}
		for _, l := range c.lines {
			p.Feed(l)
		}
		if !p.Done || strings.Join(p.Players, ",") != c.want {
			t.Errorf("%v → %v (done=%v)", c.lines, p.Players, p.Done)
		}
	}
	if !IsErrorLine("Unknown or incomplete command, see below for error") || !IsErrorLine("No targets matched selector") || IsErrorLine("Set own game mode to Creative") {
		t.Error("classify")
	}
	if StripANSI("\x1b[1;31mhi\x1b[0m") != "hi" {
		t.Error("ansi")
	}
}

func TestJoinLeave(t *testing.T) {
	cases := []struct {
		line   string
		name   string
		joined bool
		ok     bool
	}{
		{"[12:00:00 INFO]: Steve joined the game", "Steve", true, true},
		{"[12:00:00] [Server thread/INFO] [minecraft/MinecraftServer]: .Bedrock_Guy left the game", ".Bedrock_Guy", false, true},
		{"[2026-11-19 13:00:00:123 INFO] Player connected: Ae Cyrexus, xuid: 2535411", "Ae Cyrexus", true, true},
		{"[2026-11-19 13:00:00:123 INFO] Player disconnected: Bob, xuid: 2535411, pfid: x", "Bob", false, true},
		{"[12:00:00 INFO]: <Steve> my friend joined the game", "", false, false},
		{"Player Spawned: Bob xuid: 1", "", false, false},
	}
	for _, c := range cases {
		n, j, ok := ParseJoinLeave(c.line)
		if n != c.name || j != c.joined || ok != c.ok {
			t.Errorf("%q → %q %v %v", c.line, n, j, ok)
		}
	}
}
