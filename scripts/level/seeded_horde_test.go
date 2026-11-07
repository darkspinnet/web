package main

import (
	"reflect"
	"testing"

	"github.com/darkspinnet/darkspin/server/game"
)

func TestFixedHordePreviewUsesExactRostersAndEligibleFinalAgent(t *testing.T) {
	director := game.CampaignDirector{NPCProfilesByNoun: map[string]game.CampaignNPCProfile{
		"first.noun": {IsKnown: true, HitPoint: 10}, "second.noun": {IsKnown: true, HitPoint: 10}, "mutationagent.noun": {IsKnown: true, HitPoint: 10},
	}, ZoneRules: game.ZoneRules{MutationAgent: game.ZoneMutationAgentRules{
		IsEnabled: true, MinimumChainLevel: 4, WaveCount: 3, SpawnWave: 3,
		FixedWaves: [][]game.ZoneHordeEnemy{{{NounName: "First.Noun", Count: 6}}, {{NounName: "Second.Noun", Count: 2}}, {{NounName: "First.Noun", Count: 4}, {NounName: "Second.Noun", Count: 4}}},
	}}}
	publication := game.CampaignDirectorPublication{
		MarkerSetName: "fixed.Markerset", TriggerMarkerID: 1, EventName: "horde triggered", CallbackName: "HordeTrigger_OnEnterPlayer",
		Listeners: []game.CampaignDirectorListenerPublication{{MarkerID: 2, CallbackName: "HordeSpawner_Register", SpawnKind: 5, PoolKind: "agent", IsSpawnKindKnown: true}},
	}
	waves, err := planSeededHorde(director, publication, nil, 4, 1)
	if err != nil {
		t.Fatal(err)
	}
	var counts []int
	for _, wave := range waves {
		counts = append(counts, wave.Count)
	}
	if !reflect.DeepEqual(counts, []int{6, 2, 9}) {
		t.Fatalf("wave counts = %v", counts)
	}
	if !reflect.DeepEqual(waves[2].Enemies, []seededHordeEnemy{{Slug: "first-noun", Count: 4}, {Slug: "mutationagent-noun", Count: 1}, {Slug: "second-noun", Count: 4}}) {
		t.Fatalf("final composition = %+v", waves[2].Enemies)
	}
	locked, err := planSeededHorde(director, publication, nil, 3, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(locked) != 1 || locked[0].Count != 6 {
		t.Fatalf("locked preview = %+v", locked)
	}
}
