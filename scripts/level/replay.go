package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	contentsqlite "github.com/darkspinnet/darkspin/content/sqlite"
	"github.com/darkspinnet/darkspin/server/game"
	gamedata "github.com/darkspinnet/darkspin/server/game/contentsqlite"
	"github.com/darkspinnet/darkspin/server/util"
	zoneboss "github.com/darkspinnet/darkspin/server/zone/boss"
	zonepopulation "github.com/darkspinnet/darkspin/server/zone/population"
	"github.com/darkspinnet/web/scripts/internal/catalog"
)

type replayEnemy struct {
	Noun  string   `json:"noun"`
	Slug  string   `json:"slug"`
	Name  string   `json:"name"`
	Roles []string `json:"roles"`
	Modes []string `json:"modes"`
}

type replayDetail struct {
	Enemies []replayEnemy `json:"enemies"`
}

func loadReplayLevels(dbPath, mapPath string, levels []levelReport) ([]levelReport, error) {
	ctx := context.Background()
	store, err := contentsqlite.OpenReadOnly(ctx, dbPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	source, err := gamedata.NewDirectorSource(store)
	if err != nil {
		return nil, err
	}
	var reports []levelReport
	for _, level := range levels {
		if level.Progression < 1 || level.Progression > 24 {
			continue
		}
		director, err := source.LoadCampaignDirector(ctx, level.Name, uint32(level.Progression))
		if err != nil {
			return nil, fmt.Errorf("replay director %s: %w", level.Name, err)
		}
		report := level
		report.Slug = "replay-" + level.Slug
		report.Mode, report.Slot, report.Previous, report.Next = "", "", "", ""
		report.Progression, report.Campaign, report.CampaignOrders = 0, nil, nil
		report.ZoneRules = nil // Replay rules depend on the selected threat, not this destination.
		report.Region = levelRegion(report.Name, director.PlanetConfigName)
		report.Replay = &replayDetail{Enemies: replayRoster(director)}
		report.NPCs = make([]string, 0, len(report.Replay.Enemies))
		for _, enemy := range report.Replay.Enemies {
			report.NPCs = append(report.NPCs, enemy.Noun)
		}
		report.NPCCount = len(report.NPCs)
		report.Map, err = loadReplayMap(mapPath, level)
		if err != nil {
			return nil, fmt.Errorf("replay map %s: %w", level.Name, err)
		}
		reports = append(reports, report)
	}
	return reports, nil
}

func loadReplayMap(path string, level levelReport) (*campaignMap, error) {
	raw, err := os.ReadFile(filepath.Join(path, level.Slot, "map.json"))
	if errors.Is(err, os.ErrNotExist) {
		// Keep a usable navigation map if the optional authored overlay is absent.
		if level.Map == nil {
			return nil, nil
		}
		levelMap := &campaignMap{Level: level.Name}
		for _, section := range level.Map.Sections {
			levelMap.Sections = append(levelMap.Sections, campaignMapSection{
				ComponentID: section.ComponentID, ImageURL: section.ImageURL,
			})
		}
		return levelMap, nil
	}
	if err != nil {
		return nil, err
	}
	var levelMap campaignMap
	if err = json.Unmarshal(raw, &levelMap); err != nil {
		return nil, err
	}
	if !strings.EqualFold(levelMap.Level, level.Name) {
		return nil, fmt.Errorf("overlay is for %q", levelMap.Level)
	}
	for i := range levelMap.Sections {
		section := &levelMap.Sections[i]
		section.ImageURL = "/maps/" + level.Slot + "/" + section.Image
	}
	for _, label := range []string{"B", "C", "D"} {
		if _, found := levelMap.Labels[label]; found {
			levelMap.Labels[label] = "authored leader encounter anchor"
		}
	}
	levelMap.Markers = nil
	return &levelMap, nil
}

func replayRoster(director game.CampaignDirector) []replayEnemy {
	director.IsReplay = true
	byNoun := make(map[string]*replayEnemy)
	modes := []string{"Onslaught", "Invasion", "Apocalypse"}
	for stage := uint32(1); stage <= 72; stage++ {
		director.Difficulty = stage
		mode := modes[(stage-1)/24]
		add := func(noun, role string) {
			if noun == "" {
				return
			}
			key := strings.ToLower(noun)
			enemy := byNoun[key]
			if enemy == nil {
				name := director.NPCIdentitiesByNoun[key].DisplayName
				if name == "" {
					name = catalog.DisplayName(noun)
				}
				enemy = &replayEnemy{Noun: noun, Slug: catalog.Slug(noun), Name: name}
				byNoun[key] = enemy
			}
			if !slices.Contains(enemy.Roles, role) {
				enemy.Roles = append(enemy.Roles, role)
			}
			if !slices.Contains(enemy.Modes, mode) {
				enemy.Modes = append(enemy.Modes, mode)
			}
		}
		tierSuffixes := map[string]bool{}
		for _, entry := range replayDrawCandidates(director) {
			add(entry.NounName, entry.Role)
			noun := strings.TrimSuffix(strings.ToLower(entry.NounName), ".noun")
			suffix := ""
			for _, tier := range []string{"_2", "_3"} {
				if strings.HasSuffix(noun, tier) {
					suffix = tier
				}
			}
			tierSuffixes[suffix] = true
		}
		if leader, found := zoneboss.NamedBossNounForDirector(director, stage); found {
			role := "Captain"
			if zoneboss.IsFinalBossNoun(leader) {
				role = "Destructor"
			}
			add(leader, role)
		}
		agents := zonepopulation.PoolEntries(director, "agent")
		hasAgent := false
		for _, agent := range agents {
			if replayEntryEligible(agent, stage) {
				hasAgent = true
				if stage >= 9 || agent.IsHordeLegal {
					add(agent.NounName, "Agent")
				}
			}
		}
		if !hasAgent && strings.EqualFold(director.Level, game.InitialChainLevel) {
			for _, entry := range replayPool(director.Pools, "firstTimeConfig", "minion") {
				if replayEntryEligible(entry, stage) && entry.IsHordeLegal {
					add(entry.NounName, "Horde")
				}
			}
		}
		// The default co-op policy can replace ordinary groups with these
		// operatives. Its first placement is Cyber on even map hashes, and
		// odd hashes allow a Spacetime placement as well as a Cyber one.
		for _, suffix := range []string{"", "_2", "_3"} {
			if !tierSuffixes[suffix] {
				continue
			}
			for _, stem := range []string{"NomadCyberOne", "NomadSpacetimeAgent"} {
				if stem == "NomadSpacetimeAgent" && util.HashID(director.Level)%2 == 0 {
					continue
				}
				noun := stem + suffix + ".Noun"
				profile := director.NPCProfilesByNoun[strings.ToLower(noun)]
				if profile.IsKnown && profile.HitPoint > 0 {
					add(noun, "Co-op operative")
				}
			}
		}
	}
	for _, set := range director.MarkerSets {
		for _, marker := range set.Markers {
			if !marker.NPCProfile.IsKnown || marker.NPCProfile.HitPoint <= 0 ||
				!marker.NPCProfile.IsTargetable || marker.NPCProfile.IsPlayerPet || marker.NounType != game.NounTypeCreature {
				continue
			}
			key := strings.ToLower(marker.NounName)
			if enemy := byNoun[key]; enemy != nil {
				if !slices.Contains(enemy.Roles, "Fixed encounter") {
					enemy.Roles = append(enemy.Roles, "Fixed encounter")
				}
				enemy.Modes = slices.Clone(modes)
				continue
			}
			name := director.NPCIdentitiesByNoun[key].DisplayName
			if name == "" {
				name = catalog.DisplayName(marker.NounName)
			}
			byNoun[key] = &replayEnemy{Noun: marker.NounName, Slug: catalog.Slug(marker.NounName), Name: name,
				Roles: []string{"Fixed encounter"}, Modes: slices.Clone(modes)}
		}
	}
	enemies := make([]replayEnemy, 0, len(byNoun))
	for _, enemy := range byNoun {
		enemies = append(enemies, *enemy)
	}
	sort.Slice(enemies, func(i, j int) bool {
		if enemies[i].Name == enemies[j].Name {
			return enemies[i].Slug < enemies[j].Slug
		}
		return enemies[i].Name < enemies[j].Name
	})
	return enemies
}

type replayDrawCandidate struct {
	NounName string
	Role     string
}

// Mirror the active source branches in game.composeCampaignRoster. Enumerate
// their eligible entries instead of rolling a sample roster from each source.
func replayDrawCandidates(director game.CampaignDirector) []replayDrawCandidate {
	science := [...]string{"Cyber", "Chrono", "Bio", "Plasma", "Necro", "Generic"}
	planets := [...]string{"Sentios", "Zelem", "Verdanth", "Cryos", "Nocturna", "Scaldron"}
	stage := director.Difficulty
	secondary, tertiary := science[director.SecondaryType], science[director.TertiaryType]
	if stage >= 25 {
		secondary = planets[director.SecondaryType]
	}
	if stage >= 49 {
		tertiary = planets[director.TertiaryType]
	}
	var candidates []replayDrawCandidate
	add := func(pools []game.CampaignDirectorPool, source, kind, role string) {
		for _, entry := range replayPool(pools, source, kind) {
			if replayEntryEligible(entry, stage) {
				candidates = append(candidates, replayDrawCandidate{NounName: entry.NounName, Role: role})
			}
		}
	}
	add(director.Pools, "levelConfig", "minion", "Minion")
	add(director.ExternalPools, secondary+".LevelConfig", "minion", "Minion")
	if stage < 49 {
		add(director.ExternalPools, director.PlanetConfigName, "minion", "Minion")
	} else {
		add(director.ExternalPools, tertiary+".LevelConfig", "minion", "Minion")
	}
	add(director.ExternalPools, director.PlanetConfigName, "special", "Lieutenant")
	add(director.ExternalPools, secondary+".LevelConfig", "special", "Lieutenant")
	switch {
	case stage >= 49:
		add(director.ExternalPools, science[director.QuaternaryType]+".LevelConfig", "special", "Lieutenant")
	case stage >= 25:
		add(director.ExternalPools, tertiary+".LevelConfig", "special", "Lieutenant")
	}
	return candidates
}

func replayPool(pools []game.CampaignDirectorPool, source, kind string) []game.CampaignDirectorEntry {
	for _, pool := range pools {
		if strings.EqualFold(pool.ConfigurationName, source) && strings.EqualFold(pool.ConfigKind, kind) {
			return pool.Entries
		}
	}
	return nil
}

func replayEntryEligible(entry game.CampaignDirectorEntry, stage uint32) bool {
	return stage >= entry.MinimumDifficulty && stage <= entry.MaximumDifficulty &&
		entry.NPCProfile.IsKnown && entry.NPCProfile.HitPoint > 0
}
