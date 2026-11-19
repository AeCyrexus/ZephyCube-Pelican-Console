package pelican

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Stats is the payload of the Wings "stats" websocket event.
type Stats struct {
	MemoryBytes      int64   `json:"memory_bytes"`
	MemoryLimitBytes int64   `json:"memory_limit_bytes"`
	CPUAbsolute      float64 `json:"cpu_absolute"`
	DiskBytes        int64   `json:"disk_bytes"`
	State            string  `json:"state"`
	Uptime           int64   `json:"uptime"`
	Network          struct {
		RxBytes int64 `json:"rx_bytes"`
		TxBytes int64 `json:"tx_bytes"`
	} `json:"network"`
}

// ConsoleHandlers are invoked from the console goroutine.
type ConsoleHandlers struct {
	OnLine   func(line string, kind LineKind) // console / daemon / install output, ANSI stripped
	OnStatus func(state string)               // running, offline, starting, stopping
	OnStats  func(Stats)
	OnConn   func(connected bool, err error)   // connection state changes
	OnEvent  func(event string, args []string) // anything else (backup completed, etc.)
}

type LineKind int

const (
	LineConsole LineKind = iota
	LineDaemon
	LineInstall
	LineError
)

type Console struct {
	c      *Client
	id     string
	h      ConsoleHandlers
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	conn   *wsConn
	done   chan struct{}
}

// OpenConsole connects to the server console and keeps reconnecting until Close.
func (c *Client) OpenConsole(id string, h ConsoleHandlers) *Console {
	ctx, cancel := context.WithCancel(context.Background())
	con := &Console{c: c, id: id, h: h, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	go con.loop()
	return con
}

func (con *Console) Close() {
	con.cancel()
	con.mu.Lock()
	if con.conn != nil {
		con.conn.Close()
	}
	con.mu.Unlock()
}

func (con *Console) Connected() bool {
	con.mu.Lock()
	defer con.mu.Unlock()
	return con.conn != nil
}

// Send sends a raw websocket event (e.g. "send command", "set state").
func (con *Console) Send(event string, args ...any) error {
	con.mu.Lock()
	w := con.conn
	con.mu.Unlock()
	if w == nil {
		return errors.New("console not connected")
	}
	if args == nil {
		args = []any{}
	}
	b, _ := json.Marshal(map[string]any{"event": event, "args": args})
	return w.WriteText(b)
}

func (con *Console) loop() {
	defer close(con.done)
	backoff := 2 * time.Second
	for con.ctx.Err() == nil {
		start := time.Now()
		err := con.runOnce()
		con.mu.Lock()
		con.conn = nil
		con.mu.Unlock()
		if con.ctx.Err() != nil {
			return
		}
		if con.h.OnConn != nil {
			con.h.OnConn(false, err)
		}
		if time.Since(start) > 30*time.Second {
			backoff = 2 * time.Second
		}
		select {
		case <-con.ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (con *Console) auth(w *wsConn) error {
	ctx, cancel := context.WithTimeout(con.ctx, 20*time.Second)
	defer cancel()
	token, _, err := con.c.WebsocketCreds(ctx, con.id)
	if err != nil {
		return err
	}
	b, _ := json.Marshal(map[string]any{"event": "auth", "args": []string{token}})
	return w.WriteText(b)
}

func (con *Console) runOnce() error {
	ctx, cancel := context.WithTimeout(con.ctx, 20*time.Second)
	token, socket, err := con.c.WebsocketCreds(ctx, con.id)
	cancel()
	if err != nil {
		return err
	}
	hdr := http.Header{}
	hdr.Set("Origin", con.c.Origin())
	hdr.Set("User-Agent", UserAgent)
	dctx, dcancel := context.WithTimeout(con.ctx, 20*time.Second)
	w, err := dialWS(dctx, socket, hdr)
	dcancel()
	if err != nil {
		return err
	}
	defer w.Close()

	con.mu.Lock()
	if con.ctx.Err() != nil {
		con.mu.Unlock()
		return nil
	}
	con.conn = w
	con.mu.Unlock()

	b, _ := json.Marshal(map[string]any{"event": "auth", "args": []string{token}})
	if err := w.WriteText(b); err != nil {
		return err
	}

	// keepalive pings
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		t := time.NewTicker(25 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if w.Ping() != nil {
					w.Close()
					return
				}
			}
		}
	}()

	for {
		data, err := w.ReadMessage(90 * time.Second)
		if err != nil {
			return err
		}
		var m struct {
			Event string            `json:"event"`
			Args  []json.RawMessage `json:"args"`
		}
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		args := make([]string, 0, len(m.Args))
		for _, a := range m.Args {
			var s string
			if json.Unmarshal(a, &s) == nil {
				args = append(args, s)
			} else {
				args = append(args, string(a))
			}
		}
		arg0 := ""
		if len(args) > 0 {
			arg0 = args[0]
		}
		switch m.Event {
		case "auth success":
			if con.h.OnConn != nil {
				con.h.OnConn(true, nil)
			}
			_ = con.Send("send logs", nil)
			_ = con.Send("send stats", nil)
		case "console output":
			con.emitLines(arg0, LineConsole)
		case "daemon message":
			con.emitLines(arg0, LineDaemon)
		case "install output":
			con.emitLines(arg0, LineInstall)
		case "daemon error":
			con.emitLines(arg0, LineError)
		case "status":
			if con.h.OnStatus != nil {
				con.h.OnStatus(arg0)
			}
		case "stats":
			var s Stats
			if json.Unmarshal([]byte(arg0), &s) == nil {
				if con.h.OnStats != nil {
					con.h.OnStats(s)
				}
				if s.State != "" && con.h.OnStatus != nil {
					con.h.OnStatus(s.State)
				}
			}
		case "token expiring":
			go func() {
				if err := con.auth(w); err != nil {
					con.emitLines("token refresh failed: "+err.Error(), LineError)
				}
			}()
		case "token expired":
			return errors.New("websocket token expired")
		case "jwt error":
			return fmt.Errorf("websocket auth error: %s", arg0)
		default:
			if con.h.OnEvent != nil {
				con.h.OnEvent(m.Event, args)
			}
		}
	}
}

func (con *Console) emitLines(s string, k LineKind) {
	if con.h.OnLine == nil {
		return
	}
	s = StripANSI(s)
	for _, ln := range strings.Split(s, "\n") {
		ln = strings.TrimRight(ln, "\r")
		con.h.OnLine(ln, k)
	}
}
