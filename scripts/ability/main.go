package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/darkspinnet/web/scripts/internal/catalog"
)

type heroCatalog struct {
	Heroes []hero `json:"heroes"`
}
type hero struct {
	Family    string    `json:"family"`
	Variant   string    `json:"variant"`
	Element   string    `json:"element"`
	Class     string    `json:"class"`
	Abilities []ability `json:"abilities"`
}
type ability struct {
	Slot      string `json:"slot"`
	AssetName string `json:"asset_name"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
}
type owner struct {
	Name     string   `json:"name"`
	Slug     string   `json:"slug"`
	Element  string   `json:"element"`
	Class    string   `json:"class"`
	Variants []string `json:"variants"`
	Slots    []string `json:"slots"`
}
type abilityEntry struct {
	AssetName     string        `json:"asset_name"`
	Name          string        `json:"name"`
	Slug          string        `json:"slug"`
	Description   string        `json:"description,omitempty"`
	IconReference string        `json:"icon_reference,omitempty"`
	IconPath      string        `json:"icon_path,omitempty"`
	PrimarySlot   string        `json:"primary_slot"`
	Slots         []string      `json:"slots"`
	UsageCount    int           `json:"usage_count"`
	Heroes        []owner       `json:"heroes"`
	Combat        *combatRecord `json:"combat,omitempty"`
	Source        *sourceRecord `json:"source,omitempty"`
}
type abilityCatalog struct {
	Count int                     `json:"count"`
	Slots map[string]int          `json:"slots"`
	Items []abilityEntry          `json:"items"`
	Entry map[string]abilityEntry `json:"entry"`
}
type abilityBuilder struct {
	entry  abilityEntry
	owners map[string]*owner
}

func main() {
	heroPath := flag.String("hero", filepath.Join("data", "generated", "hero.json"), "generated hero catalog")
	databasePath := flag.String("db", catalog.DefaultDatabasePath, "content.db path")
	gamePath := flag.String("game", catalog.DefaultGamePath, "Darkspore install root")
	outputPath := flag.String("out", filepath.Join("data", "generated", "ability.json"), "generated ability catalog")
	contentPath := flag.String("content", filepath.Join("content", "ability"), "generated ability content directory")
	flag.Parse()
	contents, err := os.ReadFile(*heroPath)
	if err != nil {
		fatal(fmt.Errorf("heroOpen: %w", err))
	}
	var heroes heroCatalog
	if err = json.Unmarshal(contents, &heroes); err != nil {
		fatal(fmt.Errorf("heroDecode: %w", err))
	}
	result := build(heroes.Heroes)
	database, err := catalog.Open(*databasePath)
	if err != nil {
		fatal(err)
	}
	defer database.Close()
	err = enrichAbilities(database, *gamePath, result.Items)
	if err != nil {
		fatal(err)
	}
	reindex(&result)
	if err = catalog.WriteJSON(*outputPath, result); err != nil {
		fatal(err)
	}
	if err = writeContent(*contentPath, result.Items); err != nil {
		fatal(err)
	}
	fmt.Printf("abilities: wrote %d recovered ability assets to %s\n", result.Count, *outputPath)
}

func reindex(result *abilityCatalog) {
	result.Entry = make(map[string]abilityEntry, len(result.Items))
	sort.Slice(result.Items, func(i, j int) bool {
		return strings.ToLower(result.Items[i].Name) < strings.ToLower(result.Items[j].Name)
	})
	for _, entry := range result.Items {
		result.Entry[entry.Slug] = entry
	}
}

func build(heroes []hero) abilityCatalog {
	builders := make(map[string]*abilityBuilder)
	for _, hero := range heroes {
		familySlug := catalog.Slug(hero.Family)
		for _, source := range hero.Abilities {
			slug, name := source.Slug, source.Name
			if slug == "" {
				slug = catalog.Slug(source.AssetName)
			}
			if name == "" {
				name = catalog.DisplayName(source.AssetName)
			}
			builder := builders[slug]
			if builder == nil {
				builder = &abilityBuilder{entry: abilityEntry{AssetName: source.AssetName, Name: name, Slug: slug}, owners: make(map[string]*owner)}
				builders[slug] = builder
			}
			builder.entry.UsageCount++
			builder.entry.Slots = appendUnique(builder.entry.Slots, source.Slot)
			owned := builder.owners[familySlug]
			if owned == nil {
				owned = &owner{Name: hero.Family, Slug: familySlug, Element: hero.Element, Class: hero.Class}
				builder.owners[familySlug] = owned
			}
			owned.Variants = appendUnique(owned.Variants, hero.Variant)
			owned.Slots = appendUnique(owned.Slots, source.Slot)
		}
	}
	result := abilityCatalog{Slots: make(map[string]int), Entry: make(map[string]abilityEntry), Items: make([]abilityEntry, 0, len(builders))}
	for _, builder := range builders {
		sort.Strings(builder.entry.Slots)
		builder.entry.PrimarySlot = builder.entry.Slots[0]
		for _, owned := range builder.owners {
			sort.Slice(owned.Variants, func(i, j int) bool { return variantOrder(owned.Variants[i]) < variantOrder(owned.Variants[j]) })
			sort.Strings(owned.Slots)
			builder.entry.Heroes = append(builder.entry.Heroes, *owned)
		}
		sort.Slice(builder.entry.Heroes, func(i, j int) bool { return builder.entry.Heroes[i].Name < builder.entry.Heroes[j].Name })
		result.Items = append(result.Items, builder.entry)
		result.Entry[builder.entry.Slug] = builder.entry
		result.Slots[builder.entry.PrimarySlot]++
	}
	sort.Slice(result.Items, func(i, j int) bool {
		return strings.ToLower(result.Items[i].Name) < strings.ToLower(result.Items[j].Name)
	})
	result.Count = len(result.Items)
	return result
}

func appendUnique(values []string, addition string) []string {
	for _, value := range values {
		if value == addition {
			return values
		}
	}
	return append(values, addition)
}
func variantOrder(value string) int {
	for index, variant := range []string{"Alpha", "Beta", "Gamma", "Delta"} {
		if value == variant {
			return index
		}
	}
	return 99
}
func writeContent(path string, entries []abilityEntry) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	for _, entry := range entries {
		contents := fmt.Sprintf("+++\ntitle = %q\nability = %q\ngenerated = true\n+++\n", entry.Name, entry.Slug)
		if err := catalog.WriteText(filepath.Join(path, entry.Slug+".md"), contents); err != nil {
			return fmt.Errorf("contentWrite[%s]: %w", entry.Slug, err)
		}
	}
	return nil
}
func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
