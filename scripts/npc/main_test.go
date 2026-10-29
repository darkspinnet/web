package main

import (
	"database/sql"
	_ "modernc.org/sqlite"
	"testing"
)

func TestCatalogIncludesUnplacedCreaturesAndDestructables(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`
 CREATE TABLE non_player_class(content_source_resource_id INTEGER,instance_id INTEGER,hit_point REAL,display_name TEXT,description TEXT,npc_type INTEGER,is_player_pet INTEGER,creature_type INTEGER);
 CREATE TABLE non_player_noun(content_source_resource_id INTEGER,noun_name TEXT,non_player_class_resource_id INTEGER);
 CREATE TABLE noun_navigation(content_source_resource_id INTEGER,noun_type INTEGER);
 CREATE TABLE level(id INTEGER,name TEXT);
 CREATE TABLE marker(noun_name TEXT,level_marker_set_id INTEGER);
 CREATE TABLE level_marker_set(id INTEGER,level_id INTEGER);
 CREATE TABLE level_director_entry(level_id INTEGER,noun_name TEXT);
 INSERT INTO non_player_class VALUES(1,100,20,'Space Barracuda','Bites',0,0,1),(2,101,200,'Door','Destroy me',3,0,0),(3,102,10,'Helper','',0,0,0),(4,103,10,'Pet','',0,1,0);
 INSERT INTO non_player_noun VALUES(1,'Fish.Noun',1),(2,'Door.Noun',2),(3,'Controller.Noun',3),(4,'Pet.Noun',4),(5,'FishAlias.Noun',1);
 INSERT INTO noun_navigation VALUES(1,113409280),(2,1317812),(3,1214237073),(4,113409280),(5,113409280);
 `)
	if err != nil {
		t.Fatal(err)
	}
	result, err := loadNPCs(db)
	if err != nil {
		t.Fatal(err)
	}
	if result.EntryCount != 3 || result.LevelCount != 0 {
		t.Fatalf("unplaced noun catalog: entries=%d levels=%d", result.EntryCount, result.LevelCount)
	}
	if got := result.Entry["fishalias-noun"]; got.Name != "Space Barracuda" || got.Element != "quantum" || *got.HitPoints != 20 {
		t.Fatalf("shared class/localized identity lost: %+v", got)
	}
	if got := result.Entry["door-noun"]; got.Category != "destructables" || got.IsCombatant {
		t.Fatalf("door classified as creature: %+v", got)
	}
	for _, slug := range []string{"controller-noun", "pet-noun"} {
		if _, ok := result.Entry[slug]; ok {
			t.Fatalf("non-enemy indexed: %s", slug)
		}
	}
}
