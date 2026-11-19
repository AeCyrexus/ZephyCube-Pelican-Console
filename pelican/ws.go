package pelican

// Minimal RFC 6455 websocket client (text frames, ping/pong, close). Stdlib only.

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

const (
	opCont  = 0x0
	opText  = 0x1
	opBin   = 0x2
	opClose = 0x8
	opPing  = 0x9
	opPong  = 0xA
)

var ErrWSClosed = errors.New("websocket closed")

type wsConn struct {
	c   net.Conn
	br  *bufio.Reader
	wmu sync.Mutex
}

func dialWS(ctx context.Context, rawURL string, header http.Header) (*wsConn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	secure := false
	switch strings.ToLower(u.Scheme) {
	case "wss", "https":
		secure = true
	case "ws", "http":
	default:
		return nil, fmt.Errorf("unsupported websocket scheme %q", u.Scheme)
	}
	host := u.Hostname()
	port := u.Port()
	if port == "" {
		if secure {
			port = "443"
		} else {
			port = "80"
		}
	}
	d := &net.Dialer{Timeout: 15 * time.Second, KeepAlive: 30 * time.Second}
	raw, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return nil, err
	}
	var conn net.Conn = raw
	if secure {
		tc := tls.Client(raw, &tls.Config{ServerName: host, NextProtos: []string{"http/1.1"}})
		hctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err = tc.HandshakeContext(hctx)
		cancel()
		if err != nil {
			raw.Close()
			return nil, err
		}
		conn = tc
	}

	kb := make([]byte, 16)
	_, _ = rand.Read(kb)
	key := base64.StdEncoding.EncodeToString(kb)

	var sb strings.Builder
	sb.WriteString("GET " + u.RequestURI() + " HTTP/1.1\r\n")
	sb.WriteString("Host: " + u.Host + "\r\n")
	sb.WriteString("Upgrade: websocket\r\nConnection: Upgrade\r\n")
	sb.WriteString("Sec-WebSocket-Key: " + key + "\r\n")
	sb.WriteString("Sec-WebSocket-Version: 13\r\n")
	for k, vs := range header {
		for _, v := range vs {
			sb.WriteString(k + ": " + v + "\r\n")
		}
	}
	sb.WriteString("\r\n")

	_ = conn.SetDeadline(time.Now().Add(15 * time.Second))
	if _, err := io.WriteString(conn, sb.String()); err != nil {
		conn.Close()
		return nil, err
	}
	br := bufio.NewReaderSize(conn, 64<<10)
	resp, err := http.ReadResponse(br, &http.Request{Method: "GET"})
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("websocket handshake: %w", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		conn.Close()
		msg := strings.TrimSpace(string(b))
		if resp.StatusCode == 403 {
			msg += " (Wings rejected the Origin — panel URL in Settings must match the panel's configured URL)"
		}
		return nil, fmt.Errorf("websocket handshake: HTTP %d %s", resp.StatusCode, msg)
	}
	h := sha1.Sum([]byte(key + wsGUID))
	if resp.Header.Get("Sec-WebSocket-Accept") != base64.StdEncoding.EncodeToString(h[:]) {
		conn.Close()
		return nil, errors.New("websocket handshake: bad Sec-WebSocket-Accept")
	}
	_ = conn.SetDeadline(time.Time{})
	return &wsConn{c: conn, br: br}, nil
}

func (w *wsConn) Close() error { return w.c.Close() }

func (w *wsConn) writeFrame(op byte, payload []byte) error {
	w.wmu.Lock()
	defer w.wmu.Unlock()
	hdr := make([]byte, 0, 14)
	hdr = append(hdr, 0x80|op)
	n := len(payload)
	switch {
	case n < 126:
		hdr = append(hdr, 0x80|byte(n))
	case n <= 0xFFFF:
		hdr = append(hdr, 0x80|126, byte(n>>8), byte(n))
	default:
		hdr = append(hdr, 0x80|127)
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], uint64(n))
		hdr = append(hdr, b[:]...)
	}
	var mask [4]byte
	_, _ = rand.Read(mask[:])
	hdr = append(hdr, mask[:]...)
	buf := make([]byte, len(hdr)+n)
	copy(buf, hdr)
	for i := 0; i < n; i++ {
		buf[len(hdr)+i] = payload[i] ^ mask[i&3]
	}
	_ = w.c.SetWriteDeadline(time.Now().Add(15 * time.Second))
	_, err := w.c.Write(buf)
	return err
}

func (w *wsConn) WriteText(b []byte) error { return w.writeFrame(opText, b) }
func (w *wsConn) Ping() error             { return w.writeFrame(opPing, nil) }

// ReadMessage returns the next complete text/binary message, answering pings transparently.
// readTimeout is applied per frame (0 = none).
func (w *wsConn) ReadMessage(readTimeout time.Duration) ([]byte, error) {
	var msg []byte
	started := false
	for {
		if readTimeout > 0 {
			_ = w.c.SetReadDeadline(time.Now().Add(readTimeout))
		}
		var h [2]byte
		if _, err := io.ReadFull(w.br, h[:]); err != nil {
			return nil, err
		}
		fin := h[0]&0x80 != 0
		op := h[0] & 0x0F
		masked := h[1]&0x80 != 0
		n := uint64(h[1] & 0x7F)
		switch n {
		case 126:
			var b [2]byte
			if _, err := io.ReadFull(w.br, b[:]); err != nil {
				return nil, err
			}
			n = uint64(binary.BigEndian.Uint16(b[:]))
		case 127:
			var b [8]byte
			if _, err := io.ReadFull(w.br, b[:]); err != nil {
				return nil, err
			}
			n = binary.BigEndian.Uint64(b[:])
		}
		if n > 32<<20 {
			return nil, errors.New("websocket frame too large")
		}
		var mask [4]byte
		if masked {
			if _, err := io.ReadFull(w.br, mask[:]); err != nil {
				return nil, err
			}
		}
		payload := make([]byte, n)
		if _, err := io.ReadFull(w.br, payload); err != nil {
			return nil, err
		}
		if masked {
			for i := range payload {
				payload[i] ^= mask[i&3]
			}
		}
		switch op {
		case opPing:
			_ = w.writeFrame(opPong, payload)
			continue
		case opPong:
			continue
		case opClose:
			_ = w.writeFrame(opClose, payload)
			reason := ""
			if len(payload) >= 2 {
				reason = fmt.Sprintf(" (code %d %s)", binary.BigEndian.Uint16(payload[:2]), string(payload[2:]))
			}
			return nil, fmt.Errorf("%w%s", ErrWSClosed, reason)
		case opText, opBin:
			msg = append(msg[:0], payload...)
			started = true
		case opCont:
			if !started {
				return nil, errors.New("websocket: unexpected continuation frame")
			}
			msg = append(msg, payload...)
		default:
			return nil, fmt.Errorf("websocket: unknown opcode %d", op)
		}
		if fin && started {
			return msg, nil
		}
	}
}
