package main

import (
	"reflect"
	"testing"
)

func TestMapSectionsFollowEntryAndTeleportRoutes(t *testing.T) {
	link := func(id uint32) seededMapObject {
		return seededMapObject{Kind: "teleport", Destination: &id}
	}
	levelMap := campaignMap{Sections: []campaignMapSection{
		{ComponentID: 0, Objects: []seededMapObject{link(4)}},
		{ComponentID: 1}, // Disconnected: keep it after the reachable route.
		{ComponentID: 2, Objects: []seededMapObject{link(3)}},
		{ComponentID: 3, Objects: []seededMapObject{link(2)}},
		{ComponentID: 4, Objects: []seededMapObject{{Kind: "entry"}, link(2), link(0), link(99)}},
	}}
	orderSeededMapSections(&levelMap)
	var got []uint32
	for _, section := range levelMap.Sections {
		got = append(got, section.ComponentID)
	}
	if want := []uint32{4, 2, 3, 0, 1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("component order = %v, want %v", got, want)
	}
}
