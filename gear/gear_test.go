package gear

import "testing"

func TestArmorSet(t *testing.T) {
	s := ArmorSet("Netherite")
	if len(s) != 4 || s[0] != "netherite_helmet" || s[3] != "netherite_boots" {
		t.Fatal(s)
	}
}

func TestMaxEnchants(t *testing.T) {
	has := func(id, e string, lvl int) bool {
		for _, x := range MaxEnchants(id) {
			if x.ID == e {
				return x.Level == lvl
			}
		}
		return false
	}
	cases := []struct {
		id, ench string
		lvl      int
	}{
		{"minecraft:diamond_helmet", "aqua_affinity", 1},
		{"netherite_chestplate", "protection", 4},
		{"iron_leggings", "swift_sneak", 3},
		{"golden_boots", "depth_strider", 3},
		{"netherite_sword", "sweeping_edge", 3},
		{"diamond_pickaxe", "fortune", 3},
		{"bow", "infinity", 1},
		{"mace", "density", 5},
		{"elytra", "mending", 1},
	}
	for _, c := range cases {
		if !has(c.id, c.ench, c.lvl) {
			t.Errorf("%s: missing %s %d in %v", c.id, c.ench, c.lvl, MaxEnchants(c.id))
		}
	}
	if has("golden_boots", "frost_walker", 2) || has("bow", "mending", 1) || has("diamond_pickaxe", "silk_touch", 1) {
		t.Error("conflicting enchant included")
	}
	if MaxEnchants("diamond") != nil || MaxEnchants("stone") != nil {
		t.Error("non-gear got enchants")
	}
	for _, id := range []string{"turtle_helmet", "leather_boots", "trident", "crossbow", "fishing_rod", "shears", "wooden_hoe", "iron_spear"} {
		if len(MaxEnchants(id)) == 0 {
			t.Errorf("%s: no enchants", id)
		}
	}
}

func TestGive(t *testing.T) {
	got := JavaGive(`"Ae Cyrexus"`, "minecraft:Diamond_Boots", 1, []Ench{{"protection", 4}, {"mending", 1}})
	want := `give "Ae Cyrexus" minecraft:diamond_boots[minecraft:enchantments={protection:4,mending:1}] 1`
	if got != want {
		t.Fatalf("\n got %s\nwant %s", got, want)
	}
	if JavaGive("Steve", "stone", 64, nil) != "give Steve minecraft:stone 64" {
		t.Fatal("plain")
	}
	if BedrockGive("Steve", "minecraft:iron_helmet", 1) != "give Steve iron_helmet 1" {
		t.Fatal("bedrock")
	}
}
