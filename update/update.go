// Package update checks GitHub Releases for a newer build and verifies it with ed25519.
package update

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	Repo         = "AeCyrexus/ZephyCube-Pelican-Console"
	ManifestName = "update.json"
	ExeName      = "PelicanConsole.exe"
)

// APIBase can be overridden in tests.
var APIBase = "https://api.github.com"

var httpClient = &http.Client{Timeout: 10 * time.Minute}

// Manifest is published as update.json next to the exe in every release.
type Manifest struct {
	Version string `json:"version"`
	File    string `json:"file"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
	Sig     string `json:"sig"` // base64 ed25519 signature over Message()
}

// Message is the exact byte string that gets signed (binds version, name, size and hash).
func (m Manifest) Message() []byte {
	return []byte(fmt.Sprintf("ZCPC-UPDATE\n%s\n%s\n%d\n%s", m.Version, m.File, m.Size, strings.ToLower(m.SHA256)))
}

func Sign(priv ed25519.PrivateKey, m *Manifest) {
	m.Sig = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, m.Message()))
}

func (m Manifest) Verify(pub ed25519.PublicKey) error {
	sig, err := base64.StdEncoding.DecodeString(m.Sig)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("update signature missing or malformed")
	}
	if !ed25519.Verify(pub, m.Message(), sig) {
		return errors.New("update signature is INVALID — refusing to install")
	}
	return nil
}

// ParsePublicKey reads a base64 ed25519 public key (whitespace ignored).
func ParsePublicKey(s string) (ed25519.PublicKey, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, errors.New("bad update public key")
	}
	return ed25519.PublicKey(b), nil
}

// Newer reports whether version a is strictly newer than b ("v1.2.3" or "1.2.3").
func Newer(a, b string) bool {
	pa, pb := parse(a), parse(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			return pa[i] > pb[i]
		}
	}
	return false
}

func parse(v string) [3]int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	var out [3]int
	for i, p := range strings.SplitN(v, ".", 3) {
		out[i], _ = strconv.Atoi(p)
	}
	return out
}

type Release struct {
	Tag     string
	Version string
	Notes   string
	HTMLURL string
	assets  map[string]asset
}

type asset struct {
	URL  string
	Size int64
}

// Latest fetches the latest published (non-draft, non-prerelease) release.
func Latest(ctx context.Context, userAgent string) (*Release, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", APIBase+"/repos/"+Repo+"/releases/latest", nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, errors.New("no releases published yet")
	}
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return nil, fmt.Errorf("GitHub API HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var r struct {
		TagName string `json:"tag_name"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
			Size int64  `json:"size"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	rel := &Release{Tag: r.TagName, Version: strings.TrimPrefix(r.TagName, "v"), Notes: r.Body, HTMLURL: r.HTMLURL, assets: map[string]asset{}}
	for _, a := range r.Assets {
		rel.assets[a.Name] = asset{a.URL, a.Size}
	}
	return rel, nil
}

func get(ctx context.Context, url, ua string, w io.Writer, max int64, progress func(done, total int64)) error {
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "application/octet-stream")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download HTTP %d", resp.StatusCode)
	}
	total := resp.ContentLength
	var done int64
	buf := make([]byte, 64<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			done += int64(n)
			if done > max {
				return errors.New("download larger than expected")
			}
			if _, err := w.Write(buf[:n]); err != nil {
				return err
			}
			if progress != nil {
				progress(done, total)
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}

// FetchManifest downloads and verifies update.json for a release.
func (r *Release) FetchManifest(ctx context.Context, ua string, pub ed25519.PublicKey) (*Manifest, error) {
	a, ok := r.assets[ManifestName]
	if !ok {
		return nil, errors.New("release has no " + ManifestName)
	}
	var sb strings.Builder
	if err := get(ctx, a.URL, ua, &sb, 64<<10, nil); err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal([]byte(sb.String()), &m); err != nil {
		return nil, fmt.Errorf("bad %s: %w", ManifestName, err)
	}
	if err := m.Verify(pub); err != nil {
		return nil, err
	}
	if strings.TrimPrefix(m.Version, "v") != r.Version {
		return nil, fmt.Errorf("manifest version %s doesn't match release %s", m.Version, r.Tag)
	}
	if _, ok := r.assets[m.File]; !ok {
		return nil, errors.New("release has no " + m.File)
	}
	return &m, nil
}

// Download saves the verified exe to dst (sha256 + size checked). dst is removed on failure.
func (r *Release) Download(ctx context.Context, ua string, m *Manifest, dst string, progress func(done, total int64)) (err error) {
	a := r.assets[m.File]
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}
	defer func() {
		f.Close()
		if err != nil {
			os.Remove(dst)
		}
	}()
	h := sha256.New()
	if err = get(ctx, a.URL, ua, io.MultiWriter(f, h), m.Size+1, progress); err != nil {
		return err
	}
	st, _ := f.Stat()
	if st == nil || st.Size() != m.Size {
		return errors.New("downloaded size doesn't match the signed manifest")
	}
	if !strings.EqualFold(hex.EncodeToString(h.Sum(nil)), m.SHA256) {
		return errors.New("downloaded file hash doesn't match the signed manifest")
	}
	return f.Sync()
}
