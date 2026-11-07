package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"

	"github.com/darkspinnet/darkspin/server/game"
)

// Overrides replace the whole entry, including the presence of optional features.
// Keep this adapter aligned with server/runtime/zone_rules.go.
type zoneRuleDocument struct {
	Default       zoneRuleConfiguration            `json:"default"`
	ReplayDefault *zoneRuleOverrides               `json:"replay_default,omitempty"`
	Campaign      map[string]zoneRuleConfiguration `json:"campaign"`
	Replay        map[string]zoneRuleConfiguration `json:"replay"`
	Tutorial      *zoneRuleConfiguration           `json:"tutorial,omitempty"`
	Arena         *zoneRuleConfiguration           `json:"arena,omitempty"`
}

type zoneRuleOverrides struct {
	MutationAgent *zoneMutationConfiguration `json:"mutation_agent,omitempty"`
}

type zoneMutationConfiguration struct {
	MinimumChainLevel            uint32                          `json:"minimum_chain_level"`
	WaveCount                    int                             `json:"wave_count"`
	AdditionalWaveChancePercents []uint32                        `json:"additional_wave_chance_percents,omitempty"`
	FixedWaves                   [][]zoneHordeEnemyConfiguration `json:"fixed_waves,omitempty"`
	LieutenantChancePercent      uint32                          `json:"lieutenant_chance_percent"`
	LieutenantLimitPerWave       int                             `json:"lieutenant_limit_per_wave"`
	InterWaveDelayMillisecond    uint32                          `json:"inter_wave_delay_millisecond"`
	Completion                   string                          `json:"completion"`
}

type zoneHordeEnemyConfiguration struct {
	Noun  string `json:"noun"`
	Count uint32 `json:"count"`
}

func (c zoneMutationConfiguration) fixedWaves() [][]game.ZoneHordeEnemy {
	if c.FixedWaves == nil {
		return nil
	}
	waves := make([][]game.ZoneHordeEnemy, len(c.FixedWaves))
	for i, enemies := range c.FixedWaves {
		for _, enemy := range enemies {
			waves[i] = append(waves[i], game.ZoneHordeEnemy{NounName: enemy.Noun, Count: enemy.Count})
		}
	}
	return waves
}

func (d zoneRuleDocument) configuration(mode, mission string) (zoneRuleConfiguration, bool) {
	entries := d.Campaign
	if mode == "replay" {
		entries = d.Replay
	}
	if config, found := entries[mission]; found {
		return config, true
	}
	config := d.Default
	if mode == "replay" && d.ReplayDefault != nil && d.ReplayDefault.MutationAgent != nil {
		config.MutationAgent = d.ReplayDefault.MutationAgent
	}
	return config, false
}

type zoneRuleConfiguration struct {
	EnemiesPerSection uint32 `json:"enemies_per_section"`
	EnemyPopulation   struct {
		AgentSelection              string `json:"agent_selection"`
		AgentsPerSection            uint32 `json:"agents_per_section"`
		RosterSelection             string `json:"roster_selection"`
		ExternalRosterWeightPercent uint32 `json:"external_roster_weight_percent"`
		AmbientEliteChancePercent   uint32 `json:"ambient_elite_chance_percent"`
		OwnedSummonHealthScaling    string `json:"owned_summon_health_scaling"`
		FixtureHealthScaling        string `json:"fixture_health_scaling"`
	} `json:"enemy_population"`
	AllyAlert *struct {
		RangePercent uint32 `json:"range_percent"`
		RangeOwner   string `json:"range_owner"`
		MaxHops      uint32 `json:"max_hops"`
	} `json:"ally_alert,omitempty"`
	BossSpawning struct {
		OpeningAddsPerAnchor    uint32 `json:"opening_adds_per_anchor"`
		InitialBossAddCount     uint32 `json:"initial_boss_add_count"`
		CaptainDelayMillisecond uint32 `json:"captain_delay_millisecond"`
	} `json:"boss_spawning"`
	MutationAgent *zoneMutationConfiguration `json:"mutation_agent,omitempty"`
}

func (c zoneRuleConfiguration) rules(key string) game.ZoneRules {
	p := c.EnemyPopulation
	b := game.DefaultBossSpawnRules()
	b.OpeningAddsPerAnchor, b.InitialBossAddCount, b.CaptainDelayMillisecond = c.BossSpawning.OpeningAddsPerAnchor, c.BossSpawning.InitialBossAddCount, c.BossSpawning.CaptainDelayMillisecond
	r := game.ZoneRules{Key: key, EnemyTargetPerSection: c.EnemiesPerSection, BossSpawning: b,
		EnemyPopulation: game.EnemyPopulationRules{AgentSelection: p.AgentSelection, AgentsPerSection: p.AgentsPerSection,
			RosterSelection: p.RosterSelection, ExternalRosterWeightPercent: p.ExternalRosterWeightPercent,
			IsAmbientEliteEnabled: p.AmbientEliteChancePercent > 0, AmbientEliteChancePercent: p.AmbientEliteChancePercent,
			OwnedSummonHealthScaling: p.OwnedSummonHealthScaling, FixtureHealthScaling: p.FixtureHealthScaling},
		AllyAlert:     game.ZoneAllyAlertRules{RangePercent: 100, RangeOwner: "recipient", MaxHops: 1},
		MutationAgent: game.ZoneMutationAgentRules{MinimumChainLevel: 6, WaveCount: 3, SpawnWave: 3, LieutenantChancePercent: 30, LieutenantLimitPerWave: 1, InterWaveDelayMillisecond: 500, IsAgentDefeatCompleting: true},
	}
	if a := c.AllyAlert; a != nil {
		r.AllyAlert = game.ZoneAllyAlertRules{IsEnabled: true, RangePercent: a.RangePercent, RangeOwner: a.RangeOwner, MaxHops: a.MaxHops}
	}
	if m := c.MutationAgent; m != nil {
		r.MutationAgent = game.ZoneMutationAgentRules{IsEnabled: true, MinimumChainLevel: m.MinimumChainLevel, WaveCount: m.WaveCount, SpawnWave: m.WaveCount,
			FixedWaves: m.fixedWaves(), AdditionalWaveChancePercents: append([]uint32(nil), m.AdditionalWaveChancePercents...),
			LieutenantChancePercent: m.LieutenantChancePercent, LieutenantLimitPerWave: m.LieutenantLimitPerWave,
			InterWaveDelayMillisecond: m.InterWaveDelayMillisecond, IsAgentDefeatCompleting: m.Completion == "agent_defeat"}
	}
	return r
}

func loadZoneRules(path string) (zoneRuleDocument, error) {
	var document zoneRuleDocument
	raw, err := os.ReadFile(path)
	if err != nil {
		return document, fmt.Errorf("zone rules: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&document); err != nil {
		return document, fmt.Errorf("zone rules: %w", err)
	}
	var trailing any
	if err = decoder.Decode(&trailing); err != io.EOF {
		return document, fmt.Errorf("zone rules: unexpected trailing JSON")
	}
	validate := func(key string, c zoneRuleConfiguration) error {
		if c.MutationAgent != nil && c.MutationAgent.Completion != "agent_defeat" && c.MutationAgent.Completion != "all_waves" {
			return fmt.Errorf("zone rules %s: invalid completion", key)
		}
		if err := c.rules(key).Validate(); err != nil {
			return fmt.Errorf("zone rules %s: %w", key, err)
		}
		return nil
	}
	if err = validate("default", document.Default); err != nil {
		return document, err
	}
	if document.Default.MutationAgent != nil && len(document.Default.MutationAgent.AdditionalWaveChancePercents) > 0 {
		return document, fmt.Errorf("campaign default requires a fixed wave count")
	}
	if document.ReplayDefault != nil {
		config, _ := document.configuration("replay", "default")
		if err = validate("replay:default", config); err != nil {
			return document, err
		}
	}
	validMissions := make(map[string]bool)
	for stage := 1; stage <= 72; stage++ {
		validMissions[fmt.Sprintf("%d-%d", (stage-1)/4+1, (stage-1)%4+1)] = true
	}
	for group, entries := range map[string]map[string]zoneRuleConfiguration{"campaign": document.Campaign, "replay": document.Replay} {
		for mission, config := range entries {
			if group == "campaign" && config.MutationAgent != nil && len(config.MutationAgent.AdditionalWaveChancePercents) > 0 {
				return document, fmt.Errorf("campaign %s requires a fixed wave count", mission)
			}
			if !validMissions[mission] {
				return document, fmt.Errorf("zone rules %s:%s: invalid mission", group, mission)
			}
			if err = validate(group+":"+mission, config); err != nil {
				return document, err
			}
		}
	}
	for key, config := range map[string]*zoneRuleConfiguration{"tutorial": document.Tutorial, "arena": document.Arena} {
		if config != nil {
			if err = validate(key, *config); err != nil {
				return document, err
			}
		}
	}
	return document, nil
}

type zoneRuleReport struct {
	Key          string          `json:"key"`
	Override     bool            `json:"override"`
	ChangedCount int             `json:"changed_count"`
	Modified     bool            `json:"modified"`
	Groups       []zoneRuleGroup `json:"groups"`
	resolved     game.ZoneRules
}
type zoneRuleGroup struct {
	Name string        `json:"name"`
	Rows []zoneRuleRow `json:"rows"`
}
type zoneRuleRow struct {
	Label   string `json:"label"`
	Value   string `json:"value"`
	Default string `json:"default"`
	Changed bool   `json:"changed"`
}

func enrichZoneRules(path string, levels []levelReport) error {
	document, err := loadZoneRules(path)
	if err != nil {
		return err
	}
	for i := range levels {
		level := &levels[i]
		if level.Campaign == nil {
			continue
		}
		config, override := document.configuration("campaign", level.Slot)
		level.ZoneRules = buildZoneRuleReport("campaign:"+level.Slot, config, document.Default, override)
	}
	return nil
}

func buildZoneRuleReport(key string, config, baseline zoneRuleConfiguration, override bool) *zoneRuleReport {
	r := &zoneRuleReport{Key: key, Override: override, resolved: config.rules(key)}
	comparison := r.resolved
	comparison.Key = "default"
	r.Modified = !reflect.DeepEqual(comparison, baseline.rules("default"))
	r.Groups = zoneRuleGroups(r.resolved)
	defaults := zoneRuleGroups(baseline.rules("default"))
	for i := range r.Groups {
		for j := range r.Groups[i].Rows {
			row := &r.Groups[i].Rows[j]
			row.Default = defaults[i].Rows[j].Value
			row.Changed = row.Value != row.Default
			if row.Changed {
				r.ChangedCount++
			}
		}
	}
	return r
}

func zoneRuleGroups(r game.ZoneRules) []zoneRuleGroup {
	row := func(label string, value any) zoneRuleRow { return zoneRuleRow{Label: label, Value: fmt.Sprint(value)} }
	onOff := func(enabled bool) string {
		if enabled {
			return "Enabled"
		}
		return "Disabled"
	}
	seconds := func(ms uint32) string { return strconv.FormatFloat(float64(ms)/1000, 'f', -1, 64) + " s" }
	label := func(value string) string {
		if text, ok := map[string]string{"current_coop": "Co-op roster", "authored_external": "Authored external roster", "current_composition": "Campaign composition", "local_and_external": "Local + external", "base": "Base health", "run_only": "Difficulty", "run_and_party": "Difficulty + players", "recipient": "Recipient", "source": "Source"}[value]; ok {
			return text
		}
		return value
	}
	p, a, b, m := r.EnemyPopulation, r.AllyAlert, r.BossSpawning, r.MutationAgent
	completion := "All waves cleared"
	if m.IsAgentDefeatCompleting {
		completion = "Mutation Agent defeated"
	}
	return []zoneRuleGroup{
		{Name: "Enemy population", Rows: []zoneRuleRow{row("Enemies per section", r.EnemyTargetPerSection), row("Agent selection", label(p.AgentSelection)), row("Agents per section", p.AgentsPerSection), row("Roster selection", label(p.RosterSelection)), row("External roster weight", fmt.Sprintf("%d%%", p.ExternalRosterWeightPercent)), row("Ambient elite chance", fmt.Sprintf("%d%%", p.AmbientEliteChancePercent)), row("Summon health", label(p.OwnedSummonHealthScaling)), row("Destructible health", label(p.FixtureHealthScaling))}},
		{Name: "Ally alerts", Rows: []zoneRuleRow{row("Ally alerts", onOff(a.IsEnabled)), row("Alert range", fmt.Sprintf("%d%%", a.RangePercent)), row("Range owner", label(a.RangeOwner)), row("Maximum hops", a.MaxHops)}},
		{Name: "Boss spawning", Rows: []zoneRuleRow{row("Opening adds per anchor", b.OpeningAddsPerAnchor), row("Initial boss reinforcements", b.InitialBossAddCount), row("Captain delay", seconds(b.CaptainDelayMillisecond))}},
		{Name: "Hordes & Mutation Agents", Rows: []zoneRuleRow{row("Mutation Agents", onOff(m.IsEnabled)), row("Completed missions to unlock", m.MinimumChainLevel), row("Horde waves", m.WaveCount), row("Mutation Agent wave", m.SpawnWave), row("Delay between waves", seconds(m.InterWaveDelayMillisecond)), row("Horde completes when", completion)}},
	}
}
