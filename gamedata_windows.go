//go:build windows

package main

import (
	_ "embed"
	"strconv"
	"strings"
)

//go:embed gamedata/items_java.txt
var itemsJavaTxt string

//go:embed gamedata/items_bedrock.txt
var itemsBedrockTxt string

//go:embed gamedata/effects.txt
var effectsTxt string

type mcItem struct {
	ID    string
	Stack int
	Name  string
}

type mcEffect struct {
	Java, Bedrock, Name string
}

var itemsJava, itemsBedrock []mcItem
var effects []mcEffect

func init() {
	itemsJava = parseItems(itemsJavaTxt)
	itemsBedrock = parseItems(itemsBedrockTxt)
	for _, ln := range strings.Split(strings.TrimSpace(effectsTxt), "\n") {
		p := strings.Split(ln, "|")
		if len(p) == 3 {
			effects = append(effects, mcEffect{p[0], p[1], p[2]})
		}
	}
}

func parseItems(txt string) []mcItem {
	var out []mcItem
	for _, ln := range strings.Split(strings.TrimSpace(txt), "\n") {
		p := strings.SplitN(ln, "|", 3)
		if len(p) != 3 {
			continue
		}
		n, _ := strconv.Atoi(p[1])
		if n <= 0 {
			n = 64
		}
		out = append(out, mcItem{p[0], n, p[2]})
	}
	return out
}

func itemList(bedrock bool) []mcItem {
	if bedrock {
		return itemsBedrock
	}
	return itemsJava
}

// effectList returns the effects available for an edition (display name, command id).
func effectList(bedrock bool) []mcEffect {
	var out []mcEffect
	for _, e := range effects {
		if (bedrock && e.Bedrock != "") || (!bedrock && e.Java != "") {
			out = append(out, e)
		}
	}
	return out
}

func filterItems(list []mcItem, q string, limit int) []mcItem {
	q = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(q), "minecraft:")))
	if q == "" {
		return list
	}
	qs := strings.ReplaceAll(q, " ", "_")
	var pre, mid []mcItem
	for _, it := range list {
		id := it.ID
		name := strings.ToLower(it.Name)
		switch {
		case strings.HasPrefix(id, qs) || strings.HasPrefix(name, q):
			pre = append(pre, it)
		case strings.Contains(id, qs) || strings.Contains(name, q):
			mid = append(mid, it)
		}
		if len(pre) >= limit {
			break
		}
	}
	out := append(pre, mid...)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func findItem(list []mcItem, id string) *mcItem {
	id = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(id), "minecraft:")))
	for i := range list {
		if list[i].ID == id {
			return &list[i]
		}
	}
	return nil
}
