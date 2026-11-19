package pelican

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// PlayerRecord is what is remembered about one player on one server.
type PlayerRecord struct {
	Name        string    `json:"name"`
	XUID        string    `json:"xuid,omitempty"`
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
	Sessions    int       `json:"sessions"`
	TotalSec    int64     `json:"total_sec"`
	LastSessSec int64     `json:"last_session_sec"`

	Online bool      `json:"-"`
	Since  time.Time `json:"-"` // start of the current session
	Approx bool      `json:"-"` // Since is when we noticed, not the real join time
}

// SessionSec is the current session length (0 when offline).
func (r *PlayerRecord) SessionSec(now time.Time) int64 {
	if !r.Online || r.Since.IsZero() {
		return 0
	}
	return int64(now.Sub(r.Since).Seconds())
}

// History is the per-server player log, keyed by lower-case name (XUID-aware).
type History struct {
	Players map[string]*PlayerRecord `json:"players"`
}

func (h *History) find(name, xuid string) *PlayerRecord {
	if h.Players == nil {
		h.Players = map[string]*PlayerRecord{}
	}
	if xuid != "" {
		for k, r := range h.Players {
			if r.XUID == xuid {
				if !strings.EqualFold(r.Name, name) && name != "" { // gamertag changed
					delete(h.Players, k)
					r.Name = name
					h.Players[strings.ToLower(name)] = r
				}
				return r
			}
		}
	}
	return h.Players[strings.ToLower(name)]
}

// Get returns the record for name, or nil.
func (h *History) Get(name string) *PlayerRecord { return h.find(name, "") }

// Join records a player entering the world. approx means the real join time is
// unknown (player was already online when we started watching).
func (h *History) Join(name, xuid string, t time.Time, approx bool) *PlayerRecord {
	r := h.find(name, xuid)
	if r == nil {
		r = &PlayerRecord{Name: name, FirstSeen: t}
		h.Players[strings.ToLower(name)] = r
	}
	if xuid != "" {
		r.XUID = xuid
	}
	r.LastSeen = t
	if !r.Online {
		r.Online, r.Since, r.Approx = true, t, approx
		if !approx {
			r.Sessions++
		}
	}
	return r
}

// Leave closes the player's session.
func (h *History) Leave(name, xuid string, t time.Time) *PlayerRecord {
	r := h.find(name, xuid)
	if r == nil {
		return nil
	}
	if xuid != "" {
		r.XUID = xuid
	}
	h.end(r, t)
	return r
}

func (h *History) end(r *PlayerRecord, t time.Time) {
	if r.Online {
		if d := r.SessionSec(t); d > 0 {
			r.TotalSec += d
			r.LastSessSec = d
		}
		r.Online = false
	}
	r.LastSeen = t
}

// EndAll closes every open session (server stopped / console lost).
func (h *History) EndAll(t time.Time) {
	for _, r := range h.Players {
		if r.Online {
			h.end(r, t)
		}
	}
}

// Touch updates LastSeen for everyone online (call before saving).
func (h *History) Touch(t time.Time) {
	for _, r := range h.Players {
		if r.Online {
			r.LastSeen = t
		}
	}
}

// Sorted returns online players first (by name), then the rest by last seen, newest first.
func (h *History) Sorted() []*PlayerRecord {
	out := make([]*PlayerRecord, 0, len(h.Players))
	for _, r := range h.Players {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Online != b.Online {
			return a.Online
		}
		if a.Online {
			return strings.ToLower(a.Name) < strings.ToLower(b.Name)
		}
		return a.LastSeen.After(b.LastSeen)
	})
	return out
}

// HumanDuration formats seconds as "2h 05m" / "5m" / "40s".
func HumanDuration(sec int64) string {
	switch {
	case sec <= 0:
		return "—"
	case sec < 60:
		return fmt.Sprintf("%ds", sec)
	case sec < 3600:
		return fmt.Sprintf("%dm", sec/60)
	case sec < 86400:
		return fmt.Sprintf("%dh %02dm", sec/3600, sec/60%60)
	default:
		return fmt.Sprintf("%dd %dh", sec/86400, sec/3600%24)
	}
}
