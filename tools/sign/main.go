// sign — release signing for ZephyCube Pelican Console auto-updates.
//
//	go run ./tools/sign keygen                 # new keypair: writes update_pubkey.txt, prints the PRIVATE key once
//	go run ./tools/sign sign <version> <exe>   # writes update.json next to the exe (key from env ZC_SIGNING_KEY)
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"zephycube/pelicanconsole/update"
)

func die(f string, a ...any) {
	fmt.Fprintf(os.Stderr, f+"\n", a...)
	os.Exit(1)
}

func main() {
	if len(os.Args) < 2 {
		die("usage: sign keygen | sign sign <version> <exe>")
	}
	switch os.Args[1] {
	case "keygen":
		pub, priv, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			die("%v", err)
		}
		if err := os.WriteFile("update_pubkey.txt", []byte(base64.StdEncoding.EncodeToString(pub)+"\n"), 0644); err != nil {
			die("%v", err)
		}
		fmt.Println("Wrote update_pubkey.txt (commit this).")
		fmt.Println("PRIVATE key — add as GitHub secret ZC_SIGNING_KEY, never commit it:")
		fmt.Println(base64.StdEncoding.EncodeToString(priv))
	case "sign":
		if len(os.Args) != 4 {
			die("usage: sign sign <version> <exe>")
		}
		raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Getenv("ZC_SIGNING_KEY")))
		if err != nil || len(raw) != ed25519.PrivateKeySize {
			die("ZC_SIGNING_KEY missing or invalid")
		}
		priv := ed25519.PrivateKey(raw)
		pubTxt, err := os.ReadFile("update_pubkey.txt")
		if err != nil {
			die("read update_pubkey.txt: %v", err)
		}
		pub, err := update.ParsePublicKey(string(pubTxt))
		if err != nil || !pub.Equal(priv.Public()) {
			die("ZC_SIGNING_KEY does not match update_pubkey.txt — clients would reject this release")
		}
		exe := os.Args[3]
		b, err := os.ReadFile(exe)
		if err != nil {
			die("%v", err)
		}
		sum := sha256.Sum256(b)
		m := update.Manifest{
			Version: strings.TrimPrefix(os.Args[2], "v"),
			File:    filepath.Base(exe),
			Size:    int64(len(b)),
			SHA256:  hex.EncodeToString(sum[:]),
		}
		update.Sign(priv, &m)
		if err := m.Verify(pub); err != nil {
			die("self-verify failed: %v", err)
		}
		out, _ := json.MarshalIndent(m, "", "  ")
		dst := filepath.Join(filepath.Dir(exe), update.ManifestName)
		if err := os.WriteFile(dst, out, 0644); err != nil {
			die("%v", err)
		}
		fmt.Printf("Signed %s v%s (%d bytes, sha256 %s) → %s\n", m.File, m.Version, m.Size, m.SHA256, dst)
	default:
		die("unknown command %q", os.Args[1])
	}
}
