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
	AssetName   string         `json:"asset_name"`
	Name        string         `json:"name"`
	Slug        string         `json:"slug"`
	InstanceID  uint32         `json:"instance_id"`
	HitPoints   *float64       `json:"hit_points,omitempty"`
	IsCombatant bool           `json:"is_combatant"`
	Placements  []npcPlacement `json:"placements"`
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
	sort.Strings(slugs)
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
			true, strings.Join(sources, ","), entry.Slug,
		})
	}
	err := catalog.WriteCompactJSON(filepath.Join(path, "index.json"), rows)
	if err != nil {
		return fmt.Errorf("indexWrite: %w", err)
	}
	return nil
}

func loadNPCs(database *sql.DB) (npcCatalog, error) {
	hitPointByID, err := loadHitPoints(database)
	if err != nil {
		return npcCatalog{}, fmt.Errorf("hitPointsLoad: %w", err)
	}
	levels, err := loadLevels(database)
	if err != nil {
		return npcCatalog{}, fmt.Errorf("levelsLoad: %w", err)
	}
	npcByLevel, err := loadPlacements(database)
	if err != nil {
		return npcCatalog{}, fmt.Errorf("placementsLoad: %w", err)
	}
	result := npcCatalog{Levels: make([]levelGroup, 0, len(levels)), Entry: make(map[string]npcDetail)}
	uniqueCombatant := make(map[uint32]struct{})
	for _, level := range levels {
		placement := npcByLevel[level.ID]
		if len(placement) == 0 {
			continue
		}
		group := levelGroup{ID: level.ID, Name: level.Name, Slug: catalog.Slug(level.Name), NPCs: make([]npc, 0, len(placement))}
		for assetName, sourceSet := range placement {
			instanceID, hitPoint, isCombatant := combatIdentity(assetName, hitPointByID)
			if !isCombatant {
				continue
			}
			sources := make([]string, 0, len(sourceSet))
			for source := range sourceSet {
				sources = append(sources, source)
			}
			sort.Strings(sources)
			entry := npc{AssetName: assetName, Name: catalog.DisplayName(assetName), Slug: catalog.Slug(assetName), InstanceID: instanceID, IsCombatant: isCombatant, Sources: sources}
			entry.HitPoints = &hitPoint
			group.CombatCount++
			uniqueCombatant[instanceID] = struct{}{}
			group.NPCs = append(group.NPCs, entry)
			detail := result.Entry[entry.Slug]
			if detail.Name == "" {
				detail = npcDetail{AssetName: entry.AssetName, Name: entry.Name, Slug: entry.Slug, InstanceID: entry.InstanceID, HitPoints: entry.HitPoints, IsCombatant: entry.IsCombatant}
			}
			detail.Placements = append(detail.Placements, npcPlacement{Level: level.Name, LevelSlug: catalog.Slug(level.Name), Sources: entry.Sources})
			result.Entry[entry.Slug] = detail
		}
		sort.Slice(group.NPCs, func(left, right int) bool {
			return strings.ToLower(group.NPCs[left].Name) < strings.ToLower(group.NPCs[right].Name)
		})
		group.NPCCount = len(group.NPCs)
		if group.NPCCount == 0 {
			continue
		}
		result.Count += group.NPCCount
		result.Levels = append(result.Levels, group)
	}
	result.CombatCount = len(uniqueCombatant)
	result.EntryCount = len(result.Entry)
	result.LevelCount = len(result.Levels)
	return result, nil
}

func combatIdentity(assetName string, hitPointByID map[uint32]float64) (uint32, float64, bool) {
	baseName := assetName
	if strings.HasSuffix(strings.ToLower(baseName), ".noun") {
		baseName = baseName[:len(baseName)-len(".Noun")]
	}
	candidates := []string{
		assetName,
		baseName + ".NonPlayerClass",
		baseName + ".ClassAttributes",
		baseName,
		baseName + "_Captain.Noun",
		baseName + "_Captain.NonPlayerClass",
		baseName + "_Captain.ClassAttributes",
		baseName + "_Captain",
	}
	for _, candidate := range candidates {
		instanceID := catalog.HashID(candidate)
		hitPoint, isFound := hitPointByID[instanceID]
		if isFound {
			return instanceID, hitPoint, true
		}
	}
	return catalog.HashID(assetName), 0, false
}

func loadHitPoints(database *sql.DB) (map[uint32]float64, error) {
	rows, err := database.Query(`SELECT instance_id, hit_point FROM non_player_class`)
	if err != nil {
		return nil, fmt.Errorf("hitPointQuery: %w", err)
	}
	defer rows.Close()
	hitPointByID := make(map[uint32]float64, 624)
	for rows.Next() {
		var instanceID uint32
		var hitPoint float64
		err = rows.Scan(&instanceID, &hitPoint)
		if err != nil {
			return nil, fmt.Errorf("hitPointScan: %w", err)
		}
		hitPointByID[instanceID] = hitPoint
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("hitPointRows: %w", err)
	}
	return hitPointByID, nil
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
