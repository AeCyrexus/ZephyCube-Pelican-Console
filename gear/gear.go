// Package gear knows Minecraft armor sets and the best non-conflicting
// ("max") enchantments for gear, and builds Java /give commands for them.
package gear

import (
	"fmt"
	"strings"
)

// Ench is one enchantment id (Java/Bedrock share these ids) and level.
type Ench struct {
	ID    string
	Level int
}

// ArmorMaterials in UI order.
var ArmorMaterials = []string{"Netherite", "Diamond", "Iron", "Copper", "Golden", "Chainmail", "Leather"}

// ArmorPieces in head-to-feet order.
var ArmorPieces = []string{"helmet", "chestplate", "leggings", "boots"}

// ArmorSet returns the item ids of a full set for a material ("Diamond" → diamond_helmet …).
func ArmorSet(material string) []string {
	m := strings.ToLower(strings.TrimSpace(material))
	out := make([]string, len(ArmorPieces))
	for i, p := range ArmorPieces {
		out[i] = m + "_" + p
	}
	return out
}

var (
	base   = []Ench{{"unbreaking", 3}, {"mending", 1}}
	armor  = append([]Ench{{"protection", 4}, {"thorns", 3}}, base...)
	weapon = []Ench{{"sharpness", 5}, {"looting", 3}, {"fire_aspect", 2}, {"knockback", 2}}
	tool   = []Ench{{"efficiency", 5}, {"fortune", 3}}
)

func join(parts ...[]Ench) []Ench {
	var out []Ench
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// MaxEnchants returns the strongest compatible enchantment set for an item id
// (with or without "minecraft:"), or nil if the item takes no enchantments we know.
// Conflicts are resolved toward the usual "god" picks: Protection over the other
// protections, Depth Strider over Frost Walker, Sharpness over Smite/Bane,
// Fortune over Silk Touch, Infinity over Mending on bows, Loyalty over Riptide.
func MaxEnchants(id string) []Ench {
	id = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(id), "minecraft:"))
	switch {
	case strings.HasSuffix(id, "_helmet"):
		return join([]Ench{{"respiration", 3}, {"aqua_affinity", 1}}, armor)
	case strings.HasSuffix(id, "_chestplate"):
		return armor
	case strings.HasSuffix(id, "_leggings"):
		return join([]Ench{{"swift_sneak", 3}}, armor)
	case strings.HasSuffix(id, "_boots"):
		return join([]Ench{{"feather_falling", 4}, {"depth_strider", 3}, {"soul_speed", 3}}, armor)
	case strings.HasSuffix(id, "_sword"):
		return join(weapon, []Ench{{"sweeping_edge", 3}}, base)
	case strings.HasSuffix(id, "_spear"):
		return join(weapon, []Ench{{"lunge", 3}}, base)
	case strings.HasSuffix(id, "_axe"):
		return join([]Ench{{"sharpness", 5}}, tool, base)
	case strings.HasSuffix(id, "_pickaxe"), strings.HasSuffix(id, "_shovel"), strings.HasSuffix(id, "_hoe"):
		return join(tool, base)
	}
	switch id {
	case "bow":
		return []Ench{{"power", 5}, {"punch", 2}, {"flame", 1}, {"infinity", 1}, {"unbreaking", 3}}
	case "crossbow":
		return join([]Ench{{"quick_charge", 3}, {"multishot", 1}}, base)
	case "trident":
		return join([]Ench{{"loyalty", 3}, {"impaling", 5}, {"channeling", 1}}, base)
	case "mace":
		return join([]Ench{{"density", 5}, {"wind_burst", 3}, {"fire_aspect", 2}}, base)
	case "fishing_rod":
		return join([]Ench{{"luck_of_the_sea", 3}, {"lure", 3}}, base)
	case "shears":
		return join([]Ench{{"efficiency", 5}}, base)
	case "elytra", "shield", "flint_and_steel", "carrot_on_a_stick", "warped_fungus_on_a_stick", "brush":
		return base
	}
	return nil
}

// Opts tweaks the max set.
type Opts struct {
	NoThorns  bool // drop Thorns from armor
	SilkTouch bool // Silk Touch instead of Fortune on tools
}

// Pick is MaxEnchants with options applied, filtered for the edition
// (Bedrock has no Sweeping Edge).
func Pick(id string, bedrock bool, o Opts) []Ench {
	var out []Ench
	for _, e := range MaxEnchants(id) {
		switch {
		case e.ID == "sweeping_edge" && bedrock:
			continue
		case e.ID == "thorns" && o.NoThorns:
			continue
		case e.ID == "fortune" && o.SilkTouch:
			e = Ench{"silk_touch", 1}
		}
		out = append(out, e)
	}
	return out
}

// BedrockToHand puts an item into the target's main hand only if it is empty
// ("keep"), so the following /enchant commands can't hit another item.
func BedrockToHand(target, id string, qty int) string {
	id = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(id)), "minecraft:")
	return fmt.Sprintf("replaceitem entity %s slot.weapon.mainhand 0 keep %s %d", target, id, qty)
}

// EnchantCmds returns one /enchant command per enchantment (works on the held item).
func EnchantCmds(target string, ench []Ench) []string {
	out := make([]string, len(ench))
	for i, e := range ench {
		out[i] = fmt.Sprintf("enchant %s %s %d", target, e.ID, e.Level)
	}
	return out
}

// JavaGive builds a Java 1.21.5+ give command (item components syntax).
// target is inserted as-is (already quoted if needed).
func JavaGive(target, id string, qty int, ench []Ench) string {
	id = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(id)), "minecraft:")
	item := "minecraft:" + id
	if len(ench) > 0 {
		parts := make([]string, len(ench))
		for i, e := range ench {
			parts[i] = fmt.Sprintf("%s:%d", e.ID, e.Level)
		}
		item += "[minecraft:enchantments={" + strings.Join(parts, ",") + "}]"
	}
	return fmt.Sprintf("give %s %s %d", target, item, qty)
}

// BedrockGive builds a Bedrock give command (vanilla Bedrock cannot enchant via /give).
func BedrockGive(target, id string, qty int) string {
	id = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(id)), "minecraft:")
	return fmt.Sprintf("give %s %s %d", target, id, qty)
}
