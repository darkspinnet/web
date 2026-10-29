package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestEnemyReferencesIncludeActionsAndLifecycleButNotConditions(t *testing.T) {
	var def enemyDefinition
	if err := json.Unmarshal([]byte(`{"Nodes":[{"Phase":"Fish.Phase","Condition":"NeverAnAbility.Condition"}],"PassiveAbility":"FishArmor","DeathAbility":"FishExplosion","FirstAggroAbility":"FaceTarget","FirstAggroAbility2":"FaceTarget","FirstAlertAbility":null,"DeathCondition":"NeverAnAbility","CombatIdle":"IdleAnimation"}`), &def); err != nil {
		t.Fatal(err)
	}
	var phase enemyPhase
	if err := json.Unmarshal([]byte(`{"Gambits":[{"Condition":"HasTarget","Ability":"FishBite"},{"Ability":"FishBlink"},{"Ability":"FishBite"},{"Ability":null}]}`), &phase); err != nil {
		t.Fatal(err)
	}
	refs, err := enemyReferences(def, map[string]enemyPhase{"fish.phase": phase})
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string][]string{"FishArmor": {"passive"}, "FishExplosion": {"death"}, "FaceTarget": {"first_aggro"}, "FishBite": {"action"}, "FishBlink": {"action"}}
	if !reflect.DeepEqual(refs, expected) {
		t.Fatalf("references=%+v, want %+v", refs, expected)
	}
}

func TestEnemyReferencesPreserveKnownAbilitiesWhenPhaseIsMissing(t *testing.T) {
	var def enemyDefinition
	json.Unmarshal([]byte(`{"DeathAbility":"Destructible","Nodes":[{"Phase":"Missing.Phase"},{"Phase":"Present.Phase"}]}`), &def)
	var phase enemyPhase
	json.Unmarshal([]byte(`{"Gambits":[{"Ability":"Blast"}]}`), &phase)
	refs, err := enemyReferences(def, map[string]enemyPhase{"present.phase": phase})
	if err == nil || len(refs) != 2 {
		t.Fatalf("known references lost or missing phase hidden: refs=%v err=%v", refs, err)
	}
}

func TestEnemyAbilitiesMergeSharedOwnershipAndReindexCounts(t *testing.T) {
	result := build([]hero{{Family: "Sage", Variant: "Alpha", Abilities: []ability{{Slot: "passive", AssetName: "SharedArmor", Slug: "sharedarmor"}}}})
	owned := npcAbilityOwner{Name: "Armored Fish", Slug: "fish-noun", Element: "quantum"}
	refs := map[string][]string{"sharedarmor": {"passive"}, "FishBite": {"action"}}
	mergeEnemyAbilities(&result, owned, refs)
	mergeEnemyAbilities(&result, owned, refs)
	reindex(&result)
	if result.Count != 2 || result.HeroCount != 1 || result.EnemyCount != 2 {
		t.Fatalf("counts=%d hero=%d enemy=%d", result.Count, result.HeroCount, result.EnemyCount)
	}
	shared := result.Entry["sharedarmor"]
	if len(shared.Heroes) != 1 || len(shared.Enemies) != 1 || shared.PrimarySlot != "passive" || shared.UsageCount != 2 {
		t.Fatalf("shared ownership not preserved: %+v", shared)
	}
	if result.Slots["enemy_action"] != 1 || result.Slots["passive"] != 1 {
		t.Fatalf("slot counts=%v", result.Slots)
	}
}
