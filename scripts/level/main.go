package main

import (
	"database/sql"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/darkspinnet/web/scripts/internal/catalog"
)

type levelReport struct {
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
	notesPath := flag.String("campaign-notes", filepath.Join("..", "darkspin", "notes", "campaign"), "campaign description audit path")
	mapPath := flag.String("campaign-maps", filepath.Join("static", "maps"), "rendered campaign map path")
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
	err = enrichCampaignLevels(database, result.Levels, *notesPath)
	if err != nil {
		fatal(err)
	}
	err = enrichLevelMaps(database, result.Levels, *mapPath)
	if err != nil {
		fatal(err)
	}
	result.Groups = buildGroups(result.Levels)
	result.Entry = make(map[string]levelReport, len(result.Levels))
	for _, report := range result.Levels {
		result.Entry[report.Slug] = report
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
		       level.camera_pitch, level.camera_yaw, level.camera_distance,
		       (SELECT COUNT(*) FROM level_marker_set WHERE level_id=level.id),
		       (SELECT COUNT(*) FROM marker JOIN level_marker_set ON level_marker_set.id=marker.level_marker_set_id WHERE level_marker_set.level_id=level.id),
		       (SELECT COUNT(*) FROM level_event JOIN marker ON marker.id=level_event.marker_id JOIN level_marker_set ON level_marker_set.id=marker.level_marker_set_id WHERE level_marker_set.level_id=level.id),
		       (SELECT COUNT(*) FROM level_script WHERE level_id=level.id)
		FROM level ORDER BY level.name COLLATE NOCASE`)
	if err != nil {
		return levelCatalog{}, fmt.Errorf("levelQuery: %w", err)
	}
	defer rows.Close()
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

func buildGroups(levels []levelReport) []levelGroup {
	groups := make([]levelGroup, 0, 7)
	for number := 1; number <= 6; number++ {
		groups = append(groups, levelGroup{Name: "Level " + strconv.Itoa(number), Slug: strconv.Itoa(number)})
	}
	groups = append(groups, levelGroup{Name: "Other", Slug: "other"})
	for index := range levels {
		report := &levels[index]
		report.DisplayName = catalog.DisplayName(report.Name)
		if strings.Contains(strings.ToLower(report.Name), "tutorial") {
			report.DisplayName = "Tutorial"
			report.GroupName = "Other"
			report.GroupSlug = "other"
			groups[6].Levels = append(groups[6].Levels, *report)
			continue
		}
		if len(report.CampaignOrders) == 0 || report.CampaignOrders[0] < 1 || report.CampaignOrders[0] > 24 {
			report.GroupName = "Other"
			report.GroupSlug = "other"
			groups[6].Levels = append(groups[6].Levels, *report)
			continue
		}
		position := report.CampaignOrders[0]
		groupNumber := (position-1)/4 + 1
		stage := (position-1)%4 + 1
		report.DisplayName = fmt.Sprintf("%d-%d", groupNumber, stage)
		report.GroupName = fmt.Sprintf("Level %d", groupNumber)
		report.GroupSlug = strconv.Itoa(groupNumber)
		report.Stage = stage
		groups[groupNumber-1].Levels = append(groups[groupNumber-1].Levels, *report)
	}
	for index := range groups {
		if groups[index].Slug == "other" {
			sort.Slice(groups[index].Levels, func(left, right int) bool {
				leftTutorial := groups[index].Levels[left].DisplayName == "Tutorial"
				rightTutorial := groups[index].Levels[right].DisplayName == "Tutorial"
				if leftTutorial != rightTutorial {
					return leftTutorial
				}
				return strings.ToLower(groups[index].Levels[left].DisplayName) < strings.ToLower(groups[index].Levels[right].DisplayName)
			})
		} else {
			sort.Slice(groups[index].Levels, func(left, right int) bool {
				return groups[index].Levels[left].Stage < groups[index].Levels[right].Stage
			})
		}
		groups[index].Count = len(groups[index].Levels)
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
	regions := []string{"Cryos", "Nocturna", "Scaldron", "Verdanth", "Zelem"}
	for _, region := range regions {
		if strings.Contains(identity, strings.ToLower(region)) {
			return region
		}
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
