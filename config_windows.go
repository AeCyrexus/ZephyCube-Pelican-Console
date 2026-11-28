//go:build windows

package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

type QuickCmd struct {
	Group   string `json:"group,omitempty"`
	Name    string `json:"name"`
	Command string `json:"command"` // one command per line; placeholders {player} {me} {input} {server}
}

type Config struct {
	PanelURL      string            `json:"panel_url"`
	APIKeyDPAPI   string            `json:"api_key_dpapi"` // encrypted with Windows DPAPI (current user)
	IGN           string            `json:"ign"`
	AdminAll      bool              `json:"admin_all"`
	LastServer    string            `json:"last_server"`
	LightMode     bool              `json:"light_mode"`
	ListHidden    bool              `json:"list_hidden"`
	HiddenServers []string          `json:"hidden_servers"`
	EggFilter     string            `json:"egg_filter,omitempty"` // server list: show only this egg ("" = all)
	Edition       map[string]string `json:"edition,omitempty"`    // server id → java|bedrock (manual override)
	QuickVersion  int               `json:"quick_version"`
	NoUpdateCheck bool              `json:"no_update_check"`
	QuickCommands []QuickCmd        `json:"quick_commands"`
	apiKey        string
}

var defaultQuick = []QuickCmd{
	{"Time", "Day", "time set day"},
	{"Time", "Night", "time set night"},
	{"Time", "Noon", "time set noon"},
	{"Weather", "Clear", "weather clear"},
	{"Weather", "Rain", "weather rain"},
	{"Weather", "Thunder", "weather thunder"},
}

// v1.0–1.2 defaults, removed/replaced in quick-command version 2.
var legacyQuick = map[string]bool{
	"time set day": true, "time set night": true, "weather clear": true, "list": true,
	"say {input}": true, "kick {player}": true, "op {player}": true, "clear {player}": true,
}

const quickVersion = 2

func (c *Config) migrateQuick() {
	if c.QuickVersion >= quickVersion {
		return
	}
	var keep []QuickCmd
	for _, q := range c.QuickCommands {
		if q.Group == "" && legacyQuick[q.Command] {
			continue
		}
		keep = append(keep, q)
	}
	c.QuickCommands = append(append([]QuickCmd(nil), defaultQuick...), keep...)
	c.QuickVersion = quickVersion
	_ = c.Save()
}

func configPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = "."
	}
	return filepath.Join(dir, "ZephyCube", "PelicanConsole", "config.json")
}

func loadConfig() *Config {
	c := &Config{}
	b, err := os.ReadFile(configPath())
	if err != nil {
		c.QuickCommands = append([]QuickCmd(nil), defaultQuick...)
		c.QuickVersion = quickVersion
		return c
	}
	_ = json.Unmarshal(b, c)
	defer c.migrateQuick()
	if c.APIKeyDPAPI != "" {
		if raw, err := base64.StdEncoding.DecodeString(c.APIKeyDPAPI); err == nil {
			if plain, err := dpapiDecrypt(raw); err == nil {
				c.apiKey = string(plain)
			}
		}
	}
	return c
}

func (c *Config) Save() error {
	if c.apiKey != "" {
		if enc, err := dpapiEncrypt([]byte(c.apiKey)); err == nil {
			c.APIKeyDPAPI = base64.StdEncoding.EncodeToString(enc)
		}
	} else {
		c.APIKeyDPAPI = ""
	}
	p := configPath()
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(c, "", "  ")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// ---- DPAPI ----

var (
	crypt32             = syscall.NewLazyDLL("crypt32.dll")
	pCryptProtectData   = crypt32.NewProc("CryptProtectData")
	pCryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
	pLocalFree          = kernel32.NewProc("LocalFree")
)

type dataBlob struct {
	cb uint32
	pb *byte
}

func blob(b []byte) *dataBlob {
	if len(b) == 0 {
		return &dataBlob{}
	}
	return &dataBlob{cb: uint32(len(b)), pb: &b[0]}
}

func (d *dataBlob) bytes() []byte {
	if d.pb == nil || d.cb == 0 {
		return nil
	}
	out := make([]byte, d.cb)
	copy(out, unsafe.Slice(d.pb, d.cb))
	return out
}

var dpapiEntropy = []byte("ZephyCube.PelicanConsole.v1")

func dpapiEncrypt(data []byte) ([]byte, error) {
	in, ent := blob(data), blob(dpapiEntropy)
	var out dataBlob
	desc := u16("PelicanConsole API key")
	r, _, err := pCryptProtectData.Call(uintptr(unsafe.Pointer(in)), uintptr(unsafe.Pointer(desc)),
		uintptr(unsafe.Pointer(ent)), 0, 0, 0x1 /*CRYPTPROTECT_UI_FORBIDDEN*/, uintptr(unsafe.Pointer(&out)))
	if r == 0 {
		return nil, err
	}
	defer pLocalFree.Call(uintptr(unsafe.Pointer(out.pb)))
	return out.bytes(), nil
}

func dpapiDecrypt(data []byte) ([]byte, error) {
	in, ent := blob(data), blob(dpapiEntropy)
	var out dataBlob
	r, _, err := pCryptUnprotectData.Call(uintptr(unsafe.Pointer(in)), 0,
		uintptr(unsafe.Pointer(ent)), 0, 0, 0x1, uintptr(unsafe.Pointer(&out)))
	if r == 0 {
		return nil, err
	}
	defer pLocalFree.Call(uintptr(unsafe.Pointer(out.pb)))
	return out.bytes(), nil
}
