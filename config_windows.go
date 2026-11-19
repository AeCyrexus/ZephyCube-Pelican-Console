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
	Name    string `json:"name"`
	Command string `json:"command"` // one command per line; placeholders {player} {me} {input} {server}
}

type Config struct {
	PanelURL      string     `json:"panel_url"`
	APIKeyDPAPI   string     `json:"api_key_dpapi"` // encrypted with Windows DPAPI (current user)
	IGN           string     `json:"ign"`
	AdminAll      bool       `json:"admin_all"`
	LastServer    string     `json:"last_server"`
	QuickCommands []QuickCmd `json:"quick_commands"`
	apiKey        string
}

var defaultQuick = []QuickCmd{
	{"Day", "time set day"},
	{"Night", "time set night"},
	{"Clear weather", "weather clear"},
	{"List players", "list"},
	{"Say…", "say {input}"},
	{"Kick player", "kick {player}"},
	{"OP player", "op {player}"},
	{"Clear inventory", "clear {player}"},
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
		return c
	}
	_ = json.Unmarshal(b, c)
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
	crypt32              = syscall.NewLazyDLL("crypt32.dll")
	pCryptProtectData    = crypt32.NewProc("CryptProtectData")
	pCryptUnprotectData  = crypt32.NewProc("CryptUnprotectData")
	pLocalFree           = kernel32.NewProc("LocalFree")
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
