package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

const testZoneRules = `{"default":{"enemies_per_section":15,"enemy_population":{"agent_selection":"current_coop","agents_per_section":1,"roster_selection":"current_composition","external_roster_weight_percent":50,"ambient_elite_chance_percent":0,"owned_summon_health_scaling":"run_and_party","fixture_health_scaling":"base"},"ally_alert":{"range_percent":100,"range_owner":"recipient","max_hops":1},"boss_spawning":{"opening_adds_per_anchor":1,"initial_boss_add_count":2,"captain_delay_millisecond":2000},"mutation_agent":{"minimum_chain_level":7,"wave_count":3,"spawn_wave":3,"inter_wave_delay_millisecond":500,"completion":"agent_defeat"}},"campaign":{},"replay":{}}`

func TestZoneRuleOverridesReplaceDefaultsAndHighlightDifferences(t *testing.T) {
	var document zoneRuleDocument
	if err := json.Unmarshal([]byte(testZoneRules), &document); err != nil {
		t.Fatal(err)
	}
	override := document.Default
	override.EnemiesPerSection = 24
	override.AllyAlert, override.MutationAgent = nil, nil
	document.Campaign["7-1"] = override
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "zone-rules.json")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	levels := []levelReport{{Slot: "1-1", Campaign: &campaignDetail{}}, {Slot: "7-1", Campaign: &campaignDetail{}}}
	if err = enrichZoneRules(path, levels); err != nil {
		t.Fatal(err)
	}
	base, changed := levels[0].ZoneRules, levels[1].ZoneRules
	if base.Override || base.ChangedCount != 0 || base.resolved.EnemyTargetPerSection != 15 {
		t.Fatalf("bad fallback: %+v", base)
	}
	if !changed.Override || changed.resolved.EnemyTargetPerSection != 24 || changed.resolved.AllyAlert.IsEnabled || changed.resolved.MutationAgent.IsEnabled {
		t.Fatalf("override was merged rather than replaced: %+v", changed)
	}
	row := changed.Groups[0].Rows[0]
	if !row.Changed || row.Default != "15" || row.Value != "24" {
		t.Fatalf("bad highlight: %+v", row)
	}
}

func TestZoneRuleLoaderRejectsInvalidDocuments(t *testing.T) {
	for name, transform := range map[string]func(map[string]any){
		"unknown property": func(d map[string]any) { d["unexpected"] = true },
		"invalid mission":  func(d map[string]any) { d["campaign"] = map[string]any{"19-1": d["default"]} },
		"partial override": func(d map[string]any) {
			d["campaign"] = map[string]any{"1-1": map[string]any{"enemies_per_section": 20}}
		},
		"invalid completion": func(d map[string]any) {
			d["default"].(map[string]any)["mutation_agent"].(map[string]any)["completion"] = "anything"
		},
	} {
		t.Run(name, func(t *testing.T) {
			var document map[string]any
			if err := json.Unmarshal([]byte(testZoneRules), &document); err != nil {
				t.Fatal(err)
			}
			transform(document)
			raw, err := json.Marshal(document)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "zone-rules.json")
			if err = os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err = loadZoneRules(path); err == nil {
				t.Fatal("invalid rules accepted")
			}
		})
	}
}
