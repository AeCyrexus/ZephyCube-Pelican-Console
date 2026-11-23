// Package pelican is a small client for the Pelican (Pterodactyl-compatible)
// Client API and the Wings console websocket. Stdlib only.
package pelican

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const UserAgent = "ZephyCube-PelicanConsole/1.0"

type Client struct {
	BaseURL string // e.g. https://pelican.zephycube.com (no trailing slash)
	APIKey  string // client API key
	HTTP    *http.Client
}

func New(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL: NormalizeURL(baseURL),
		APIKey:  strings.TrimSpace(apiKey),
		HTTP:    &http.Client{Timeout: 30 * time.Second},
	}
}

// NormalizeURL trims whitespace/trailing slashes and adds https:// if missing.
func NormalizeURL(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimRight(s, "/")
	if s != "" && !strings.Contains(s, "://") {
		s = "https://" + s
	}
	return s
}

// Origin returns scheme://host of the panel URL (Wings checks the Origin header against it).
func (c *Client) Origin() string {
	u, err := url.Parse(c.BaseURL)
	if err != nil {
		return c.BaseURL
	}
	return u.Scheme + "://" + u.Host
}

type APIError struct {
	Status int
	Code   string
	Detail string
}

func (e *APIError) Error() string {
	d := e.Detail
	if d == "" {
		d = http.StatusText(e.Status)
	}
	if e.Code != "" {
		return fmt.Sprintf("HTTP %d %s: %s", e.Status, e.Code, d)
	}
	return fmt.Sprintf("HTTP %d: %s", e.Status, d)
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	if c.BaseURL == "" || c.APIKey == "" {
		return fmt.Errorf("panel URL / API key not configured")
	}
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", UserAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 400 {
		ae := &APIError{Status: resp.StatusCode}
		var e struct {
			Errors []struct {
				Code   string `json:"code"`
				Detail string `json:"detail"`
			} `json:"errors"`
			Message string `json:"message"`
		}
		if json.Unmarshal(data, &e) == nil {
			if len(e.Errors) > 0 {
				ae.Code, ae.Detail = e.Errors[0].Code, e.Errors[0].Detail
			} else if e.Message != "" {
				ae.Detail = e.Message
			}
		}
		if ae.Detail == "" && ae.Code == "" {
			snip := strings.TrimSpace(string(data))
			if len(snip) > 200 {
				snip = snip[:200] + "…"
			}
			if strings.HasPrefix(snip, "<") {
				snip = "" // HTML error page (Cloudflare etc.)
			}
			ae.Detail = snip
		}
		return ae
	}
	if out != nil && resp.StatusCode != http.StatusNoContent && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("decode %s: %w", path, err)
		}
	}
	return nil
}

// ---------- Types ----------

type Server struct {
	Identifier   string `json:"identifier"`
	UUID         string `json:"uuid"`
	Name         string `json:"name"`
	Node         string `json:"node"`
	Description  string `json:"description"`
	Invocation   string `json:"invocation"`
	DockerImage  string `json:"docker_image"`
	IsOwner      bool   `json:"server_owner"`
	Status       string `json:"status"` // install status: "", installing, suspended…
	IsSuspended  bool   `json:"is_suspended"`
	IsInstalling bool   `json:"is_installing"`
	Limits       struct {
		Memory int64 `json:"memory"`
		Disk   int64 `json:"disk"`
		CPU    int64 `json:"cpu"`
	} `json:"limits"`
	FeatureLimits struct {
		Backups int `json:"backups"`
	} `json:"feature_limits"`
	Relationships struct {
		Egg struct {
			Attributes struct {
				Name string `json:"name"`
			} `json:"attributes"`
		} `json:"egg"`
	} `json:"relationships"`
}

// EggName is the server's egg (from ?include=egg), or "" if the panel didn't send it.
func (s Server) EggName() string { return strings.TrimSpace(s.Relationships.Egg.Attributes.Name) }

type Resources struct {
	CurrentState string `json:"current_state"`
	IsSuspended  bool   `json:"is_suspended"`
	Resources    struct {
		MemoryBytes int64   `json:"memory_bytes"`
		CPUAbsolute float64 `json:"cpu_absolute"`
		DiskBytes   int64   `json:"disk_bytes"`
		RxBytes     int64   `json:"network_rx_bytes"`
		TxBytes     int64   `json:"network_tx_bytes"`
		Uptime      int64   `json:"uptime"`
	} `json:"resources"`
}

type Backup struct {
	UUID         string `json:"uuid"`
	Name         string `json:"name"`
	IsSuccessful bool   `json:"is_successful"`
	IsLocked     bool   `json:"is_locked"`
	Bytes        int64  `json:"bytes"`
	CreatedAt    string `json:"created_at"`
	CompletedAt  string `json:"completed_at"`
}

type Account struct {
	Username  string `json:"username"`
	Email     string `json:"email"`
	Admin     bool   `json:"admin"`
	RootAdmin bool   `json:"root_admin"`
}

// ---------- Endpoints ----------

func (c *Client) Account(ctx context.Context) (Account, error) {
	var r struct {
		Attributes Account `json:"attributes"`
	}
	err := c.do(ctx, "GET", "/api/client/account", nil, &r)
	return r.Attributes, err
}

// ListServers returns every server visible to the key (all pages).
// adminAll=true adds ?type=admin-all (admins only: every server on the panel).
func (c *Client) ListServers(ctx context.Context, adminAll bool) ([]Server, error) {
	var all []Server
	for page := 1; page <= 200; page++ {
		q := fmt.Sprintf("/api/client?page=%d&per_page=100&include=egg", page)
		if adminAll {
			q += "&type=admin-all"
		}
		var r struct {
			Data []struct {
				Attributes Server `json:"attributes"`
			} `json:"data"`
			Meta struct {
				Pagination struct {
					TotalPages int `json:"total_pages"`
				} `json:"pagination"`
			} `json:"meta"`
		}
		if err := c.do(ctx, "GET", q, nil, &r); err != nil {
			return nil, err
		}
		for _, d := range r.Data {
			all = append(all, d.Attributes)
		}
		if page >= r.Meta.Pagination.TotalPages {
			break
		}
	}
	return all, nil
}

func sp(id string) string { return "/api/client/servers/" + url.PathEscape(id) }

func (c *Client) Resources(ctx context.Context, id string) (Resources, error) {
	var r struct {
		Attributes Resources `json:"attributes"`
	}
	err := c.do(ctx, "GET", sp(id)+"/resources", nil, &r)
	return r.Attributes, err
}

func (c *Client) SendCommand(ctx context.Context, id, command string) error {
	return c.do(ctx, "POST", sp(id)+"/command", map[string]string{"command": command}, nil)
}

// Power sends start | stop | restart | kill.
func (c *Client) Power(ctx context.Context, id, signal string) error {
	return c.do(ctx, "POST", sp(id)+"/power", map[string]string{"signal": signal}, nil)
}

func (c *Client) CreateBackup(ctx context.Context, id, name string) (Backup, error) {
	body := map[string]any{}
	if strings.TrimSpace(name) != "" {
		body["name"] = name
	}
	var r struct {
		Attributes Backup `json:"attributes"`
	}
	err := c.do(ctx, "POST", sp(id)+"/backups", body, &r)
	return r.Attributes, err
}

func (c *Client) WebsocketCreds(ctx context.Context, id string) (token, socket string, err error) {
	var r struct {
		Data struct {
			Token  string `json:"token"`
			Socket string `json:"socket"`
		} `json:"data"`
	}
	if err = c.do(ctx, "GET", sp(id)+"/websocket", nil, &r); err != nil {
		return
	}
	if r.Data.Token == "" || r.Data.Socket == "" {
		err = fmt.Errorf("panel returned empty websocket credentials")
	}
	return r.Data.Token, r.Data.Socket, err
}

// ListBackups returns all backups of a server (all pages).
func (c *Client) ListBackups(ctx context.Context, id string) ([]Backup, error) {
	var all []Backup
	for page := 1; page <= 50; page++ {
		var r struct {
			Data []struct {
				Attributes Backup `json:"attributes"`
			} `json:"data"`
			Meta struct {
				Pagination struct {
					TotalPages int `json:"total_pages"`
				} `json:"pagination"`
			} `json:"meta"`
		}
		if err := c.do(ctx, "GET", fmt.Sprintf("%s/backups?page=%d&per_page=100", sp(id), page), nil, &r); err != nil {
			return nil, err
		}
		for _, d := range r.Data {
			all = append(all, d.Attributes)
		}
		if page >= r.Meta.Pagination.TotalPages {
			break
		}
	}
	return all, nil
}

func (c *Client) DeleteBackup(ctx context.Context, id, uuid string) error {
	return c.do(ctx, "DELETE", sp(id)+"/backups/"+url.PathEscape(uuid), nil, nil)
}

// OldestRemovable picks the oldest finished, unlocked backup (nil if none).
func OldestRemovable(bs []Backup) *Backup {
	var best *Backup
	for i := range bs {
		b := &bs[i]
		if b.IsLocked || b.CompletedAt == "" {
			continue
		}
		if best == nil || b.CreatedAt < best.CreatedAt {
			best = b
		}
	}
	return best
}

// GuessBedrock reports whether a server looks like Bedrock (BDS) from its startup/image.
func GuessBedrock(s Server) bool {
	h := strings.ToLower(s.EggName() + " " + s.Invocation + " " + s.DockerImage + " " + s.Name + " " + s.Description)
	return strings.Contains(h, "bedrock") || strings.Contains(h, "bedrock_server") || strings.Contains(h, " bds")
}
