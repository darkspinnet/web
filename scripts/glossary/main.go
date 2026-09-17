package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/darkspinnet/web/scripts/internal/catalog"
)

type glossaryEntry struct {
	Name       string   `json:"name"`
	Slug       string   `json:"slug"`
	Category   string   `json:"category"`
	Definition string   `json:"definition"`
	Link       string   `json:"link,omitempty"`
	SeeAlso    []string `json:"see_also"`
}

type glossaryCatalog struct {
	Count      int                      `json:"count"`
	Categories map[string]int           `json:"categories"`
	Items      []glossaryEntry          `json:"items"`
	Entry      map[string]glossaryEntry `json:"entry"`
}

var definitions = []glossaryEntry{
	{Name: "Ability", Category: "combat", Definition: "A hero action or persistent trait defined by packaged gameplay data. Abilities occupy basic, passive, random, or special loadout slots.", Link: "/ability/", SeeAlso: []string{"basic-ability", "passive-ability", "special-ability", "variant-ability"}},
	{Name: "Basic Ability", Category: "combat", Definition: "The hero's standard combat action in the basic loadout slot.", Link: "/ability/", SeeAlso: []string{"ability"}},
	{Name: "Cooldown", Category: "combat", Definition: "The recovery period after an ability is used before it can be activated again.", SeeAlso: []string{"ability", "power"}},
	{Name: "Energy Damage", Category: "combat", Definition: "One of Darkspore's two broad damage channels. Abilities identify their authored damage channel separately from their elemental theme.", SeeAlso: []string{"physical-damage", "element"}},
	{Name: "Health", Category: "combat", Definition: "A creature's remaining survivability. A hero or enemy is defeated when its health is exhausted."},
	{Name: "Passive Ability", Category: "combat", Definition: "An always-on or automatically triggered ability in the passive loadout slot.", Link: "/ability/", SeeAlso: []string{"ability"}},
	{Name: "Physical Damage", Category: "combat", Definition: "One of Darkspore's two broad damage channels, distinct from energy damage.", SeeAlso: []string{"energy-damage"}},
	{Name: "Power", Category: "combat", Definition: "The resource spent to activate abilities. Packaged Lua registrations commonly call this value mana cost internally.", SeeAlso: []string{"ability", "cooldown"}},
	{Name: "Special Ability", Category: "combat", Definition: "An activated hero ability occupying one of the two special loadout positions.", Link: "/ability/", SeeAlso: []string{"ability"}},
	{Name: "Status Effect", Category: "combat", Definition: "A temporary gameplay condition that buffs, damages, controls, protects, or restricts a creature.", Link: "/buff/"},
	{Name: "Variant Ability", Category: "combat", Definition: "The ability that changes between a hero family's Alpha, Beta, Gamma, and Delta profiles. The archive labels this as the random slot.", Link: "/ability/", SeeAlso: []string{"genetic-variant"}},

	{Name: "Class", Category: "hero", Definition: "A hero's broad combat role: Sentinel, Ravager, or Tempest.", Link: "/hero/", SeeAlso: []string{"sentinel", "ravager", "tempest"}},
	{Name: "Crogenitor Level", Category: "hero", Definition: "The progression level used to determine when hero profiles become available."},
	{Name: "Element", Category: "hero", Definition: "A hero's genesis affinity. The archive preserves the content labels Cyber, Chrono, Bio, Plasma, and Necro.", Link: "/hero/", SeeAlso: []string{"genesis", "quantum"}},
	{Name: "Genesis", Category: "hero", Definition: "The in-game term for elemental affinity, used to group heroes and combat relationships.", SeeAlso: []string{"element"}},
	{Name: "Genetic Variant", Category: "hero", Definition: "One of four profiles in a hero family: Alpha, Beta, Gamma, or Delta. Variants can change unlock level, statistics, and the variant ability.", Link: "/hero/", SeeAlso: []string{"hero-family", "variant-ability"}},
	{Name: "Hero", Category: "hero", Definition: "A playable genetic combatant with an element, class, statistics, equipment, and ability loadout.", Link: "/hero/"},
	{Name: "Hero Family", Category: "hero", Definition: "The archive grouping that combines a hero's four genetic variants into one dossier.", Link: "/hero/", SeeAlso: []string{"genetic-variant"}},
	{Name: "Quantum", Category: "hero", Definition: "The player-facing affinity name associated with quantum and spacetime powers. Some recovered content fields use the label Chrono.", SeeAlso: []string{"element"}},
	{Name: "Ravager", Category: "hero", Definition: "One of the three hero classes, generally associated with aggressive close-range or mobile combat.", Link: "/hero/"},
	{Name: "Sentinel", Category: "hero", Definition: "One of the three hero classes, generally associated with durability and defensive control.", Link: "/hero/"},
	{Name: "Squad", Category: "hero", Definition: "A player-selected group of heroes used together during an expedition."},
	{Name: "Tempest", Category: "hero", Definition: "One of the three hero classes, generally associated with ranged abilities and support or control.", Link: "/hero/"},

	{Name: "Item", Category: "equipment", Definition: "A piece of equipment used to improve a hero. The archive groups procedural item variations beneath a recovered base family.", Link: "/item/"},
	{Name: "Item Level", Category: "equipment", Definition: "The level attached to an equipment roll, used as a compact indication of that item's progression tier.", Link: "/item/"},
	{Name: "Prefix", Category: "equipment", Definition: "A procedural item-name component associated with one set of generated item modifiers.", Link: "/item/", SeeAlso: []string{"suffix", "rigblock"}},
	{Name: "Rigblock", Category: "equipment", Definition: "A generated equipment record that joins a visual part, compatible affinities, and procedural item data.", Link: "/item/", SeeAlso: []string{"prefix", "suffix"}},
	{Name: "Suffix", Category: "equipment", Definition: "A procedural item-name component associated with an additional generated modifier.", Link: "/item/", SeeAlso: []string{"prefix", "rigblock"}},

	{Name: "Enemy", Category: "archive", Definition: "A combat-capable non-player creature recovered from placed level nouns and class data.", Link: "/npc/"},
	{Name: "Level", Category: "archive", Definition: "A playable or development area recovered from packaged level, chain, marker, and spawn data.", Link: "/level/"},
	{Name: "Locale Key", Category: "archive", Definition: "A stable identifier used to look up authored text in Darkspore's localization tables."},
	{Name: "Noun", Category: "archive", Definition: "Darkspore's content term for a game object definition, including creatures, projectiles, pickups, and world objects."},
	{Name: "Package", Category: "archive", Definition: "A DBPF container from the Darkspore client that stores typed game resources such as UI images, levels, scripts, and asset definitions."},
}

func main() {
	outputPath := flag.String("out", filepath.Join("data", "generated", "glossary.json"), "generated glossary catalog")
	contentPath := flag.String("content", filepath.Join("content", "glossary"), "generated glossary content directory")
	flag.Parse()
	result := build(definitions)
	if err := catalog.WriteJSON(*outputPath, result); err != nil {
		fatal(err)
	}
	if err := writeContent(*contentPath, result.Items); err != nil {
		fatal(err)
	}
	fmt.Printf("glossary: wrote %d archive terms to %s\n", result.Count, *outputPath)
}

func build(entries []glossaryEntry) glossaryCatalog {
	result := glossaryCatalog{Categories: make(map[string]int), Entry: make(map[string]glossaryEntry), Items: append([]glossaryEntry(nil), entries...)}
	for index := range result.Items {
		entry := &result.Items[index]
		entry.Slug = catalog.Slug(entry.Name)
		if entry.SeeAlso == nil {
			entry.SeeAlso = []string{}
		}
		sort.Strings(entry.SeeAlso)
		result.Entry[entry.Slug] = *entry
		result.Categories[entry.Category]++
	}
	sort.Slice(result.Items, func(i, j int) bool { return result.Items[i].Name < result.Items[j].Name })
	result.Count = len(result.Items)
	return result
}

func writeContent(path string, entries []glossaryEntry) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	for _, entry := range entries {
		contents := fmt.Sprintf("+++\ntitle = %q\nglossary = %q\ngenerated = true\n+++\n", entry.Name, entry.Slug)
		if err := catalog.WriteText(filepath.Join(path, entry.Slug+".md"), contents); err != nil {
			return fmt.Errorf("contentWrite[%s]: %w", entry.Slug, err)
		}
	}
	return nil
}

func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
