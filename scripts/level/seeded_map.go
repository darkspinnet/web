package main

import (
	"context"
	"database/sql"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
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
	"github.com/darkspinnet/web/scripts/internal/catalog"
)

const seededMapSize = 512

type seededMapPoint struct {
	X          int    `json:"x"`
	Y          int    `json:"y"`
	Slug       string `json:"slug"`
	Name       string `json:"name"`
	Destructor bool   `json:"destructor,omitempty"`
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
		if err = generateSeededMission(ctx, database, setup, level, nav, base, seed, descriptions); err != nil {
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

func generateSeededMission(ctx context.Context, database *sql.DB, setup *game.CampaignSetup, level *levelReport, nav seededMapGeometry, base string, seed uint64, descriptions map[uint32][]string) error {
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
	for _, plan := range plans {
		if !addSeededMapPoint(levelMap, nav, plan, false) {
			levelMap.UnmappedSpawns++
		}
	}
	leader, found := zoneboss.NamedBossNounForDirector(director, stage)
	if found && zoneboss.IsFinalBossNoun(leader) {
		publication, err := zoneboss.DeveloperPublication(director)
		if err != nil {
			return err
		}
		_, encounter, _, err := zoneboss.PlanNamedEncounter(director, publication, 1, 1, stage)
		if err != nil {
			return err
		}
		boss := encounter[0]
		if !strings.EqualFold(boss.NounName, leader) {
			return fmt.Errorf("expected Destructor %s, planned %s", leader, boss.NounName)
		}
		if !addSeededMapPoint(levelMap, nav, boss, true) {
			levelMap.UnmappedSpawns++
		}
		noun := leader[:len(leader)-len(".Noun")]
		var name, description string
		err = database.QueryRow(`SELECT c.display_name, c.description
			FROM non_player_noun n JOIN non_player_class c ON c.content_source_resource_id=n.non_player_class_resource_id
			WHERE n.noun_name=? COLLATE NOCASE`, leader).Scan(&name, &description)
		if err != nil {
			return err
		}
		source := authoredEnemy{Name: name, Noun: noun}
		if description != "" {
			source.Descriptions = []string{description}
		}
		enemy, err := buildCampaignEnemy(database, source, level.Campaign.HealthMultiplier, level.Campaign.DamageMultiplier, descriptions)
		if err != nil {
			return err
		}
		enemy.Role = "Destructor"
		level.Campaign.Enemies = append(level.Campaign.Enemies, enemy)
		level.NPCs = append(level.NPCs, leader)
		level.NPCCount = len(level.NPCs)
	}
	level.Map = levelMap
	return nil
}

func addSeededMapPoint(levelMap *campaignMap, nav seededMapGeometry, plan zonenpc.SpawnPlan, destructor bool) bool {
	position := navigation.Vec3(plan.Position)
	projection, err := nav.mesh.Project(position, navigation.ProjectionOptions{MaxDistance: 20})
	if err != nil {
		return false
	}
	for i, info := range nav.sections {
		if info.ComponentID != projection.ComponentID {
			continue
		}
		point, ok := info.ProjectTopDown(position)
		if !ok {
			point, ok = info.ProjectTopDown(projection.Position)
		}
		if !ok {
			return false
		}
		section := &levelMap.Sections[i]
		section.Spawns = append(section.Spawns, seededMapPoint{
			X: point.X, Y: point.Y, Slug: catalog.Slug(plan.NounName),
			Name: catalog.DisplayName(plan.NounName), Destructor: destructor,
		})
		section.MarkerCount++
		return true
	}
	return false
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
