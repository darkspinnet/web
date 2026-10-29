package main

import (
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/darkspinnet/web/scripts/internal/catalog"
)

type npc struct {
	AssetName   string   `json:"asset_name"`
	Name        string   `json:"name"`
	Slug        string   `json:"slug"`
	InstanceID  uint32   `json:"instance_id"`
	HitPoints   *float64 `json:"hit_points,omitempty"`
	Element     string   `json:"element,omitempty"`
	Image       string   `json:"image,omitempty"`
	Category    string   `json:"category"`
	Description string   `json:"description,omitempty"`
	IsCombatant bool     `json:"is_combatant"`
	Sources     []string `json:"sources"`
}

type levelGroup struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	NPCCount    int    `json:"npc_count"`
	CombatCount int    `json:"combat_count"`
	NPCs        []npc  `json:"npcs"`
}

type npcCatalog struct {
	Count       int                  `json:"count"`
	EntryCount  int                  `json:"entry_count"`
	CombatCount int                  `json:"combat_count"`
	LevelCount  int                  `json:"level_count"`
	Levels      []levelGroup         `json:"levels"`
	Entry       map[string]npcDetail `json:"entry"`
}

type npcDetail struct {
	FamilySlug             string              `json:"family_slug"`
	NPCRank                int                 `json:"npc_rank"`
	PlayerCountHealthScale float32             `json:"player_count_health_scale"`
	HealthScaling          string              `json:"health_scaling"`
	AbilityPreviews        []npcAbilityPreview `json:"ability_previews,omitempty"`
	ImageAssetID           uint64              `json:"image_asset_id,omitempty"`
	ImageModel             string              `json:"image_model,omitempty"`
	AssetName              string              `json:"asset_name"`
	Name                   string              `json:"name"`
	Slug                   string              `json:"slug"`
	InstanceID             uint32              `json:"instance_id"`
	HitPoints              *float64            `json:"hit_points,omitempty"`
	Element                string              `json:"element,omitempty"`
	Image                  string              `json:"image,omitempty"`
	Category               string              `json:"category"`
	Description            string              `json:"description,omitempty"`
	IsCombatant            bool                `json:"is_combatant"`
	Placements             []npcPlacement      `json:"placements"`
}

type npcPlacement struct {
	Level     string   `json:"level"`
	LevelSlug string   `json:"level_slug"`
	Sources   []string `json:"sources"`
}

type levelIndex struct {
	ID   int64
	Name string
}

func main() {
	gamePath := flag.String("game", catalog.DefaultGamePath, "installed client root for original creature portraits")
	databasePath := flag.String("db", catalog.DefaultDatabasePath, "content.db path")
	outputPath := flag.String("out", filepath.Join("data", "generated", "npc.json"), "generated JSON path")
	lookupPath := flag.String("lookup", filepath.Join("static", "data", "npc"), "compact lookup directory")
	contentPath := flag.String("content", filepath.Join("content", "npc"), "generated NPC content directory")
	flag.Parse()
	database, err := catalog.Open(*databasePath)
	if err != nil {
		fatal(err)
	}
	defer database.Close()
	result, err := loadNPCs(database)
	if err != nil {
		fatal(err)
	}
	if err = enrichNPCImages(database, &result, *gamePath); err != nil {
		fatal(err)
	}
	scaling, err := enrichNPCCombat(database, &result)
	if err != nil {
		fatal(err)
	}
	if err = catalog.WriteJSON(filepath.Join(*lookupPath, "scaling.json"), scaling); err != nil {
		fatal(err)
	}
	err = catalog.WriteJSON(*outputPath, result)
	if err != nil {
		fatal(err)
	}
	err = writeLookup(*lookupPath, result)
	if err != nil {
		fatal(err)
	}
	err = writeContent(*contentPath, result.Entry)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("npcs: wrote %d unique level placements across %d levels to %s\n", result.Count, result.LevelCount, *outputPath)
}

func writeContent(path string, entries map[string]npcDetail) error {
	err := pruneContent(path, entries)
	if err != nil {
		return err
	}
	for slug, entry := range entries {
		contents := fmt.Sprintf("+++\ntitle = %q\nnpc = %q\ngenerated = true\n+++\n", entry.Name, slug)
		err = catalog.WriteText(filepath.Join(path, slug+".md"), contents)
		if err != nil {
			return fmt.Errorf("contentWrite[%s]: %w", slug, err)
		}
	}
	return nil
}

func pruneContent(path string, entries map[string]npcDetail) error {
	files, err := os.ReadDir(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("contentRead: %w", err)
	}
	for _, file := range files {
		if file.IsDir() || file.Name() == "_index.md" || filepath.Ext(file.Name()) != ".md" {
			continue
		}
		slug := strings.TrimSuffix(file.Name(), ".md")
		if _, keep := entries[slug]; keep {
			continue
		}
		contents, readErr := os.ReadFile(filepath.Join(path, file.Name()))
		if readErr != nil {
			return fmt.Errorf("contentInspect[%s]: %w", file.Name(), readErr)
		}
		if !strings.Contains(string(contents), "npc =") {
			continue
		}
		if removeErr := os.Remove(filepath.Join(path, file.Name())); removeErr != nil {
			return fmt.Errorf("contentPrune[%s]: %w", file.Name(), removeErr)
		}
	}
	return nil
}

func writeLookup(path string, result npcCatalog) error {
	rows := make([][]any, 0, result.EntryCount)
	for _, level := range result.Levels {
		err := catalog.WriteCompactJSON(filepath.Join(path, level.Slug+".json"), level)
		if err != nil {
			return fmt.Errorf("levelWrite[%s]: %w", level.Slug, err)
		}
	}
	slugs := make([]string, 0, len(result.Entry))
	for slug := range result.Entry {
		slugs = append(slugs, slug)
	}
	sort.Slice(slugs, func(i, j int) bool {
		a, b := strings.ToLower(result.Entry[slugs[i]].Name), strings.ToLower(result.Entry[slugs[j]].Name)
		if a == b {
			return slugs[i] < slugs[j]
		}
		return a < b
	})
	for _, slug := range slugs {
		entry := result.Entry[slug]
		levels := make([]string, 0, len(entry.Placements))
		sourceSet := make(map[string]struct{})
		for _, placement := range entry.Placements {
			levels = append(levels, placement.Level)
			for _, source := range placement.Sources {
				sourceSet[source] = struct{}{}
			}
		}
		sources := make([]string, 0, len(sourceSet))
		for source := range sourceSet {
			sources = append(sources, source)
		}
		sort.Strings(sources)
		rows = append(rows, []any{
			entry.Name, entry.AssetName, strings.Join(levels, ", "), entry.HitPoints,
			entry.IsCombatant, strings.Join(sources, ","), entry.Slug, entry.Category, entry.Element, entry.Image, entry.PlayerCountHealthScale, entry.HealthScaling,
		})
	}
	err := catalog.WriteCompactJSON(filepath.Join(path, "index.json"), rows)
	if err != nil {
		return fmt.Errorf("indexWrite: %w", err)
	}
	return nil
}

// Import every decoded non-player noun, including unplaced summons and fixtures.
func loadNPCs(database *sql.DB) (npcCatalog, error) {
	rows, err := database.Query(`SELECT n.noun_name, c.instance_id, c.hit_point, c.display_name, c.description, c.npc_type, c.is_player_pet, c.creature_type, nav.noun_type
 FROM non_player_noun n
 JOIN non_player_class c ON c.content_source_resource_id=n.non_player_class_resource_id
 JOIN noun_navigation nav ON nav.content_source_resource_id=n.content_source_resource_id
 WHERE c.is_player_pet=0 AND c.hit_point>0
 AND (nav.noun_type=113409280 OR c.npc_type IN (3,4))
 ORDER BY n.noun_name COLLATE NOCASE`)
	if err != nil {
		return npcCatalog{}, err
	}
	result := npcCatalog{Entry: make(map[string]npcDetail), Levels: []levelGroup{}}
	byName := make(map[string]string)
	for rows.Next() {
		var name, display, description string
		var id uint32
		var hp float64
		var kind, pet, element int
		var nounType uint32
		if err = rows.Scan(&name, &id, &hp, &display, &description, &kind, &pet, &element, &nounType); err != nil {
			rows.Close()
			return result, err
		}
		category := "combat"
		elementName := map[int]string{0: "cyber", 1: "quantum", 2: "bio", 3: "plasma", 4: "necro"}[element]
		if kind == 3 || kind == 4 || strings.HasPrefix(strings.ToLower(name), "dest_") || strings.Contains(strings.ToLower(name), "destructible") {
			category = "destructables"
		}
		if display == "" {
			display = catalog.DisplayName(name)
		}
		slug := catalog.Slug(name)
		image := ""
		for _, ext := range []string{".webp", ".png"} {
			if info, e := os.Stat(filepath.Join("static", "image", "npc", slug+ext)); e == nil && info.Size() > 0 {
				image = "/image/npc/" + slug + ext
				break
			}
		}
		result.Entry[slug] = npcDetail{AssetName: name, Name: display, Slug: slug, InstanceID: id, HitPoints: &hp, Category: category, Element: elementName, Image: image, Description: description, IsCombatant: category == "combat", Placements: []npcPlacement{}}
		byName[strings.ToLower(name)] = slug
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return result, err
	}
	rows.Close()
	levels, err := loadLevels(database)
	if err != nil {
		return result, err
	}
	placements, err := loadPlacements(database)
	if err != nil {
		return result, err
	}
	for _, level := range levels {
		group := levelGroup{ID: level.ID, Name: level.Name, Slug: catalog.Slug(level.Name), NPCs: []npc{}}
		for asset, sources := range placements[level.ID] {
			slug, ok := byName[strings.ToLower(asset)]
			if !ok {
				continue
			}
			detail := result.Entry[slug]
			sourceList := []string{}
			for source := range sources {
				sourceList = append(sourceList, source)
			}
			sort.Strings(sourceList)
			detail.Placements = append(detail.Placements, npcPlacement{Level: level.Name, LevelSlug: group.Slug, Sources: sourceList})
			result.Entry[slug] = detail
			group.NPCs = append(group.NPCs, npc{AssetName: detail.AssetName, Name: detail.Name, Slug: slug, InstanceID: detail.InstanceID, HitPoints: detail.HitPoints, IsCombatant: detail.IsCombatant, Category: detail.Category, Element: detail.Element, Image: detail.Image, Description: detail.Description, Sources: sourceList})
			if detail.IsCombatant {
				group.CombatCount++
			}
		}
		sort.Slice(group.NPCs, func(i, j int) bool { return strings.ToLower(group.NPCs[i].Name) < strings.ToLower(group.NPCs[j].Name) })
		group.NPCCount = len(group.NPCs)
		if group.NPCCount > 0 {
			result.Levels = append(result.Levels, group)
			result.Count += group.NPCCount
		}
	}
	for _, entry := range result.Entry {
		if entry.IsCombatant {
			result.CombatCount++
		}
	}
	result.EntryCount = len(result.Entry)
	result.LevelCount = len(result.Levels)
	return result, nil
}

func loadLevels(database *sql.DB) ([]levelIndex, error) {
	rows, err := database.Query(`SELECT id, name FROM level ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, fmt.Errorf("levelQuery: %w", err)
	}
	defer rows.Close()
	levels := make([]levelIndex, 0, 61)
	for rows.Next() {
		var level levelIndex
		err = rows.Scan(&level.ID, &level.Name)
		if err != nil {
			return nil, fmt.Errorf("levelScan: %w", err)
		}
		levels = append(levels, level)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("levelRows: %w", err)
	}
	return levels, nil
}

func loadPlacements(database *sql.DB) (map[int64]map[string]map[string]struct{}, error) {
	rows, err := database.Query(`
		SELECT level_id, noun_name, source FROM (
			SELECT level_marker_set.level_id AS level_id, marker.noun_name AS noun_name, 'marker' AS source
			FROM marker JOIN level_marker_set ON level_marker_set.id=marker.level_marker_set_id
			UNION ALL
			SELECT level_id, noun_name, 'director' AS source FROM level_director_entry
		)
		WHERE noun_name <> '' AND noun_name LIKE '%.Noun'`)
	if err != nil {
		return nil, fmt.Errorf("placementQuery: %w", err)
	}
	defer rows.Close()
	npcByLevel := make(map[int64]map[string]map[string]struct{})
	for rows.Next() {
		var levelID int64
		var assetName, source string
		err = rows.Scan(&levelID, &assetName, &source)
		if err != nil {
			return nil, fmt.Errorf("placementScan: %w", err)
		}
		if strings.HasPrefix(strings.ToLower(assetName), "pc_") {
			continue
		}
		if npcByLevel[levelID] == nil {
			npcByLevel[levelID] = make(map[string]map[string]struct{})
		}
		if npcByLevel[levelID][assetName] == nil {
			npcByLevel[levelID][assetName] = make(map[string]struct{})
		}
		npcByLevel[levelID][assetName][source] = struct{}{}
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("placementRows: %w", err)
	}
	return npcByLevel, nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
