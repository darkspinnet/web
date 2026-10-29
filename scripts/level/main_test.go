package main

import (
	"database/sql"
	"fmt"
	_ "modernc.org/sqlite"
	"testing"
)

func TestProgressionCoversAllPassesAndBoundaries(t *testing.T) {
	levels := []levelReport{{Name: "tutorial", Slug: "tutorial"}}
	for n := 24; n >= 1; n-- {
		levels = append(levels, levelReport{Name: fmt.Sprintf("map%d", n), Slug: fmt.Sprintf("map-%d", n), CampaignOrders: []int{n, n + 24, n + 48}})
	}
	reports := expandProgression(levels)
	groups := buildGroups(reports)
	if len(reports) != 73 || len(groups) != 4 {
		t.Fatalf("reports=%d groups=%d", len(reports), len(groups))
	}
	for pass, mode := range []string{"Onslaught", "Invasion", "Apocalypse"} {
		if groups[pass].Name != mode || groups[pass].Count != 24 {
			t.Fatalf("bad pass: %+v", groups[pass])
		}
		for i, report := range groups[pass].Levels {
			if report.Progression != pass*24+i+1 {
				t.Fatalf("wrong order: %s", report.DisplayName)
			}
		}
	}
	last := groups[0].Levels[23]
	first := groups[1].Levels[0]
	if last.Next != first.Slug || first.Previous != last.Slug {
		t.Fatal("pass boundary navigation is disconnected")
	}
	if groups[0].Levels[0].Slug != "map-1" || first.Slot != "1-1" || first.Slug != "invasion-map-1" {
		t.Fatal("canonical URLs or stage reset changed")
	}
	if groups[2].Levels[23].Next != "" {
		t.Fatal("final mission should not link past Apocalypse")
	}
}

func TestFirstClearPoolsChangeWithDifficulty(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE level_director_entry(level_id INTEGER,configuration_ordinal INTEGER,configuration_entry_ordinal INTEGER,config_kind TEXT,noun_name TEXT,is_horde_legal INTEGER,minimum_difficulty INTEGER,maximum_difficulty INTEGER,configuration_name TEXT);
 INSERT INTO level_director_entry VALUES(1,0,0,'minion','Fallback.Noun',1,1,72,'levelConfig'),(1,5,0,'minion','Fish.Noun',1,1,24,'firstTimeConfig'),(1,5,1,'minion','Fish_2.Noun',1,25,48,'firstTimeConfig'),(1,5,2,'minion','Fish_3.Noun',1,49,72,'firstTimeConfig');`)
	if err != nil {
		t.Fatal(err)
	}
	for i, difficulty := range []int{1, 25, 49} {
		nouns, err := loadCampaignPoolNouns(db, 1, difficulty)
		if err != nil {
			t.Fatal(err)
		}
		expected := []string{"Fish.Noun", "Fish_2.Noun", "Fish_3.Noun"}[i]
		if len(nouns) != 1 || nouns[0].Noun != expected {
			t.Fatalf("difficulty %d: got %+v, want %s", difficulty, nouns, expected)
		}
	}
}
