//go:build windows

package main

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"zephycube/pelicanconsole/pelican"
	"zephycube/pelicanconsole/update"
)

//go:embed update_pubkey.txt
var updatePubKey string

var (
	shell32             = syscall.NewLazyDLL("shell32.dll")
	shell32ShellExecute = shell32.NewProc("ShellExecuteW")
)

func openURL(u string) {
	p := shell32ShellExecute
	verb, file := u16("open"), u16(u)
	p.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(file)), 0, 0, 1)
}

const releasesURL = "https://github.com/" + update.Repo + "/releases/latest"

func exePath() string {
	p, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	return p
}

// cleanupOldExe removes the previous binary left behind by an update.
func cleanupOldExe() {
	exe := exePath()
	if exe == "" {
		return
	}
	go func() {
		for i := 0; i < 20; i++ {
			if err := os.Remove(exe + ".old"); err == nil || os.IsNotExist(err) {
				return
			}
			time.Sleep(500 * time.Millisecond)
		}
	}()
	os.Remove(exe + ".new")
}

func updatedFromArg() string {
	for _, a := range os.Args[1:] {
		if strings.HasPrefix(a, "--updated=") {
			return strings.TrimPrefix(a, "--updated=")
		}
	}
	return ""
}

// checkUpdates queries GitHub; manual=true always reports the result.
func (a *App) checkUpdates(manual bool) {
	if appVersion == "dev" && !manual {
		return
	}
	pub, err := update.ParsePublicKey(updatePubKey)
	if err != nil {
		if manual {
			a.feedback("✖ Updates: "+err.Error(), fbErr)
		}
		return
	}
	if manual {
		a.feedback("… checking for updates", fbBusy)
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		rel, err := update.Latest(ctx, pelican.UserAgent)
		var m *update.Manifest
		if err == nil && update.Newer(rel.Tag, appVersion) {
			m, err = rel.FetchManifest(ctx, pelican.UserAgent, pub)
		}
		Post(func() {
			if err != nil {
				if manual {
					a.feedback("✖ Update check: "+err.Error(), fbErr)
				}
				return
			}
			if m == nil {
				if manual {
					a.feedback("✔ You're on the latest version (v"+appVersion+")", fbOK)
				}
				return
			}
			a.pendingRel, a.pendingMan = rel, m
			SetText(a.bUpdate, "Update to v"+m.Version)
			Show(a.bUpdate, SW_SHOW)
			a.relayout()
			if manual {
				a.installUpdate()
			} else {
				a.feedback("⬆ v"+m.Version+" is available — click “Update to v"+m.Version+"” in the header.", fbWarn)
			}
		})
	}()
}

func (a *App) installUpdate() {
	rel, m := a.pendingRel, a.pendingMan
	if rel == nil || m == nil {
		a.checkUpdates(true)
		return
	}
	notes := strings.TrimSpace(rel.Notes)
	if len(notes) > 900 {
		notes = notes[:900] + "…"
	}
	msg := fmt.Sprintf("Update from v%s to v%s?\r\n\r\n%s\r\n\r\nThe app will restart. The download is verified (SHA-256 + ed25519 signature) before install.",
		appVersion, m.Version, strings.ReplaceAll(notes, "\n", "\r\n"))
	if MsgBox(a.w.Hwnd, "Update available", msg, MB_YESNO|MB_ICONINFORMATION) != IDYES {
		return
	}
	exe := exePath()
	if exe == "" {
		a.feedback("✖ Can't locate the running exe.", fbErr)
		return
	}
	newPath := exe + ".new"
	Enable(a.bUpdate, false)
	a.feedback("… downloading v"+m.Version, fbBusy)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		last := time.Time{}
		err := rel.Download(ctx, pelican.UserAgent, m, newPath, func(done, total int64) {
			if time.Since(last) < 150*time.Millisecond {
				return
			}
			last = time.Now()
			if total <= 0 {
				total = m.Size
			}
			pct := done * 100 / max(total, 1)
			Post(func() { a.feedback(fmt.Sprintf("… downloading v%s  %d%%", m.Version, pct), fbBusy) })
		})
		Post(func() {
			Enable(a.bUpdate, true)
			if err != nil {
				a.feedback("✖ Update failed: "+err.Error(), fbErr)
				if os.IsPermission(err) || strings.Contains(strings.ToLower(err.Error()), "access is denied") {
					if MsgBox(a.w.Hwnd, "Update", "No write access to "+filepath.Dir(exe)+".\r\nMove the exe to a folder you own (e.g. Desktop or Documents), or open the download page?",
						MB_YESNO|MB_ICONWARNING) == IDYES {
						openURL(releasesURL)
					}
				}
				return
			}
			a.swapAndRestart(exe, newPath, m.Version)
		})
	}()
}

func (a *App) swapAndRestart(exe, newPath, ver string) {
	old := exe + ".old"
	os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		os.Remove(newPath)
		a.feedback("✖ Update failed (rename current exe): "+err.Error(), fbErr)
		return
	}
	if err := os.Rename(newPath, exe); err != nil {
		os.Rename(old, exe) // roll back
		a.feedback("✖ Update failed (install new exe): "+err.Error(), fbErr)
		return
	}
	cmd := exec.Command(exe, "--updated="+appVersion)
	cmd.Dir = filepath.Dir(exe)
	if err := cmd.Start(); err != nil {
		MsgBox(a.w.Hwnd, "Update", "v"+ver+" installed, but restarting failed: "+err.Error()+"\r\nPlease start the app again.", MB_ICONWARNING)
	}
	PostMessage(a.w.Hwnd, WM_CLOSE, 0, 0)
}

func (a *App) helpMenu() {
	menu, _, _ := pCreatePopupMenu.Call()
	defer pDestroyMenu.Call(menu)
	appendMenu(menu, MF_STRING, idHelpUpdate, "Check for updates")
	appendMenu(menu, MF_STRING, idHelpReleases, "Release notes on GitHub")
	appendMenu(menu, MF_SEPARATOR, 0, "")
	appendMenu(menu, MF_STRING, idHelpKeys, "Shortcuts && placeholders")
	appendMenu(menu, MF_STRING, idHelpAbout, "About")
	r := WindowRect(a.bHelp)
	cmd, _, _ := pTrackPopupMenu.Call(menu, TPM_RETURNCMD|TPM_RIGHTBUTTON|0x8 /*TPM_RIGHTALIGN*/, uintptr(r.Right), uintptr(r.Bottom), 0, a.w.Hwnd, 0)
	switch int(cmd) {
	case idHelpUpdate:
		a.checkUpdates(true)
	case idHelpReleases:
		openURL("https://github.com/" + update.Repo + "/releases")
	case idHelpKeys:
		a.showShortcuts()
	case idHelpAbout:
		MsgBox(a.w.Hwnd, "About", fmt.Sprintf("ZephyCube Pelican Console v%s\r\n\r\ngithub.com/%s\r\nMIT License — ZephyCube name and logo not included.\r\n\r\nNot affiliated with Mojang, Microsoft or the Pelican project.", appVersion, update.Repo), MB_ICONINFORMATION)
	}
}
