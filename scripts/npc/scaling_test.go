package main

import (
	"database/sql"
	"testing"
)

func TestEnemyPreviewPreservesVariantRankAndPerClassPartyScaling(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`CREATE TABLE difficulty_tuning(difficulty INTEGER,health_multiplier REAL,damage_multiplier REAL);
	CREATE TABLE non_player_noun(noun_name TEXT,non_player_class_resource_id INTEGER);
	CREATE TABLE non_player_class(content_source_resource_id INTEGER,npc_rank INTEGER,player_count_health_scale REAL,mind REAL,npc_type INTEGER);
	INSERT INTO non_player_noun VALUES('ZelemBasicRanged_2.Noun',1),('Door.Noun',2);
	INSERT INTO non_player_class VALUES(1,2,.5,1,0),(2,1,0,0,3);`)
	if err != nil {
		t.Fatal(err)
	}
	for stage := 1; stage <= 72; stage++ {
		if _, err = db.Exec(`INSERT INTO difficulty_tuning VALUES(?,?,?)`, stage, float64(stage), float64(stage)*2); err != nil {
			t.Fatal(err)
		}
	}
	result := npcCatalog{Entry: map[string]npcDetail{"zelembasicranged-2-noun": {Category: "combat"}, "door-noun": {Category: "destructables"}}}
	scaling, err := enrichNPCCombat(db, &result)
	if err != nil {
		t.Fatal(err)
	}
	if len(scaling.Difficulties) != 72 || scaling.Difficulties[24].Stage != 25 || scaling.Difficulties[48].Stage != 49 {
		t.Fatalf("campaign progression: %+v", scaling)
	}
	for i, want := range []float32{1, 1.4, 1.8, 2.2} {
		if scaling.PartyDamage[i] != want {
			t.Fatalf("%d players: got %g, want %g", i+1, scaling.PartyDamage[i], want)
		}
	}
	fish := result.Entry["zelembasicranged-2-noun"]
	if fish.NPCRank != 2 || fish.PlayerCountHealthScale != .5 || fish.HealthScaling != "run_and_party" {
		t.Fatalf("variant metadata: %+v", fish)
	}
	if len(fish.AbilityPreviews) != 2 {
		t.Fatalf("shot and blink must both be present: %+v", fish.AbilityPreviews)
	}
	var shot, blink npcAbilityPreview
	for _, preview := range fish.AbilityPreviews {
		if preview.AssetName == "ZelemBasicRanged" {
			shot = preview
		}
		if preview.AssetName == "ZelemBasicRanged_Blink" {
			blink = preview
		}
	}
	if shot.MinimumDamage <= 0 || shot.MaximumDamage < shot.MinimumDamage || shot.Cooldown != 4 || shot.Range != 15 || blink.Cooldown != 6 {
		t.Fatalf("incomplete action previews: shot=%+v blink=%+v", shot, blink)
	}
	if fixture := result.Entry["door-noun"]; fixture.HealthScaling != "base" || fixture.PlayerCountHealthScale != 0 {
		t.Fatalf("fixture policy/zero coefficient lost: %+v", fixture)
	}
	_, err = db.Exec("DELETE FROM difficulty_tuning WHERE difficulty=72")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = enrichNPCCombat(db, &result); err == nil {
		t.Fatal("accepted incomplete campaign tuning")
	}
}
