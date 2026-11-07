package main

import (
	"context"
	"database/sql"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	contentsqlite "github.com/darkspinnet/darkspin/content/sqlite"
	"github.com/darkspinnet/darkspin/server/game"
	gamedata "github.com/darkspinnet/darkspin/server/game/contentsqlite"
	"github.com/darkspinnet/darkspin/server/navigation"
	"github.com/darkspinnet/darkspin/server/util"
	zoneboss "github.com/darkspinnet/darkspin/server/zone/boss"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	zonepopulation "github.com/darkspinnet/darkspin/server/zone/population"
	zonesecurity "github.com/darkspinnet/darkspin/server/zone/security"
	"github.com/darkspinnet/web/scripts/internal/catalog"
)

const seededMapSize = 512

type seededMapPoint struct {
	X          int      `json:"x"`
	Y          int      `json:"y"`
	Slug       string   `json:"slug"`
	Name       string   `json:"name"`
	Destructor bool     `json:"destructor,omitempty"`
	Captain    bool     `json:"captain,omitempty"`
	SpawnName  string   `json:"spawn_name,omitempty"`
	Affixes    []string `json:"affixes,omitempty"`
}

type seededEnemyVariant struct {
	Name    string   `json:"name"`
	Affixes []string `json:"affixes,omitempty"`
	Count   int      `json:"count"`
}

type seededMapObject struct {
	X           int     `json:"x"`
	Y           int     `json:"y"`
	Kind        string  `json:"kind"`
	Name        string  `json:"name"`
	Slug        string  `json:"slug,omitempty"`
	Destination *uint32 `json:"destination_component,omitempty"`
	Arrival     bool    `json:"arrival,omitempty"`
}

type seededMapGeometry struct {
	mesh     *navigation.Mesh
	sections []navigation.MapInfo
}

func generateSeededMaps(database *sql.DB, dbPath, mapPath string, seed uint64, levels []levelReport, descriptions map[uint32][]string) error {
	if seed == 0 {
		return fmt.Errorf("campaign-seed must be nonzero")
	}
	ctx := context.Background()
	store, err := contentsqlite.OpenReadOnly(ctx, dbPath)
	if err != nil {
		return err
	}
	defer store.Close()
	affixCatalog, err := store.NPCAffixes(ctx)
	if err != nil {
		return err
	}
	affixLabels := make(map[string]string, len(affixCatalog))
	for _, affix := range affixCatalog {
		if affix.Description != nil && *affix.Description != "" {
			affixLabels[strings.ToLower(affix.AssetName)] = *affix.Description
		}
	}
	source, err := gamedata.NewDirectorSource(store)
	if err != nil {
		return err
	}
	setup, err := game.NewCampaignSetup(source)
	if err != nil {
		return err
	}
	geometry := make(map[string]seededMapGeometry)
	for i := range levels {
		level := &levels[i]
		if level.Campaign == nil {
			continue
		}
		base := catalog.Slug(level.Name)
		nav, found := geometry[base]
		if !found {
			nav, err = renderSeededGeometry(ctx, store, mapPath, level.Name, base)
			if err != nil {
				return fmt.Errorf("campaign map geometry %s: %w", level.Name, err)
			}
			geometry[base] = nav
		}
		if err = generateSeededMission(ctx, database, setup, level, nav, base, seed, descriptions, affixLabels); err != nil {
			return fmt.Errorf("campaign map %s: %w", level.Slug, err)
		}
	}
	return nil
}

func renderSeededGeometry(ctx context.Context, store *contentsqlite.Store, mapPath, level, slug string) (seededMapGeometry, error) {
	data, err := store.LevelNavigation(ctx, level)
	if err != nil {
		return seededMapGeometry{}, err
	}
	mesh, err := navigation.ParseBFX(data)
	if err != nil {
		return seededMapGeometry{}, err
	}
	sections, err := mesh.RenderTopDownSections(0, seededMapSize, seededMapSize, 32)
	if err != nil {
		return seededMapGeometry{}, err
	}
	dir := filepath.Join(mapPath, slug)
	if err = os.MkdirAll(dir, 0o755); err != nil {
		return seededMapGeometry{}, err
	}
	geometry := seededMapGeometry{mesh: mesh}
	for _, section := range sections {
		path := filepath.Join(dir, seededBackground(section.Info.ComponentID))
		file, err := os.Create(path)
		if err != nil {
			return seededMapGeometry{}, err
		}
		encodeErr := png.Encode(file, section.Image)
		closeErr := file.Close()
		if encodeErr != nil {
			return seededMapGeometry{}, encodeErr
		}
		if closeErr != nil {
			return seededMapGeometry{}, closeErr
		}
		geometry.sections = append(geometry.sections, section.Info)
	}
	return geometry, nil
}

func seededBackground(component uint32) string {
	return fmt.Sprintf("navigation-%02d.png", component)
}

// Follow outgoing teleport routes from the player entrance. Retain disconnected
// components afterward, without inventing connections to them.
func orderSeededMapSections(levelMap *campaignMap) {
	byID := make(map[uint32]campaignMapSection, len(levelMap.Sections))
	var start uint32
	foundStart := false
	for _, section := range levelMap.Sections {
		byID[section.ComponentID] = section
		for _, object := range section.Objects {
			if object.Kind == "entry" {
				start, foundStart = section.ComponentID, true
			}
		}
	}
	if !foundStart {
		return
	}
	ordered := make([]campaignMapSection, 0, len(levelMap.Sections))
	seen := make(map[uint32]bool)
	var visit func(uint32)
	visit = func(id uint32) {
		section, exists := byID[id]
		if seen[id] || !exists {
			return
		}
		seen[id] = true
		ordered = append(ordered, section)
		for _, object := range section.Objects {
			if object.Kind == "teleport" && object.Destination != nil {
				visit(*object.Destination)
			}
		}
	}
	visit(start)
	for _, section := range levelMap.Sections {
		visit(section.ComponentID)
	}
	levelMap.Sections = ordered
}

func generateSeededMission(ctx context.Context, database *sql.DB, setup *game.CampaignSetup, level *levelReport, nav seededMapGeometry, base string, seed uint64, descriptions map[uint32][]string, affixLabels map[string]string) error {
	stage := uint32(level.Progression)
	director, err := setup.Execute(ctx, game.GameplayBinding{
		Level: level.Name, Mode: game.ModeChain, Difficulty: stage, ChainLevelIndex: stage,
		RunSeed: seed, IsNoReplay: true, PartyCompletedStages: []uint32{0},
	})
	if err != nil {
		return err
	}
	director, err = game.ApplyPopulationRules(director)
	if err != nil {
		return err
	}
	variant := campaignMapVariant(seed, level.Name, stage)
	ordinals, err := director.SelectMapLayout(variant, 0)
	if err != nil {
		return err
	}
	director, err = director.InitialMapLayout(variant, ordinals)
	if err != nil {
		return err
	}
	director, err = zoneboss.PrepareNamedBossDirector(director, stage)
	if err != nil {
		return err
	}

	// The first-time branch in gameplay.campaignPopulationSeed uses this
	// stable stage seed independently of the run's map variant stream.
	populationSeed := (stage << 24) ^ 0x103 ^ ((stage - 1) << 16)
	if !director.IsFirstTimeRosterSelected {
		return fmt.Errorf("first-time roster unavailable")
	}
	population, err := zonepopulation.NewFirstClearSessionWithNavigation(director, populationSeed, nav.mesh)
	if err != nil {
		return err
	}
	decisions, err := population.PrimeOpening(game.Vec3{})
	if err != nil {
		return err
	}
	plans, _, err := population.PlanCampaignSpawnsWithContext(director, decisions, 1, stage,
		zonepopulation.GroupCompositionContext{AlivePartyCount: 1})
	if err != nil {
		return err
	}
	plans, err = population.ApplyRules(director, plans, nil)
	if err != nil {
		return err
	}
	levelMap := &campaignMap{
		Level: level.Name, Selection: level.Slot, RunSeed: strconv.FormatUint(seed, 10),
		PopulationSeed: populationSeed,
	}
	for _, section := range nav.sections {
		levelMap.Sections = append(levelMap.Sections, campaignMapSection{
			ComponentID: section.ComponentID, ImageURL: "/maps/" + base + "/" + seededBackground(section.ComponentID),
		})
	}
	if err = addSeededMapObjects(levelMap, nav, director); err != nil {
		return err
	}
	for _, plan := range plans {
		variant := seededVariant(plan.BossIdentity, affixLabels)
		if len(variant.Affixes) > 0 {
			enemy, err := seededRosterEnemy(database, level, plan.NounName, descriptions)
			if err != nil {
				return err
			}
			enemy.addSeededVariant(variant)
		}
		if !addSeededMapPoint(levelMap, nav, plan, variant) {
			levelMap.UnmappedSpawns++
		}
	}
	leader, found := zoneboss.NamedBossNounForDirector(director, stage)
	if found {
		boss, err := planSeededEncounter(director, stage)
		if err != nil {
			return err
		}
		if !strings.EqualFold(boss.NounName, leader) {
			return fmt.Errorf("expected encounter leader %s, planned %s", leader, boss.NounName)
		}
		variant := seededVariant(boss.BossIdentity, affixLabels)
		if !addSeededMapPoint(levelMap, nav, boss, variant) {
			levelMap.UnmappedSpawns++
		}
		enemy, err := seededRosterEnemy(database, level, leader, descriptions)
		if err != nil {
			return err
		}
		if boss.IsCaptain {
			enemy.Role = "Captain"
			enemy.EstimatedHitPoints = roundCampaignValue(float64(boss.NPCProfile.HitPoint) * level.Campaign.HealthMultiplier)
		} else {
			enemy.Role = "Destructor"
		}
		if variant.Name != "" {
			enemy.Name = variant.Name
		}
		if len(variant.Affixes) > 0 {
			enemy.addSeededVariant(variant)
		}
	}
	orderSeededMapSections(levelMap)
	level.Map = levelMap
	return nil
}

func planSeededEncounter(director game.CampaignDirector, stage uint32) (zonenpc.SpawnPlan, error) {
	var plans []zonenpc.SpawnPlan
	var err error
	if strings.EqualFold(director.Level, "zelems_1") {
		publication, publicationErr := zoneboss.InitialDeveloperPublication(director)
		if publicationErr != nil {
			return zonenpc.SpawnPlan{}, publicationErr
		}
		plans, _, err = zoneboss.PlanInitialEncounter(director, publication, 1, 1, stage)
	} else {
		publication, publicationErr := zoneboss.DeveloperPublication(director)
		if publicationErr != nil {
			return zonenpc.SpawnPlan{}, publicationErr
		}
		_, plans, _, err = zoneboss.PlanNamedEncounter(director, publication, 1, 1, stage)
	}
	if err != nil {
		return zonenpc.SpawnPlan{}, err
	}
	if len(plans) == 0 {
		return zonenpc.SpawnPlan{}, fmt.Errorf("named encounter has no leader")
	}
	return plans[0], nil
}

func seededVariant(identity zonenpc.BossIdentity, affixLabels map[string]string) seededEnemyVariant {
	variant := seededEnemyVariant{Name: strings.TrimSpace(identity.DisplayName), Count: 1}
	for _, asset := range identity.AffixNames {
		if asset != "" {
			label := affixLabels[strings.ToLower(asset)]
			if label == "" {
				label = catalog.DisplayName(strings.TrimSuffix(asset, ".NPCAffix"))
			}
			variant.Affixes = append(variant.Affixes, label)
		}
	}
	return variant
}

func (enemy *campaignEnemy) addSeededVariant(variant seededEnemyVariant) {
	for i := range enemy.SeededVariants {
		current := &enemy.SeededVariants[i]
		if current.Name == variant.Name && slices.Equal(current.Affixes, variant.Affixes) {
			current.Count++
			return
		}
	}
	enemy.SeededVariants = append(enemy.SeededVariants, variant)
}

func seededRosterEnemy(database *sql.DB, level *levelReport, noun string, descriptions map[uint32][]string) (*campaignEnemy, error) {
	campaign := level.Campaign
	slug := catalog.Slug(noun)
	for i := range campaign.Enemies {
		if campaign.Enemies[i].Slug == slug {
			return &campaign.Enemies[i], nil
		}
	}
	source := authoredEnemy{Noun: strings.TrimSuffix(noun, ".Noun")}
	var description string
	err := database.QueryRow(`SELECT c.display_name, c.description
		FROM non_player_noun n JOIN non_player_class c ON c.content_source_resource_id=n.non_player_class_resource_id
		WHERE n.noun_name=? COLLATE NOCASE`, noun).Scan(&source.Name, &description)
	if err != nil {
		return nil, err
	}
	if description != "" {
		source.Descriptions = []string{description}
	}
	enemy, err := buildCampaignEnemy(database, source, campaign.HealthMultiplier, campaign.DamageMultiplier, descriptions)
	if err != nil {
		return nil, err
	}
	campaign.Enemies = append(campaign.Enemies, enemy)
	level.NPCs = append(level.NPCs, noun)
	level.NPCCount = len(level.NPCs)
	return &campaign.Enemies[len(campaign.Enemies)-1], nil
}

func addSeededMapObjects(levelMap *campaignMap, nav seededMapGeometry, director game.CampaignDirector) error {
	add := func(position game.Vec3, object seededMapObject) {
		section, point, ok := projectSeededMapPoint(nav, position)
		if !ok {
			levelMap.UnmappedObjects++
			return
		}
		object.X, object.Y = point.X, point.Y
		levelMap.Sections[section].Objects = append(levelMap.Sections[section].Objects, object)
	}
	// Match gameplay.campaignEntryPosition for the solo opening represented here.
	entry := game.Vec3{X: -123.8707, Y: -151.64705, Z: 10.037109}
	if !strings.EqualFold(director.Level, "zelems_1") && len(director.EntryPositions) > 0 {
		entry = director.EntryPositions[0]
	}
	add(entry, seededMapObject{Kind: "entry", Name: "Player spawn point"})
	obeliskObjects, err := director.CampaignInteractables(0)
	if err != nil {
		return fmt.Errorf("map obelisks: %w", err)
	}
	for _, obelisk := range obeliskObjects {
		name := "Loot obelisk"
		if obelisk.InteractableAbility == "InteractHealthObelisk" {
			name = "Health obelisk"
		}
		add(obelisk.Position, seededMapObject{Kind: "obelisk", Name: name})
	}
	addTeleport := func(source, destination game.Vec3, name string) {
		object := seededMapObject{Kind: "teleport", Name: name}
		if section, _, ok := projectSeededMapPoint(nav, destination); ok {
			destination := nav.sections[section].ComponentID
			object.Destination = &destination
		}
		add(source, object)
		if name == "Boss teleporter" {
			add(destination, seededMapObject{Kind: "teleport", Name: "Boss teleporter arrival (one-way entrance)", Arrival: true})
		}
	}
	for _, route := range director.TeleportRoutes() {
		name := "Teleporter"
		switch {
		case route.IsBoss:
			name = "Boss teleporter"
		case route.IsSecurity:
			name = "Security teleporter"
		case route.IsTunnel:
			name = "Tunnel teleporter"
		}
		addTeleport(route.Source, route.Destination, name)
	}
	for _, route := range zonesecurity.Routes(director.Level) {
		name := "Security teleporter"
		if route.IsBoss {
			name = "Boss teleporter"
		}
		addTeleport(route.Source, route.Destination, name)
	}
	fixtures, takeoverIDs, removalIDs, err := director.MapDestructibles()
	if err != nil {
		return fmt.Errorf("map destructibles: %w", err)
	}
	addDestructible := func(position game.Vec3, noun string) {
		add(position, seededMapObject{Kind: "destructible", Name: catalog.DisplayName(noun), Slug: catalog.Slug(noun)})
	}
	for _, fixture := range fixtures {
		addDestructible(fixture.Position, fixture.NounName)
	}
	// The server replaces some client-owned ornaments and removes others.
	seen := make(map[uint32]bool, len(takeoverIDs)+len(removalIDs))
	for _, ids := range [][]uint32{takeoverIDs, removalIDs} {
		for _, id := range ids {
			seen[id] = true
		}
	}
	for _, set := range director.MarkerSets {
		for _, definition := range set.Definitions {
			if seen[definition.MarkerID] {
				continue
			}
			seen[definition.MarkerID] = true
			marker, found := director.SelectedMarkerDefinition(definition.MarkerID)
			if found && marker.IsVisible && director.NounTypeForAsset(marker.NounName) == game.NounTypeDestructibleOrnament {
				addDestructible(marker.Position, marker.NounName)
			}
		}
	}
	return nil
}

func addSeededMapPoint(levelMap *campaignMap, nav seededMapGeometry, plan zonenpc.SpawnPlan, variant seededEnemyVariant) bool {
	index, point, ok := projectSeededMapPoint(nav, plan.Position)
	if !ok {
		return false
	}
	section := &levelMap.Sections[index]
	section.Spawns = append(section.Spawns, seededMapPoint{
		X: point.X, Y: point.Y, Slug: catalog.Slug(plan.NounName),
		Name: catalog.DisplayName(plan.NounName), SpawnName: variant.Name, Affixes: variant.Affixes,
		Destructor: plan.IsBoss && !plan.IsCaptain, Captain: plan.IsBoss && plan.IsCaptain,
	})
	section.MarkerCount++
	return true
}

func projectSeededMapPoint(nav seededMapGeometry, world game.Vec3) (int, image.Point, bool) {
	position := navigation.Vec3(world)
	projection, err := nav.mesh.Project(position, navigation.ProjectionOptions{MaxDistance: 20})
	if err != nil {
		return 0, image.Point{}, false
	}
	for i, info := range nav.sections {
		if info.ComponentID != projection.ComponentID {
			continue
		}
		point, ok := info.ProjectTopDown(position)
		if !ok {
			point, ok = info.ProjectTopDown(projection.Position)
		}
		return i, point, ok
	}
	return 0, image.Point{}, false
}

// Match gameplay.zoneVariantSeed, including its native automatic-seed sentinels.
func campaignMapVariant(seed uint64, level string, stage uint32) uint32 {
	const salt = uint64(0x6d6170)
	asset := strings.TrimSuffix(strings.ToLower(level), "_v2")
	mixed := seed ^ uint64(util.HashID(asset))<<32 ^ uint64(stage)<<16 ^ salt
	mixed ^= mixed >> 30
	mixed *= 0xbf58476d1ce4e5b9
	mixed ^= mixed >> 27
	mixed *= 0x94d049bb133111eb
	mixed ^= mixed >> 31
	variant := uint32(mixed) ^ uint32(mixed>>32)
	if variant == 0 || variant == ^uint32(0) {
		return uint32(salt)
	}
	return variant
}
