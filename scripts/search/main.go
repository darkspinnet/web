package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/darkspinnet/web/scripts/internal/catalog"
)

type heroCatalog struct {
	Families []heroFamily `json:"families"`
}

type heroFamily struct {
	Name     string        `json:"name"`
	Slug     string        `json:"slug"`
	Element  string        `json:"element"`
	Class    string        `json:"class"`
	Title    string        `json:"title"`
	Variants []heroVariant `json:"variants"`
}

type heroVariant struct {
	Name    string `json:"name"`
	Variant string `json:"variant"`
}

type npcCatalog struct {
	Entry map[string]npcDetail `json:"entry"`
}

type npcDetail struct {
	Name        string   `json:"name"`
	AssetName   string   `json:"asset_name"`
	Slug        string   `json:"slug"`
	HitPoints   *float64 `json:"hit_points"`
	IsCombatant bool     `json:"is_combatant"`
}

type levelCatalog struct {
	Levels []levelReport `json:"levels"`
}

type levelReport struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Slug        string `json:"slug"`
	Region      string `json:"region"`
}

type itemCatalog struct {
	Items []map[string]any `json:"items"`
}

type abilityCatalog struct {
	Items []abilityEntry `json:"items"`
}
type abilityEntry struct {
	Name        string         `json:"name"`
	AssetName   string         `json:"asset_name"`
	Description string         `json:"description"`
	Slug        string         `json:"slug"`
	PrimarySlot string         `json:"primary_slot"`
	Heroes      []abilityOwner `json:"heroes"`
}
type abilityOwner struct{ Name, Element, Class string }

type buffCatalog struct {
	Items []buffEntry `json:"items"`
}
type buffEntry struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Category    string `json:"category"`
	Description string `json:"description"`
}

type effectCatalog struct {
	Items []effectEntry `json:"items"`
}
type effectEntry struct {
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	Category string `json:"category"`
}

type glossaryCatalog struct {
	Items []glossaryEntry `json:"items"`
}
type glossaryEntry struct {
	Name       string `json:"name"`
	Slug       string `json:"slug"`
	Category   string `json:"category"`
	Definition string `json:"definition"`
}

type systemCatalog struct {
	ReportCount int `json:"report_count"`
}

func main() {
	dataPath := flag.String("data", filepath.Join("data", "generated"), "generated data directory")
	outputPath := flag.String("out", filepath.Join("static", "data", "search", "index.json"), "compact search index")
	flag.Parse()
	rows, err := build(*dataPath)
	if err != nil {
		fatal(err)
	}
	err = catalog.WriteCompactJSON(*outputPath, rows)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("search: wrote %d lookup records to %s\n", len(rows), *outputPath)
}

func build(dataPath string) ([][]string, error) {
	rows := make([][]string, 0, 1024)
	var heroes heroCatalog
	err := readJSON(filepath.Join(dataPath, "hero.json"), &heroes)
	if err != nil {
		return nil, fmt.Errorf("heroRead: %w", err)
	}
	for _, family := range heroes.Families {
		meta := family.Element + " / " + family.Class
		rows = append(rows, searchRow(family.Name, "HERO", meta, "/hero/"+family.Slug+"/", family.Name+" "+family.Title+" "+meta))
	}
	var npcs npcCatalog
	err = readJSON(filepath.Join(dataPath, "npc.json"), &npcs)
	if err != nil {
		return nil, fmt.Errorf("npcRead: %w", err)
	}
	for _, entry := range npcs.Entry {
		meta := "PLACED NOUN"
		if entry.IsCombatant {
			meta = "COMBAT CLASS"
			if entry.HitPoints != nil {
				meta = fmt.Sprintf("COMBAT / %.0f HP", *entry.HitPoints)
			}
		}
		rows = append(rows, searchRow(entry.Name, "ENEMY", meta, "/npc/"+entry.Slug+"/", entry.Name+" "+entry.AssetName+" "+meta))
	}
	var levels levelCatalog
	err = readJSON(filepath.Join(dataPath, "level.json"), &levels)
	if err != nil {
		return nil, fmt.Errorf("levelRead: %w", err)
	}
	for _, level := range levels.Levels {
		name := level.DisplayName
		if name == "" {
			name = level.Name
		}
		rows = append(rows, searchRow(name, "LEVEL", level.Region+" / "+level.Name, "/level/"+level.Slug+"/", name+" "+level.Name+" "+level.Region))
	}
	var abilities abilityCatalog
	err = readJSON(filepath.Join(dataPath, "ability.json"), &abilities)
	if err != nil {
		return nil, fmt.Errorf("abilityRead: %w", err)
	}
	for _, ability := range abilities.Items {
		heroes := make([]string, 0, len(ability.Heroes))
		for _, owner := range ability.Heroes {
			heroes = append(heroes, owner.Name, owner.Element, owner.Class)
		}
		meta := strings.ToUpper(strings.ReplaceAll(ability.PrimarySlot, "_", " ")) + " / ABILITY"
		rows = append(rows, searchRow(ability.Name, "ABILITY", meta, "/ability/"+ability.Slug+"/", ability.Name+" "+ability.AssetName+" "+ability.Description+" "+strings.Join(heroes, " ")+" "+meta))
	}
	var buffs buffCatalog
	err = readJSON(filepath.Join(dataPath, "buff.json"), &buffs)
	if err != nil {
		return nil, fmt.Errorf("buffRead: %w", err)
	}
	for _, buff := range buffs.Items {
		meta := strings.ToUpper(buff.Category) + " / BUFF"
		rows = append(rows, searchRow(buff.Name, "BUFF", meta, "/buff/"+buff.Slug+"/", buff.Name+" "+buff.Description+" "+meta))
	}
	var effects effectCatalog
	err = readJSON(filepath.Join(dataPath, "effect.json"), &effects)
	if err != nil {
		return nil, fmt.Errorf("effectRead: %w", err)
	}
	for _, effect := range effects.Items {
		meta := strings.ToUpper(effect.Category) + " / EFFECT"
		rows = append(rows, searchRow(effect.Name, "EFFECT", meta, "/effect/#"+effect.Slug, effect.Name+" "+meta))
	}
	var glossary glossaryCatalog
	err = readJSON(filepath.Join(dataPath, "glossary.json"), &glossary)
	if err != nil {
		return nil, fmt.Errorf("glossaryRead: %w", err)
	}
	for _, term := range glossary.Items {
		meta := strings.ToUpper(term.Category) + " / GLOSSARY"
		rows = append(rows, searchRow(term.Name, "TERM", meta, "/glossary/"+term.Slug+"/", term.Name+" "+term.Definition+" "+meta))
	}
	var systems systemCatalog
	err = readJSON(filepath.Join(dataPath, "system.json"), &systems)
	if err != nil {
		return nil, fmt.Errorf("systemRead: %w", err)
	}
	if systems.ReportCount != 5 {
		return nil, fmt.Errorf("systemCount: got %d, want 5", systems.ReportCount)
	}
	rows = append(rows,
		searchRow("Drop Probabilities", "SYSTEM", "LOOT / SERVER RUNTIME", "/system/drops/", "drop probabilities loot equipment rarity basic uncommon rare epic capsule health power catalyst DNA chance server runtime"),
		searchRow("XP & Progression", "SYSTEM", "XP / SERVER RUNTIME", "/system/progression/", "experience XP Crogenitor level curve table hero rewards progression server runtime"),
		searchRow("Level Unlocks", "SYSTEM", "UPGRADES / HEROES", "/system/level-unlocks/", "level unlocks upgrades DNA costs catalysts inventory squads chain capacity flair heroes variants alpha beta gamma delta Crogenitor eligibility reward choices milestones"),
		searchRow("Reward Tables", "SYSTEM", "REWARDS / SERVER RUNTIME", "/system/rewards/", "cash out parts item level medal special rarified purified unique hero entitlement milestones rewards server runtime"),
		searchRow("Mission Objectives & Medals", "SYSTEM", "OBJECTIVES / MEDALS", "/system/objectives/", "mission objectives medals clear map defeat all monsters bronze silver gold admitted hostile enemies fast clear timer obelisks huge damage chain result coop host slot zero cash out rarity"),
	)
	err = appendOptionalItems(filepath.Join(dataPath, "item.json"), &rows)
	if err != nil {
		return nil, fmt.Errorf("itemRead: %w", err)
	}
	return rows, nil
}

func searchRow(name, kind, meta, path, terms string) []string {
	return []string{name, kind, meta, path, strings.ToLower(terms)}
}

func appendOptionalItems(path string, rows *[][]string) error {
	contents, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("itemOpen: %w", err)
	}
	var catalog itemCatalog
	err = json.Unmarshal(contents, &catalog)
	if err != nil {
		var legacy []map[string]any
		if legacyErr := json.Unmarshal(contents, &legacy); legacyErr != nil {
			return fmt.Errorf("itemDecode: %w", err)
		}
		catalog.Items = legacy
	}
	for _, item := range catalog.Items {
		name, _ := item["name"].(string)
		if name == "" {
			name, _ = item["item_name"].(string)
		}
		if name == "" {
			continue
		}
		id := fmt.Sprint(item["variant_count"])
		slug, _ := item["slug"].(string)
		if slug == "" {
			slug = id
		}
		category, _ := item["category"].(string)
		meta := category + " / ITEM FAMILY"
		*rows = append(*rows, searchRow(name, "ITEM", meta, "/item/"+slug+"/", name+" "+id+" "+category))
	}
	return nil
}

func readJSON(path string, target any) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("fileOpen: %w", err)
	}
	err = json.Unmarshal(contents, target)
	if err != nil {
		return fmt.Errorf("fileDecode: %w", err)
	}
	return nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
