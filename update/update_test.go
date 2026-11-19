package update

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v1.4.0", "1.3.0", true}, {"1.3.0", "1.3.0", false}, {"1.2.10", "1.2.9", true},
		{"v2.0.0", "1.99.99", true}, {"1.3.0", "1.4.0", false}, {"v1.4.1-beta", "1.4.0", true},
	}
	for _, c := range cases {
		if Newer(c.a, c.b) != c.want {
			t.Errorf("Newer(%s,%s)", c.a, c.b)
		}
	}
}

func TestEndToEnd(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	exe := []byte(strings.Repeat("MZ fake exe ", 5000))
	sum := sha256.Sum256(exe)
	good := Manifest{Version: "1.4.0", File: ExeName, Size: int64(len(exe)), SHA256: hex.EncodeToString(sum[:])}
	Sign(priv, &good)
	manifest := good
	payload := exe

	mux := http.NewServeMux()
	var srvURL string
	mux.HandleFunc("/repos/"+Repo+"/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			w.WriteHeader(403)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v1.4.0", "body": "notes", "html_url": "x",
			"assets": []map[string]any{
				{"name": ManifestName, "browser_download_url": srvURL + "/dl/m", "size": 1},
				{"name": ExeName, "browser_download_url": srvURL + "/dl/exe", "size": len(exe)},
			},
		})
	})
	mux.HandleFunc("/dl/m", func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(manifest) })
	mux.HandleFunc("/dl/exe", func(w http.ResponseWriter, r *http.Request) { w.Write(payload) })
	s := httptest.NewServer(mux)
	defer s.Close()
	srvURL = s.URL
	APIBase = s.URL
	ctx := context.Background()

	rel, err := Latest(ctx, "test")
	if err != nil || rel.Version != "1.4.0" || !Newer(rel.Tag, "1.3.0") {
		t.Fatal(err, rel)
	}
	m, err := rel.FetchManifest(ctx, "test", pub)
	if err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(t.TempDir(), "new.exe")
	if err := rel.Download(ctx, "test", m, dst, nil); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(dst); string(b) != string(exe) {
		t.Fatal("content")
	}

	// tampered exe → hash mismatch, file removed
	payload = append([]byte("X"), exe[1:]...)
	if err := rel.Download(ctx, "test", m, dst, nil); err == nil || !strings.Contains(err.Error(), "hash") {
		t.Fatal("tamper not detected", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatal("bad file not removed")
	}
	// tampered manifest → signature invalid
	manifest.SHA256 = strings.Repeat("0", 64)
	if _, err := rel.FetchManifest(ctx, "test", pub); err == nil || !strings.Contains(err.Error(), "INVALID") {
		t.Fatal("manifest tamper not detected", err)
	}
	// wrong key
	manifest = good
	other, _, _ := ed25519.GenerateKey(nil)
	if _, err := rel.FetchManifest(ctx, "test", other); err == nil {
		t.Fatal("wrong key accepted")
	}
	// version mismatch (replay of an old signed manifest under a new tag)
	old := Manifest{Version: "1.0.0", File: ExeName, Size: good.Size, SHA256: good.SHA256}
	Sign(priv, &old)
	manifest = old
	if _, err := rel.FetchManifest(ctx, "test", pub); err == nil || !strings.Contains(err.Error(), "match") {
		t.Fatal("version replay not detected", err)
	}
}
