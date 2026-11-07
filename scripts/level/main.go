package main

import (
	"database/sql"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/darkspinnet/web/scripts/internal/catalog"
)

type levelReport struct {
	Mode            string          `json:"mode,omitempty"`
	Slot            string          `json:"slot,omitempty"`
	Progression     int             `json:"progression,omitempty"`
	Previous        string          `json:"previous,omitempty"`
	Next            string          `json:"next,omitempty"`
	ID              int64           `json:"id"`
	Name            string          `json:"name"`
	DisplayName     string          `json:"display_name"`
	Slug            string          `json:"slug"`
	GroupName       string          `json:"group_name,omitempty"`
	GroupSlug       string          `json:"group_slug,omitempty"`
	Stage           int             `json:"stage,omitempty"`
	Region          string          `json:"region"`
	CampaignOrders  []int           `json:"campaign_orders"`
	Music           string          `json:"music"`
	NavigationMesh  string          `json:"navigation_mesh"`
	PhysicsMesh     string          `json:"physics_mesh"`
	RenderingConfig string          `json:"rendering_config"`
	PlanetConfig    string          `json:"planet_config"`
	PrimaryType     uint32          `json:"primary_type"`
	SecondaryType   uint32          `json:"secondary_type"`
	CameraPitch     *float64        `json:"camera_pitch,omitempty"`
	CameraYaw       *float64        `json:"camera_yaw,omitempty"`
	CameraDistance  *float64        `json:"camera_distance,omitempty"`
	MarkerSetCount  int             `json:"marker_set_count"`
	MarkerCount     int             `json:"marker_count"`
	EventCount      int             `json:"event_count"`
	ScriptCount     int             `json:"script_count"`
	NPCCount        int             `json:"npc_count"`
	NPCs            []string        `json:"npcs"`
	Aliases         []string        `json:"aliases"`
	Map             *campaignMap    `json:"map,omitempty"`
	Campaign        *campaignDetail `json:"campaign,omitempty"`
	Replay          *replayDetail   `json:"replay,omitempty"`
}

type levelGroup struct {
	Name   string        `json:"name"`
	Slug   string        `json:"slug"`
	Count  int           `json:"count"`
	Levels []levelReport `json:"levels"`
}

type levelCatalog struct {
	Count         int                    `json:"count"`
	PlayableCount int                    `json:"playable_count"`
	GroupCount    int                    `json:"group_count"`
	CampaignCount int                    `json:"campaign_count"`
	Regions       map[string]int         `json:"regions"`
	Groups        []levelGroup           `json:"groups"`
	Levels        []levelReport          `json:"levels"`
	Entry         map[string]levelReport `json:"entry"`
}

func main() {
	databasePath := flag.String("db", catalog.DefaultDatabasePath, "content.db path")
	gamePath := flag.String("game", catalog.DefaultGamePath, "installed client root for creature tooltip descriptions")
	mapPath := flag.String("campaign-maps", filepath.Join("static", "maps"), "rendered campaign map path")
	runSeed := flag.Uint64("campaign-seed", 1, "nonzero run seed for solo first-clear campaign maps")
	outputPath := flag.String("out", filepath.Join("data", "generated", "level.json"), "generated JSON path")
	lookupPath := flag.String("lookup", filepath.Join("static", "data", "level"), "compact report directory")
	contentPath := flag.String("content", filepath.Join("content", "level"), "generated level content directory")
	flag.Parse()
	database, err := catalog.Open(*databasePath)
	if err != nil {
		fatal(err)
	}
	defer database.Close()
	result, err := loadLevelReports(database)
	if err != nil {
		fatal(err)
	}
	result.Levels = expandProgression(result.Levels)
	descriptions, err := catalog.LoadNPCDescriptions(database, *gamePath)
	if err != nil {
		fatal(err)
	}
	err = enrichCampaignLevels(database, result.Levels, descriptions)
	if err != nil {
		fatal(err)
	}
	if err = enrichRecommendedSquadLevels(*gamePath, result.Levels); err != nil {
		fatal(err)
	}
	err = enrichLevelMaps(database, result.Levels, *mapPath)
	if err != nil {
		fatal(err)
	}
	if err = generateSeededMaps(database, *databasePath, *mapPath, *runSeed, result.Levels, descriptions); err != nil {
		fatal(err)
	}
	replays, err := loadReplayLevels(*databasePath, *mapPath, result.Levels)
	if err != nil {
		fatal(err)
	}
	result.Levels = append(result.Levels, replays...)
	result.Groups = buildGroups(result.Levels)
	result.Count = len(result.Levels)
	result.GroupCount = len(result.Groups)
	result.CampaignCount = 0
	result.Entry = make(map[string]levelReport, len(result.Levels))
	for _, report := range result.Levels {
		result.Entry[report.Slug] = report
		if report.Progression > 0 {
			result.CampaignCount++
		}
	}
	if result.CampaignCount != 72 {
		fatal(fmt.Errorf("campaign progression: got %d missions, want 72", result.CampaignCount))
	}
	err = catalog.WriteJSON(*outputPath, result)
	if err != nil {
		fatal(err)
	}
	for _, report := range result.Levels {
		err = catalog.WriteCompactJSON(filepath.Join(*lookupPath, report.Slug+".json"), report)
		if err != nil {
			fatal(fmt.Errorf("reportWrite[%s]: %w", report.Slug, err))
		}
	}
	err = writeContent(*contentPath, result.Levels)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("levels: wrote %d reports to %s\n", result.Count, *outputPath)
}

func writeContent(path string, levels []levelReport) error {
	for _, level := range levels {
		title := level.DisplayName
		if title == "" {
			title = level.Name
		}
		if level.Slot != "" {
			title = fmt.Sprintf("%s %s · %s", level.Mode, level.Slot, title)
		} else if level.Replay != nil {
			title = fmt.Sprintf("Replay · %s", title)
		}
		contents := fmt.Sprintf("+++\ntitle = %q\nlevel = %q\ngenerated = true\n+++\n", title, level.Slug)
		if err := catalog.WriteText(filepath.Join(path, level.Slug+".md"), contents); err != nil {
			return fmt.Errorf("contentWrite[%s]: %w", level.Slug, err)
		}
	}
	return nil
}

func loadLevelReports(database *sql.DB) (levelCatalog, error) {
	combatantIDs, err := loadCombatantIDs(database)
	if err != nil {
		return levelCatalog{}, fmt.Errorf("combatantsLoad: %w", err)
	}
	rows, err := database.Query(`
		SELECT level.id, level.name, level.music, level.nav_mesh, level.physics_mesh,
		       level.rendering_config, level.planet_config, level.primary_type, level.secondary_type,
		       COALESCE(level.camera_pitch,0), COALESCE(level.camera_yaw,0), COALESCE(level.camera_distance,0),
		       (SELECT COUNT(*) FROM level_marker_set WHERE level_id=level.id),
		       (SELECT COUNT(*) FROM marker JOIN level_marker_set ON level_marker_set.id=marker.level_marker_set_id WHERE level_marker_set.level_id=level.id),
		       (SELECT COUNT(*) FROM level_event JOIN marker ON marker.id=level_event.marker_id JOIN level_marker_set ON level_marker_set.id=marker.level_marker_set_id WHERE level_marker_set.level_id=level.id),
		       (SELECT COUNT(*) FROM level_script WHERE level_id=level.id)
		FROM level ORDER BY level.name COLLATE NOCASE`)
	if err != nil {
		return levelCatalog{}, fmt.Errorf("levelQuery: %w", err)
	}
	defer rows.Close()
	titles, err := loadLevelTitles(database)
	if err != nil {
		return levelCatalog{}, err
	}
	result := levelCatalog{Regions: map[string]int{}, Levels: make([]levelReport, 0, 61), Entry: make(map[string]levelReport)}
	for rows.Next() {
		var report levelReport
		var pitch, yaw, distance float64
		err = rows.Scan(&report.ID, &report.Name, &report.Music, &report.NavigationMesh, &report.PhysicsMesh,
			&report.RenderingConfig, &report.PlanetConfig, &report.PrimaryType, &report.SecondaryType,
			&pitch, &yaw, &distance, &report.MarkerSetCount, &report.MarkerCount, &report.EventCount, &report.ScriptCount)
		if err != nil {
			return levelCatalog{}, fmt.Errorf("levelScan: %w", err)
		}
		report.Slug = catalog.Slug(report.Name)
		report.DisplayName = titles[report.ID]
		if report.DisplayName == "" {
			report.DisplayName = catalog.DisplayName(report.Name)
			if strings.Contains(strings.ToLower(report.Name), "tutorial") {
				report.DisplayName = "Tutorial"
			}
		}
		report.Region = levelRegion(report.Name, report.PlanetConfig)
		report.CameraPitch = sensibleCamera(pitch)
		report.CameraYaw = sensibleCamera(yaw)
		report.CameraDistance = sensibleCamera(distance)
		report.Aliases, err = loadStrings(database, `SELECT alias FROM level_alias WHERE level_id=? ORDER BY alias`, report.ID)
		if err != nil {
			return levelCatalog{}, fmt.Errorf("aliasesLoad[%d]: %w", report.ID, err)
		}
		nouns, loadErr := loadStrings(database, `
			SELECT DISTINCT noun_name FROM (
				SELECT marker.noun_name AS noun_name FROM marker
				JOIN level_marker_set ON level_marker_set.id=marker.level_marker_set_id
				WHERE level_marker_set.level_id=?
				UNION SELECT noun_name FROM level_director_entry WHERE level_id=?
			) WHERE noun_name <> '' AND noun_name LIKE '%.Noun' ORDER BY noun_name`, report.ID, report.ID)
		if loadErr != nil {
			return levelCatalog{}, fmt.Errorf("npcsLoad[%d]: %w", report.ID, loadErr)
		}
		report.NPCs = filterCombatants(nouns, combatantIDs)
		report.NPCCount = len(report.NPCs)
		result.Regions[report.Region]++
		result.Levels = append(result.Levels, report)
	}
	err = rows.Err()
	if err != nil {
		return levelCatalog{}, fmt.Errorf("levelRows: %w", err)
	}
	campaignOrder, err := loadCampaignOrder(database)
	if err != nil {
		return levelCatalog{}, fmt.Errorf("campaignLoad: %w", err)
	}
	for index := range result.Levels {
		result.Levels[index].CampaignOrders = campaignOrder[result.Levels[index].ID]
	}
	result.Groups = buildGroups(result.Levels)
	for _, group := range result.Groups {
		result.PlayableCount += group.Count
	}
	result.GroupCount = len(result.Groups)
	for _, report := range result.Levels {
		result.Entry[report.Slug] = report
	}
	result.Count = len(result.Levels)
	result.CampaignCount = len(campaignOrder)
	return result, nil
}

func expandProgression(levels []levelReport) []levelReport {
	result := make([]levelReport, 0, len(levels)+48)
	missions := make(map[int]string)
	modes := []string{"Onslaught", "Invasion", "Apocalypse"}
	for _, level := range levels {
		if len(level.CampaignOrders) == 0 || level.CampaignOrders[0] > 24 {
			result = append(result, level)
			continue
		}
		for _, stage := range level.CampaignOrders {
			if stage < 1 || stage > 72 {
				continue
			}
			pass := (stage - 1) / 24
			mode := modes[pass]
			mission := level
			mission.Progression = stage
			mission.Mode = mode
			mission.Slot = fmt.Sprintf("%d-%d", (stage-1)/4+1, (stage-1)%4+1)
			if pass > 0 {
				mission.Slug = strings.ToLower(mode) + "-" + level.Slug
			}
			mission.CampaignOrders = []int{mission.Progression}
			missions[mission.Progression] = mission.Slug
			result = append(result, mission)
		}
	}
	for i := range result {
		if n := result[i].Progression; n > 0 {
			result[i].Previous = missions[n-1]
			result[i].Next = missions[n+1]
		}
	}
	return result
}

func buildGroups(levels []levelReport) []levelGroup {
	groups := []levelGroup{{Name: "Onslaught", Slug: "onslaught"}, {Name: "Invasion", Slug: "invasion"}, {Name: "Apocalypse", Slug: "apocalypse"}}
	replay := levelGroup{Name: "Replay", Slug: "replay"}
	other := levelGroup{Name: "Other", Slug: "other"}
	for i := range levels {
		report := &levels[i]
		if report.Replay != nil {
			report.GroupName = replay.Name
			report.GroupSlug = replay.Slug
			replay.Levels = append(replay.Levels, *report)
		} else if report.Mode != "" {
			group := (report.Progression - 1) / 24
			report.GroupName = report.Mode
			report.GroupSlug = groups[group].Slug
			report.Stage = report.Progression
			groups[group].Levels = append(groups[group].Levels, *report)
		} else if len(report.CampaignOrders) > 0 && report.CampaignOrders[0] <= 24 {
			// Base map identity before expansion.
			groups[0].Levels = append(groups[0].Levels, *report)
		} else {
			report.GroupName = other.Name
			report.GroupSlug = other.Slug
			other.Levels = append(other.Levels, *report)
		}
	}
	if len(replay.Levels) > 0 {
		groups = append(groups, replay)
	}
	groups = append(groups, other)
	for i := range groups {
		sort.Slice(groups[i].Levels, func(a, b int) bool {
			if groups[i].Slug == "other" || groups[i].Slug == "replay" {
				return groups[i].Levels[a].DisplayName < groups[i].Levels[b].DisplayName
			}
			return groups[i].Levels[a].Stage < groups[i].Levels[b].Stage
		})
		groups[i].Count = len(groups[i].Levels)
	}
	return groups
}

func loadCombatantIDs(database *sql.DB) (map[uint32]struct{}, error) {
	rows, err := database.Query(`SELECT instance_id FROM non_player_class`)
	if err != nil {
		return nil, fmt.Errorf("combatantQuery: %w", err)
	}
	defer rows.Close()
	ids := make(map[uint32]struct{}, 624)
	for rows.Next() {
		var id uint32
		if err = rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("combatantScan: %w", err)
		}
		ids[id] = struct{}{}
	}
	return ids, rows.Err()
}

func filterCombatants(nouns []string, ids map[uint32]struct{}) []string {
	combatants := make([]string, 0, len(nouns))
	for _, assetName := range nouns {
		baseName := strings.TrimSuffix(assetName, ".Noun")
		candidates := []string{assetName, baseName + ".NonPlayerClass", baseName + ".ClassAttributes", baseName}
		for _, candidate := range candidates {
			if _, found := ids[catalog.HashID(candidate)]; found {
				combatants = append(combatants, assetName)
				break
			}
		}
	}
	return combatants
}

func loadStrings(database *sql.DB, query string, arguments ...any) ([]string, error) {
	rows, err := database.Query(query, arguments...)
	if err != nil {
		return nil, fmt.Errorf("stringQuery: %w", err)
	}
	defer rows.Close()
	items := make([]string, 0)
	for rows.Next() {
		var item string
		err = rows.Scan(&item)
		if err != nil {
			return nil, fmt.Errorf("stringScan: %w", err)
		}
		items = append(items, item)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("stringRows: %w", err)
	}
	return items, nil
}

func loadCampaignOrder(database *sql.DB) (map[int64][]int, error) {
	rows, err := database.Query(`SELECT level_id, ordinal FROM chain_level WHERE level_id IS NOT NULL ORDER BY ordinal`)
	if err != nil {
		return nil, fmt.Errorf("campaignQuery: %w", err)
	}
	defer rows.Close()
	orderByLevel := make(map[int64][]int)
	for rows.Next() {
		var levelID int64
		var ordinal int
		err = rows.Scan(&levelID, &ordinal)
		if err != nil {
			return nil, fmt.Errorf("campaignScan: %w", err)
		}
		orderByLevel[levelID] = append(orderByLevel[levelID], ordinal+1)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("campaignRows: %w", err)
	}
	for levelID := range orderByLevel {
		sort.Ints(orderByLevel[levelID])
	}
	return orderByLevel, nil
}

func sensibleCamera(number float64) *float64 {
	if math.IsNaN(number) || math.IsInf(number, 0) || math.Abs(number) > 10000 || (number != 0 && math.Abs(number) < 0.0001) {
		return nil
	}
	return &number
}

func levelRegion(name, planetConfig string) string {
	identity := strings.ToLower(name + " " + planetConfig)
	regions := []string{"Cryos", "Infinity", "Nocturna", "Scaldron", "Verdanth", "Zelem"}
	for _, region := range regions {
		if strings.Contains(identity, strings.ToLower(region)) {
			return region
		}
	}
	if strings.Contains(identity, "sentios") {
		return "Infinity"
	}
	if strings.Contains(identity, "tutorial") {
		return "Tutorial"
	}
	if strings.Contains(identity, "pvp") {
		return "PvP"
	}
	return "Other"
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
