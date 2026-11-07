package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	"github.com/darkspinnet/web/scripts/internal/catalog"
)

type campaignDetail struct {
	Difficulty          int             `json:"difficulty"`
	ExpectedAvatarLevel int             `json:"expected_avatar_level"`
	RecommendedSquadMin float32         `json:"recommended_squad_min"`
	RecommendedSquadMax float32         `json:"recommended_squad_max"`
	HealthMultiplier    float64         `json:"health_multiplier"`
	DamageMultiplier    float64         `json:"damage_multiplier"`
	Enemies             []campaignEnemy `json:"enemies"`
}

type campaignEnemy struct {
	Name                string               `json:"name"`
	Noun                string               `json:"noun"`
	Slug                string               `json:"slug"`
	Role                string               `json:"role"`
	ChallengeValue      int                  `json:"challenge_value"`
	BaseHitPoints       float64              `json:"base_hit_points"`
	EstimatedHitPoints  float64              `json:"estimated_hit_points"`
	EstimatedDamageMin  *float64             `json:"estimated_damage_min,omitempty"`
	EstimatedDamageMax  *float64             `json:"estimated_damage_max,omitempty"`
	EstimatedDamageType string               `json:"estimated_damage_type,omitempty"`
	Descriptions        []string             `json:"descriptions"`
	Behavior            string               `json:"behavior"`
	SeededVariants      []seededEnemyVariant `json:"seeded_variants,omitempty"`
}

type campaignMap struct {
	RunSeed         string               `json:"run_seed,omitempty"`
	PopulationSeed  uint32               `json:"population_seed,omitempty"`
	UnmappedSpawns  int                  `json:"unmapped_spawns,omitempty"`
	UnmappedObjects int                  `json:"unmapped_objects,omitempty"`
	Selection       string               `json:"selection"`
	Level           string               `json:"level"`
	Legend          map[string]string    `json:"legend"`
	Labels          map[string]string    `json:"labels"`
	Notes           []string             `json:"enemy_class_notes"`
	Sections        []campaignMapSection `json:"sections"`
	Markers         []campaignMapMarker  `json:"markers,omitempty"`
	Positions       []campaignPosition   `json:"positions,omitempty"`
}

type campaignMapSection struct {
	Spawns      []seededMapPoint  `json:"spawns,omitempty"`
	Objects     []seededMapObject `json:"objects,omitempty"`
	ComponentID uint32            `json:"component_id"`
	Image       string            `json:"image"`
	MarkerCount int               `json:"marker_count"`
	ImageURL    string            `json:"image_url"`
}

type campaignMapMarker struct {
	Category string  `json:"category"`
	Label    string  `json:"label,omitempty"`
	Name     string  `json:"name"`
	NounName string  `json:"noun_name,omitempty"`
	Section  uint32  `json:"section,omitempty"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Z        float64 `json:"z"`
}

type campaignPosition struct {
	ComponentID uint32                   `json:"component_id"`
	Anchor      string                   `json:"anchor"`
	ImageURL    string                   `json:"image_url"`
	MarkerCount int                      `json:"marker_count"`
	Signals     []campaignPositionSignal `json:"signals,omitempty"`
	Entries     []campaignPositionEntry  `json:"entries,omitempty"`
	Nouns       []campaignPositionNoun   `json:"nouns,omitempty"`
	Teleports   []campaignTeleportLink   `json:"teleports,omitempty"`
}

type campaignPositionSignal struct {
	Label   string `json:"label"`
	Meaning string `json:"meaning"`
	Count   int    `json:"count"`
}

type campaignPositionEntry struct {
	Name     string `json:"name"`
	Noun     string `json:"noun"`
	Category string `json:"category"`
	Count    int    `json:"count"`
}

type campaignPositionNoun struct {
	Name string `json:"name"`
	Noun string `json:"noun"`
	Slug string `json:"slug"`
	Pool string `json:"pool"`
}

type campaignTeleportLink struct {
	Label            string  `json:"label"`
	Direction        string  `json:"direction"`
	OtherComponentID uint32  `json:"other_component_id"`
	OtherAnchor      string  `json:"other_anchor"`
	X                float64 `json:"x"`
	Y                float64 `json:"y"`
	Z                float64 `json:"z"`
}

type campaignPoolNoun struct {
	Noun         string
	Pool         string
	IsHordeLegal bool
}

type campaignTeleportRoute struct {
	SourceX      float64
	SourceY      float64
	SourceZ      float64
	DestinationX float64
	DestinationY float64
	DestinationZ float64
}

type authoredEnemy struct {
	Name         string
	Noun         string
	Descriptions []string
	Behavior     string
}

func enrichCampaignLevels(database *sql.DB, levels []levelReport, descriptions map[uint32][]string) error {
	for index := range levels {
		level := &levels[index]
		if level.DisplayName == "" || len(level.CampaignOrders) == 0 || level.CampaignOrders[0] > 72 {
			continue
		}
		detail, err := loadCampaignDetail(database, level.Name, level.CampaignOrders[0], descriptions)
		if err != nil {
			return fmt.Errorf("campaign[%s]: %w", level.DisplayName, err)
		}
		level.Campaign = &detail
		level.NPCCount = len(detail.Enemies)
		level.NPCs = make([]string, 0, len(detail.Enemies))
		for _, enemy := range detail.Enemies {
			level.NPCs = append(level.NPCs, enemy.Noun+".Noun")
		}
	}
	return nil
}

func loadCampaignDetail(database *sql.DB, levelName string, difficulty int, descriptions map[uint32][]string) (campaignDetail, error) {
	var detail campaignDetail
	detail.Difficulty = difficulty
	err := database.QueryRow(`
		SELECT expected_avatar_level, health_multiplier, damage_multiplier
		FROM difficulty_tuning WHERE difficulty=?`, difficulty).Scan(
		&detail.ExpectedAvatarLevel, &detail.HealthMultiplier, &detail.DamageMultiplier,
	)
	if err != nil {
		return detail, fmt.Errorf("difficulty: %w", err)
	}
	// Resolve the active director pools for this difficulty; later passes change the roster.
	var levelID int64
	if err = database.QueryRow("SELECT id FROM level WHERE name=?", levelName).Scan(&levelID); err != nil {
		return detail, err
	}
	pools, err := loadCampaignPoolNouns(database, levelID, difficulty)
	if err != nil {
		return detail, err
	}
	authored := []authoredEnemy{}
	seen := map[string]bool{}
	for _, pool := range pools {
		if pool.Pool != "minion" && pool.Pool != "special" && pool.Pool != "agent" {
			continue
		}
		noun := pool.Noun
		if strings.HasSuffix(strings.ToLower(noun), ".noun") {
			noun = noun[:len(noun)-5]
		}
		if seen[strings.ToLower(noun)] {
			continue
		}
		seen[strings.ToLower(noun)] = true
		var display, description string
		queryErr := database.QueryRow(`SELECT c.display_name,c.description FROM non_player_noun n JOIN non_player_class c ON c.content_source_resource_id=n.non_player_class_resource_id WHERE n.noun_name=? COLLATE NOCASE`, pool.Noun).Scan(&display, &description)
		if errors.Is(queryErr, sql.ErrNoRows) {
			queryErr = database.QueryRow("SELECT display_name,description FROM non_player_class WHERE noun_name=? COLLATE NOCASE", pool.Noun).Scan(&display, &description)
		}
		if queryErr != nil {
			return detail, fmt.Errorf("noun[%s]: %w", pool.Noun, queryErr)
		}
		if display == "" {
			display = catalog.DisplayName(noun)
		}
		source := authoredEnemy{Name: display, Noun: noun}
		if description != "" {
			source.Descriptions = []string{description}
		}
		authored = append(authored, source)
	}
	detail.Enemies = make([]campaignEnemy, 0, len(authored))
	for _, source := range authored {
		enemy, enemyErr := buildCampaignEnemy(database, source, detail.HealthMultiplier, detail.DamageMultiplier, descriptions)
		if enemyErr != nil {
			return detail, fmt.Errorf("enemy[%s]: %w", source.Noun, enemyErr)
		}
		detail.Enemies = append(detail.Enemies, enemy)
	}
	return detail, nil
}

func enrichLevelMaps(database *sql.DB, levels []levelReport, mapPath string) error {
	entries, err := os.ReadDir(mapPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("mapDirectory: %w", err)
	}
	levelsByName := make(map[string][]*levelReport, len(levels))
	levelsBySlug := make(map[string]*levelReport, len(levels))
	for index := range levels {
		levelsByName[strings.ToLower(levels[index].Name)] = append(levelsByName[strings.ToLower(levels[index].Name)], &levels[index])
		levelsBySlug[levels[index].Slug] = &levels[index]
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		mapData, readErr := os.ReadFile(filepath.Join(mapPath, entry.Name(), "map.json"))
		if errors.Is(readErr, os.ErrNotExist) {
			continue
		}
		if readErr != nil {
			return fmt.Errorf("mapRead[%s]: %w", entry.Name(), readErr)
		}
		var levelMap campaignMap
		if err = json.Unmarshal(mapData, &levelMap); err != nil {
			return fmt.Errorf("mapDecode[%s]: %w", entry.Name(), err)
		}
		targets, isFound := levelsByName[strings.ToLower(levelMap.Level)]
		if !isFound {
			fallback, found := levelsBySlug[entry.Name()]
			isFound = found
			targets = []*levelReport{fallback}
		}
		if !isFound {
			return fmt.Errorf("mapLevel[%s]: level %q not found", entry.Name(), levelMap.Level)
		}
		for _, level := range targets {
			if level.Campaign != nil {
				continue
			}
			var levelMap campaignMap
			if err = json.Unmarshal(mapData, &levelMap); err != nil {
				return err
			}
			for index := range levelMap.Sections {
				levelMap.Sections[index].ImageURL = "/maps/" + entry.Name() + "/" + levelMap.Sections[index].Image
			}
			difficulty := 1
			if level.Campaign != nil {
				difficulty = level.Campaign.Difficulty
			}
			pools, poolErr := loadCampaignPoolNouns(database, level.ID, difficulty)
			if poolErr != nil {
				return fmt.Errorf("mapPools[%s]: %w", entry.Name(), poolErr)
			}
			teleports, teleportErr := loadCampaignTeleportRoutes(database, level.ID)
			if teleportErr != nil {
				return fmt.Errorf("mapTeleports[%s]: %w", entry.Name(), teleportErr)
			}
			levelMap.Positions = buildCampaignPositions(levelMap, pools, teleports)
			levelMap.Markers = nil
			level.Map = &levelMap
		}
	}
	return nil
}

func loadCampaignTeleportRoutes(database *sql.DB, levelID int64) ([]campaignTeleportRoute, error) {
	rows, err := database.Query(`
		SELECT source.position_x, source.position_y, source.position_z,
		       destination.position_x, destination.position_y, destination.position_z
		FROM marker AS source
		JOIN level_marker_set ON level_marker_set.id=source.level_marker_set_id
		JOIN marker AS destination
		  ON destination.level_marker_set_id=source.level_marker_set_id
		 AND destination.marker_id=source.target_marker_id
		WHERE level_marker_set.level_id=? AND source.target_marker_id<>0
		ORDER BY level_marker_set.ordinal, source.ordinal`, levelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]campaignTeleportRoute, 0, 12)
	for rows.Next() {
		var route campaignTeleportRoute
		if err = rows.Scan(&route.SourceX, &route.SourceY, &route.SourceZ,
			&route.DestinationX, &route.DestinationY, &route.DestinationZ); err != nil {
			return nil, err
		}
		result = append(result, route)
	}
	return result, rows.Err()
}

func loadCampaignPoolNouns(database *sql.DB, levelID int64, difficulty int) ([]campaignPoolNoun, error) {
	rows, err := database.Query(`
		SELECT configuration_ordinal, config_kind, noun_name, is_horde_legal
		FROM level_director_entry
		WHERE level_id=? AND minimum_difficulty<=? AND maximum_difficulty>=?
 AND (configuration_name='firstTimeConfig' OR NOT EXISTS (
 SELECT 1 FROM level_director_entry preferred WHERE preferred.level_id=level_director_entry.level_id
 AND preferred.config_kind=level_director_entry.config_kind AND preferred.configuration_name='firstTimeConfig'))
		ORDER BY configuration_ordinal, configuration_entry_ordinal`, levelID, difficulty, difficulty)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]campaignPoolNoun, 0, 16)
	for rows.Next() {
		var noun campaignPoolNoun
		var configurationOrdinal int
		var isHordeLegal int
		if err = rows.Scan(&configurationOrdinal, &noun.Pool, &noun.Noun, &isHordeLegal); err != nil {
			return nil, err
		}
		if noun.Pool == "" || strings.EqualFold(noun.Pool, "unknown") {
			noun.Pool = campaignPoolKind(configurationOrdinal)
		}
		noun.IsHordeLegal = isHordeLegal != 0
		result = append(result, noun)
	}
	return result, rows.Err()
}

func campaignPoolKind(configurationOrdinal int) string {
	switch configurationOrdinal {
	case 0:
		return "minion"
	case 1:
		return "special"
	case 2:
		return "agent"
	case 3:
		return "captain"
	default:
		return "unknown"
	}
}

var campaignTeleportLabelPattern = regexp.MustCompile(`^T([0-9]+)([AB])$`)

func buildCampaignPositions(
	levelMap campaignMap, pools []campaignPoolNoun, teleportRoutes []campaignTeleportRoute,
) []campaignPosition {
	positions := make(map[uint32]*campaignPosition, len(levelMap.Sections))
	for _, section := range levelMap.Sections {
		positions[section.ComponentID] = &campaignPosition{
			ComponentID: section.ComponentID,
			Anchor:      fmt.Sprintf("position-%d", section.ComponentID),
			ImageURL:    section.ImageURL,
			MarkerCount: section.MarkerCount,
		}
	}
	signalCounts := make(map[uint32]map[string]int)
	entryCounts := make(map[uint32]map[string]*campaignPositionEntry)
	for markerIndex := range levelMap.Markers {
		marker := &levelMap.Markers[markerIndex]
		position := positions[marker.Section]
		if position == nil {
			continue
		}
		if marker.Label != "" {
			if signalCounts[marker.Section] == nil {
				signalCounts[marker.Section] = make(map[string]int)
			}
			signalCounts[marker.Section][marker.Label]++
		}
		if marker.NounName != "" {
			if entryCounts[marker.Section] == nil {
				entryCounts[marker.Section] = make(map[string]*campaignPositionEntry)
			}
			key := strings.ToLower(marker.Category + "\x00" + marker.NounName)
			if entryCounts[marker.Section][key] == nil {
				entryCounts[marker.Section][key] = &campaignPositionEntry{
					Name: catalog.DisplayName(marker.NounName), Noun: marker.NounName,
					Category: marker.Category,
				}
			}
			entryCounts[marker.Section][key].Count++
		}
	}
	for componentID, position := range positions {
		labels := make([]string, 0, len(signalCounts[componentID]))
		for label := range signalCounts[componentID] {
			if !campaignTeleportLabelPattern.MatchString(label) {
				labels = append(labels, label)
			}
		}
		sort.Strings(labels)
		for _, label := range labels {
			position.Signals = append(position.Signals, campaignPositionSignal{
				Label: label, Meaning: levelMap.Labels[label], Count: signalCounts[componentID][label],
			})
		}
		for _, entry := range entryCounts[componentID] {
			position.Entries = append(position.Entries, *entry)
		}
		sort.Slice(position.Entries, func(left, right int) bool {
			if position.Entries[left].Category != position.Entries[right].Category {
				return position.Entries[left].Category < position.Entries[right].Category
			}
			return strings.ToLower(position.Entries[left].Name) < strings.ToLower(position.Entries[right].Name)
		})
		position.Nouns = campaignPositionNouns(position.Signals, pools)
	}
	for routeIndex, route := range teleportRoutes {
		sourceSection := campaignMarkerSection(levelMap.Markers, route.SourceX, route.SourceY, route.SourceZ)
		destinationSection := campaignMarkerSection(
			levelMap.Markers, route.DestinationX, route.DestinationY, route.DestinationZ,
		)
		if sourceSection == 0 || destinationSection == 0 {
			continue
		}
		label := fmt.Sprintf("T%d", routeIndex+1)
		if source := positions[sourceSection]; source != nil {
			source.Teleports = append(source.Teleports, campaignTeleportLink{
				Label: label, Direction: "outbound", OtherComponentID: destinationSection,
				OtherAnchor: fmt.Sprintf("position-%d", destinationSection),
				X:           route.DestinationX, Y: route.DestinationY, Z: route.DestinationZ,
			})
		}
		if destination := positions[destinationSection]; destination != nil {
			destination.Teleports = append(destination.Teleports, campaignTeleportLink{
				Label: label, Direction: "inbound", OtherComponentID: sourceSection,
				OtherAnchor: fmt.Sprintf("position-%d", sourceSection),
				X:           route.SourceX, Y: route.SourceY, Z: route.SourceZ,
			})
		}
	}
	result := make([]campaignPosition, 0, len(levelMap.Sections))
	for _, section := range levelMap.Sections {
		position := positions[section.ComponentID]
		sort.Slice(position.Teleports, func(left, right int) bool {
			return position.Teleports[left].Label < position.Teleports[right].Label
		})
		result = append(result, *position)
	}
	return result
}

func campaignMarkerSection(markers []campaignMapMarker, x, y, z float64) uint32 {
	const coordinateTolerance = 0.01
	for _, marker := range markers {
		if marker.Section == 0 || marker.Category == "teleporter-route" {
			continue
		}
		if math.Abs(marker.X-x) <= coordinateTolerance &&
			math.Abs(marker.Y-y) <= coordinateTolerance &&
			math.Abs(marker.Z-z) <= coordinateTolerance {
			return marker.Section
		}
	}
	return 0
}

func campaignPositionNouns(signals []campaignPositionSignal, pools []campaignPoolNoun) []campaignPositionNoun {
	poolNames := make(map[string]bool)
	hasHorde := false
	for _, signal := range signals {
		switch signal.Label {
		case "W":
			poolNames["minion"] = true
		case "L":
			poolNames["minion"] = true
			poolNames["captain"] = true
		case "H":
			hasHorde = true
		case "B", "C", "D":
			poolNames["boss"] = true
			poolNames["captain"] = true
		}
	}
	seen := make(map[string]struct{})
	result := make([]campaignPositionNoun, 0, len(pools))
	for _, source := range pools {
		if !poolNames[strings.ToLower(source.Pool)] && !(hasHorde && source.IsHordeLegal) {
			continue
		}
		key := strings.ToLower(source.Pool + "\x00" + source.Noun)
		if _, isFound := seen[key]; isFound {
			continue
		}
		seen[key] = struct{}{}
		slug := catalog.Slug(source.Noun)
		if strings.HasSuffix(source.Noun, ".noun") {
			slug = catalog.Slug(strings.TrimSuffix(source.Noun, ".noun") + "_Captain.Noun")
		}
		result = append(result, campaignPositionNoun{
			Name: catalog.DisplayName(source.Noun), Noun: source.Noun,
			Slug: slug, Pool: campaignPoolLabel(source.Pool),
		})
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].Pool != result[right].Pool {
			return result[left].Pool < result[right].Pool
		}
		return strings.ToLower(result[left].Name) < strings.ToLower(result[right].Name)
	})
	return result
}

func campaignPoolLabel(pool string) string {
	switch strings.ToLower(pool) {
	case "minion":
		return "ordinary pool"
	case "captain":
		return "lieutenant pool"
	case "boss":
		return "boss pool"
	case "agent":
		return "agent pool"
	case "special":
		return "special pool"
	default:
		return strings.ToLower(pool) + " pool"
	}
}

func buildCampaignEnemy(database *sql.DB, source authoredEnemy, healthMultiplier, damageMultiplier float64, descriptions map[uint32][]string) (campaignEnemy, error) {
	enemy := campaignEnemy{
		Name: source.Name, Noun: source.Noun, Slug: catalog.Slug(source.Noun + ".Noun"),
		Descriptions: source.Descriptions, Behavior: source.Behavior,
	}
	instanceID, hitPoints, challenge, rank, profileNoun, err := loadCampaignEnemyStats(database, source.Noun)
	if err != nil {
		return enemy, err
	}
	if paragraphs := descriptions[instanceID]; len(paragraphs) > 0 {
		enemy.Descriptions = paragraphs
	}
	if profileNoun != "" {
		enemy.Slug = catalog.Slug(profileNoun)
	}
	enemy.BaseHitPoints = hitPoints
	enemy.EstimatedHitPoints = roundCampaignValue(hitPoints * healthMultiplier)
	enemy.ChallengeValue = challenge
	enemy.Role = campaignRole(rank, challenge)
	profile, isFound := campaignDamageProfile(source.Noun)
	if isFound && profile.IsDamageProfileKnown && (profile.MinimumDamage > 0 || profile.MaximumDamage > 0) {
		minimum := roundCampaignValue(float64(profile.MinimumDamage) * damageMultiplier)
		maximum := roundCampaignValue(float64(profile.MaximumDamage) * damageMultiplier)
		enemy.EstimatedDamageMin = &minimum
		enemy.EstimatedDamageMax = &maximum
		// Match build 103's nDamageTypes and nDamageSources enums.
		damageTypes := [...]string{"Technology", "Spacetime", "Life", "Elements", "Supernatural", "Generic"}
		damageSources := [...]string{"physical", "energy"}
		if int(profile.DamageType) < len(damageTypes) {
			enemy.EstimatedDamageType = damageTypes[profile.DamageType]
			if int(profile.DamageSource) < len(damageSources) {
				enemy.EstimatedDamageType += " (" + damageSources[profile.DamageSource] + ")"
			}
		}
	}
	return enemy, nil
}

func loadCampaignEnemyStats(database *sql.DB, noun string) (uint32, float64, int, int, string, error) {
	var id uint32
	var hp float64
	var cv, rank int
	var profile string
	err := database.QueryRow(`SELECT c.instance_id,c.hit_point,c.challenge_value,c.npc_rank,n.noun_name FROM non_player_noun n JOIN non_player_class c ON c.content_source_resource_id=n.non_player_class_resource_id WHERE n.noun_name=? COLLATE NOCASE`, noun+".Noun").Scan(&id, &hp, &cv, &rank, &profile)
	if err == nil {
		return id, hp, cv, rank, profile, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, 0, 0, 0, "", err
	}
	candidates := []string{noun + ".Noun", noun + ".NonPlayerClass", noun + ".ClassAttributes", noun}
	for _, candidate := range candidates {
		instanceID := catalog.HashID(candidate)
		var hitPoints float64
		var challenge, rank int
		var profileNoun string
		err := database.QueryRow(`
			SELECT hit_point, challenge_value, npc_rank, noun_name FROM non_player_class
			WHERE instance_id=?`, instanceID).Scan(&hitPoints, &challenge, &rank, &profileNoun)
		if err == nil {
			return instanceID, hitPoints, challenge, rank, profileNoun, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, 0, 0, 0, "", err
		}
	}
	return 0, 0, 0, 0, "", errors.New("non-player class not found")
}

func campaignDamageProfile(noun string) (zonenpc.ActionProfile, bool) {
	switch strings.ToLower(noun) {
	case "zelembasicranged":
		return zonenpc.ZelemRangedShotProfile(), true
	case "zelemspecialhaster":
		return zonenpc.ZelemHasterAttackProfile(), true
	case "nomadsnipe":
		return zonenpc.NomadSnipeMeleeProfile(noun + ".Noun"), true
	default:
		return zonenpc.ActionProfileForNoun(noun + ".Noun")
	}
}

func campaignRole(rank, challenge int) string {
	if rank == 0 {
		return "Object"
	}
	if challenge >= 20 {
		return "Lieutenant"
	}
	return "Minion"
}

func roundCampaignValue(value float64) float64 {
	return math.Round(value*10) / 10
}

func formatCampaignValue(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
