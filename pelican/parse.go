package pelican

import (
	"fmt"
	"regexp"
	"strings"
)

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[@-Z\\-_]`)

func StripANSI(s string) string {
	s = ansiRe.ReplaceAllString(s, "")
	return strings.ReplaceAll(s, "§r", "") // stray MC reset codes
}

// ---- command feedback classification ----

var errPatterns = []string{
	"unknown command", "unknown or incomplete command", "syntax error", "incorrect argument",
	"no player was found", "no entity was found", "no targets matched", "player not found",
	"no player found", "that player does not exist", "could not", "cannot ", "can't ",
	"error", "exception", "invalid", "you do not have permission", "is not a valid",
	"failed", "not found", "unexpected", "expected whitespace", "too many arguments",
	"permission denied", "only players can", "must be a player",
}

var warnPatterns = []string{"warn", "[warning]"}

// IsErrorLine reports whether a console line looks like a command/server error.
func IsErrorLine(line string) bool {
	l := strings.ToLower(line)
	for _, p := range errPatterns {
		if strings.Contains(l, p) {
			return true
		}
	}
	return false
}

func IsWarnLine(line string) bool {
	l := strings.ToLower(line)
	for _, p := range warnPatterns {
		if strings.Contains(l, p) {
			return true
		}
	}
	return false
}

// ---- `list` output parsing (Java/Paper and Bedrock BDS) ----

var listRe = regexp.MustCompile(`(?i)there (?:are|is) (\d+)\s*(?:of a max of|/|out of maximum|out of a maximum of)\s*(\d+) players? online[.:]?\s*(.*)$`)
var prefixRe = regexp.MustCompile(`^(\s*\[[^\]]*\]\s*:?\s*)+`)
var bdsPrefixRe = regexp.MustCompile(`^\s*(NO LOG FILE! - |\d{4}-\d\d-\d\d[ T][\d:.,]+\s*(INFO|WARN)?\s*\]?\s*)`)

// PlayerListParser consumes console lines after a `list` command.
type PlayerListParser struct {
	expect  int
	waiting bool
	Players []string
	Done    bool
}

// CleanPrefix strips log prefixes like "[12:00:00 INFO]: " / BDS timestamps.
func CleanPrefix(s string) string { return cleanPrefix(s) }

func cleanPrefix(s string) string {
	s = prefixRe.ReplaceAllString(s, "")
	s = bdsPrefixRe.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

func splitNames(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		// Paper/Essentials may prefix with group "default: name"
		if i := strings.LastIndex(p, ": "); i >= 0 {
			p = strings.TrimSpace(p[i+2:])
		}
		p = strings.TrimPrefix(p, "~") // nick prefix
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Feed returns true once the player list is complete.
func (p *PlayerListParser) Feed(line string) bool {
	if p.Done {
		return true
	}
	if p.waiting {
		names := cleanPrefix(line)
		if names == "" {
			return false
		}
		p.Players = splitNames(names)
		p.Done = true
		return true
	}
	m := listRe.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	var n int
	fmt.Sscanf(m[1], "%d", &n)
	rest := strings.TrimSpace(m[3])
	if n == 0 {
		p.Done = true
		return true
	}
	if rest != "" {
		p.Players = splitNames(rest)
		p.Done = true
		return true
	}
	p.expect = n
	p.waiting = true // Bedrock prints names on the next line
	return false
}

// ---- formatting helpers ----

func HumanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

func HumanUptime(ms int64) string {
	s := ms / 1000
	d, s := s/86400, s%86400
	h, s := s/3600, s%3600
	m := s / 60
	switch {
	case d > 0:
		return fmt.Sprintf("%dd %dh", d, h)
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	default:
		return fmt.Sprintf("%dm", m)
	}
}

// ---- join / leave tracking ----

// PlayerEventKind classifies a player console line.
type PlayerEventKind int

const (
	PlayerConnected PlayerEventKind = iota // BDS "Player connected" (not yet in the world)
	PlayerJoined                           // Java "joined the game", BDS "Player Spawned"
	PlayerLeft                             // Java "left the game", BDS "Player disconnected"
)

// PlayerEvent is a parsed join/leave line. XUID is only set on Bedrock.
type PlayerEvent struct {
	Kind PlayerEventKind
	Name string
	XUID string
}

var javaJoinRe = regexp.MustCompile(`^([A-Za-z0-9_.*\-]{1,32}) (joined|left) the game\.?$`)
var bdsSpawnRe = regexp.MustCompile(`Player Spawned: (.+?),? xuid: ?(\d*)`)
var bdsConnRe = regexp.MustCompile(`Player (connected|disconnected): (.+?), xuid: ?(\d*)`)

// ParsePlayerEvent detects join/leave lines (Java/Paper and Bedrock BDS).
// On Bedrock the join is "Player Spawned" (the player is actually in the world);
// "Player connected" is reported as PlayerConnected and should not count as a join.
func ParsePlayerEvent(line string) (PlayerEvent, bool) {
	l := cleanPrefix(line)
	if m := bdsSpawnRe.FindStringSubmatch(l); m != nil {
		return PlayerEvent{PlayerJoined, strings.TrimSpace(m[1]), m[2]}, true
	}
	if m := bdsConnRe.FindStringSubmatch(l); m != nil {
		k := PlayerConnected
		if m[1] == "disconnected" {
			k = PlayerLeft
		}
		return PlayerEvent{k, strings.TrimSpace(m[2]), m[3]}, true
	}
	if m := javaJoinRe.FindStringSubmatch(l); m != nil {
		k := PlayerJoined
		if m[2] == "left" {
			k = PlayerLeft
		}
		return PlayerEvent{Kind: k, Name: m[1]}, true
	}
	return PlayerEvent{}, false
}
