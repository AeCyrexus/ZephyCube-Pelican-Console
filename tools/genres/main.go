// genres writes rsrc_windows_amd64.syso (COFF .rsrc: app icon, manifest, version info).
// Usage (from repo root): go run ./tools/genres 1.1.0
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

const (
	rtIcon      = 3
	rtGroupIcon = 14
	rtVersion   = 16
	rtManifest  = 24
	langEnUS    = 0x0409
)

var sizes = []int{16, 20, 24, 32, 40, 48, 64, 96, 128, 256}

const manifest = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <assemblyIdentity version="%s.0" processorArchitecture="amd64" name="ZephyCube.PelicanConsole" type="win32"/>
  <description>ZephyCube Pelican Console</description>
  <dependency><dependentAssembly>
    <assemblyIdentity type="win32" name="Microsoft.Windows.Common-Controls" version="6.0.0.0" processorArchitecture="*" publicKeyToken="6595b64144ccf1df" language="*"/>
  </dependentAssembly></dependency>
  <compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1"><application>
    <supportedOS Id="{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"/>
  </application></compatibility>
  <application xmlns="urn:schemas-microsoft-com:asm.v3"><windowsSettings>
    <dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true</dpiAware>
  </windowsSettings></application>
</assembly>`

type leaf struct {
	typ, id uint32
	data    []byte
}

func main() {
	ver := "1.0.0"
	if len(os.Args) > 1 {
		ver = os.Args[1]
	}
	var leaves []leaf
	var grp bytes.Buffer
	binary.Write(&grp, binary.LittleEndian, [3]uint16{0, 1, uint16(len(sizes))})
	for i, s := range sizes {
		png, err := os.ReadFile(fmt.Sprintf("assets/icon%d.png", s))
		if err != nil {
			panic(err)
		}
		id := uint32(i + 1)
		leaves = append(leaves, leaf{rtIcon, id, png})
		b := byte(s)
		if s >= 256 {
			b = 0
		}
		grp.Write([]byte{b, b, 0, 0})
		binary.Write(&grp, binary.LittleEndian, uint16(1))  // planes
		binary.Write(&grp, binary.LittleEndian, uint16(32)) // bpp
		binary.Write(&grp, binary.LittleEndian, uint32(len(png)))
		binary.Write(&grp, binary.LittleEndian, uint16(id))
	}
	leaves = append(leaves, leaf{rtGroupIcon, 1, grp.Bytes()})
	leaves = append(leaves, leaf{rtManifest, 1, []byte(fmt.Sprintf(manifest, ver))})
	leaves = append(leaves, leaf{rtVersion, 1, versionInfo(ver)})

	if err := os.WriteFile("rsrc_windows_amd64.syso", coff(leaves), 0644); err != nil {
		panic(err)
	}
	fmt.Println("wrote rsrc_windows_amd64.syso")
}

// ---------- VS_VERSIONINFO ----------

func u16z(s string) []byte {
	var b bytes.Buffer
	for _, r := range utf16.Encode([]rune(s)) {
		binary.Write(&b, binary.LittleEndian, r)
	}
	b.Write([]byte{0, 0})
	return b.Bytes()
}
func pad4(b *bytes.Buffer) {
	for b.Len()%4 != 0 {
		b.WriteByte(0)
	}
}

// block builds a version-resource node: wLength, wValueLength, wType, key, value, children.
func block(key string, value []byte, valueLen uint16, isText bool, children ...[]byte) []byte {
	var b bytes.Buffer
	b.Write([]byte{0, 0})
	binary.Write(&b, binary.LittleEndian, valueLen)
	t := uint16(0)
	if isText {
		t = 1
	}
	binary.Write(&b, binary.LittleEndian, t)
	b.Write(u16z(key))
	pad4(&b)
	b.Write(value)
	for _, c := range children {
		pad4(&b)
		b.Write(c)
	}
	out := b.Bytes()
	binary.LittleEndian.PutUint16(out[0:], uint16(len(out)))
	return out
}

func versionInfo(ver string) []byte {
	p := strings.Split(ver, ".")
	for len(p) < 4 {
		p = append(p, "0")
	}
	n := make([]uint16, 4)
	for i := 0; i < 4; i++ {
		v, _ := strconv.Atoi(p[i])
		n[i] = uint16(v)
	}
	var ff bytes.Buffer
	w := func(v uint32) { binary.Write(&ff, binary.LittleEndian, v) }
	w(0xFEEF04BD)
	w(0x00010000)
	w(uint32(n[0])<<16 | uint32(n[1]))
	w(uint32(n[2])<<16 | uint32(n[3]))
	w(uint32(n[0])<<16 | uint32(n[1]))
	w(uint32(n[2])<<16 | uint32(n[3]))
	w(0x3F)
	w(0)
	w(0x00040004) // VOS_NT_WINDOWS32
	w(1)          // VFT_APP
	w(0)
	w(0)
	w(0)
	str := func(k, v string) []byte {
		val := u16z(v)
		return block(k, val, uint16(len(val)/2), true)
	}
	strs := [][]byte{
		str("CompanyName", "ZephyCube"),
		str("FileDescription", "ZephyCube Pelican Console"),
		str("FileVersion", ver),
		str("InternalName", "PelicanConsole"),
		str("LegalCopyright", "© ZephyCube"),
		str("OriginalFilename", "PelicanConsole.exe"),
		str("ProductName", "ZephyCube Pelican Console"),
		str("ProductVersion", ver),
	}
	table := block("040904B0", nil, 0, true, strs...)
	sfi := block("StringFileInfo", nil, 0, true, table)
	var tr bytes.Buffer
	binary.Write(&tr, binary.LittleEndian, [2]uint16{0x0409, 0x04B0})
	vfi := block("VarFileInfo", nil, 0, true, block("Translation", tr.Bytes(), 4, false))
	return block("VS_VERSION_INFO", ff.Bytes(), uint16(ff.Len()), false, sfi, vfi)
}

// ---------- COFF .rsrc object ----------

func coff(leaves []leaf) []byte {
	types := map[uint32][]leaf{}
	var typeIDs []uint32
	for _, l := range leaves {
		if _, ok := types[l.typ]; !ok {
			typeIDs = append(typeIDs, l.typ)
		}
		types[l.typ] = append(types[l.typ], l)
	}
	sort.Slice(typeIDs, func(i, j int) bool { return typeIDs[i] < typeIDs[j] })
	for _, t := range typeIDs {
		ls := types[t]
		sort.Slice(ls, func(i, j int) bool { return ls[i].id < ls[j].id })
	}

	dirSize := func(n int) int { return 16 + 8*n }
	off := dirSize(len(typeIDs))
	nameDirOff := map[uint32]int{}
	for _, t := range typeIDs {
		nameDirOff[t] = off
		off += dirSize(len(types[t]))
	}
	type key struct{ t, id uint32 }
	langDirOff := map[key]int{}
	for _, t := range typeIDs {
		for _, l := range types[t] {
			langDirOff[key{t, l.id}] = off
			off += dirSize(1)
		}
	}
	dataEntryOff := map[key]int{}
	for _, t := range typeIDs {
		for _, l := range types[t] {
			dataEntryOff[key{t, l.id}] = off
			off += 16
		}
	}
	dataOff := map[key]int{}
	for _, t := range typeIDs {
		for _, l := range types[t] {
			off = (off + 7) &^ 7
			dataOff[key{t, l.id}] = off
			off += len(l.data)
		}
	}
	size := (off + 7) &^ 7
	sec := make([]byte, size)
	le := binary.LittleEndian
	writeDir := func(at, n int) {
		le.PutUint16(sec[at+14:], uint16(n)) // NumberOfIdEntries
	}
	writeDir(0, len(typeIDs))
	for i, t := range typeIDs {
		le.PutUint32(sec[16+8*i:], t)
		le.PutUint32(sec[16+8*i+4:], uint32(nameDirOff[t])|0x80000000)
		nd := nameDirOff[t]
		writeDir(nd, len(types[t]))
		for j, l := range types[t] {
			k := key{t, l.id}
			le.PutUint32(sec[nd+16+8*j:], l.id)
			le.PutUint32(sec[nd+16+8*j+4:], uint32(langDirOff[k])|0x80000000)
			ld := langDirOff[k]
			writeDir(ld, 1)
			le.PutUint32(sec[ld+16:], langEnUS)
			le.PutUint32(sec[ld+20:], uint32(dataEntryOff[k]))
			de := dataEntryOff[k]
			le.PutUint32(sec[de:], uint32(dataOff[k])) // relocated (ADDR32NB)
			le.PutUint32(sec[de+4:], uint32(len(l.data)))
			copy(sec[dataOff[k]:], l.data)
		}
	}

	var relocs []int
	for _, t := range typeIDs {
		for _, l := range types[t] {
			relocs = append(relocs, dataEntryOff[key{t, l.id}])
		}
	}

	var out bytes.Buffer
	const hdr, shdr = 20, 40
	rawPtr := hdr + shdr
	relPtr := rawPtr + len(sec)
	symPtr := relPtr + 10*len(relocs)
	// file header
	binary.Write(&out, le, uint16(0x8664))
	binary.Write(&out, le, uint16(1))
	binary.Write(&out, le, uint32(0))
	binary.Write(&out, le, uint32(symPtr))
	binary.Write(&out, le, uint32(1))
	binary.Write(&out, le, uint16(0))
	binary.Write(&out, le, uint16(0))
	// section header
	out.Write([]byte(".rsrc\x00\x00\x00"))
	binary.Write(&out, le, uint32(0))
	binary.Write(&out, le, uint32(0))
	binary.Write(&out, le, uint32(len(sec)))
	binary.Write(&out, le, uint32(rawPtr))
	binary.Write(&out, le, uint32(relPtr))
	binary.Write(&out, le, uint32(0))
	binary.Write(&out, le, uint16(len(relocs)))
	binary.Write(&out, le, uint16(0))
	binary.Write(&out, le, uint32(0x40000040))
	out.Write(sec)
	for _, r := range relocs {
		binary.Write(&out, le, uint32(r))
		binary.Write(&out, le, uint32(0))  // symbol index
		binary.Write(&out, le, uint16(3)) // IMAGE_REL_AMD64_ADDR32NB
	}
	// symbol table: .rsrc section symbol
	out.Write([]byte(".rsrc\x00\x00\x00"))
	binary.Write(&out, le, uint32(0))
	binary.Write(&out, le, int16(1))
	binary.Write(&out, le, uint16(0))
	out.WriteByte(3) // IMAGE_SYM_CLASS_STATIC
	out.WriteByte(0)
	binary.Write(&out, le, uint32(4)) // empty string table
	return out.Bytes()
}
