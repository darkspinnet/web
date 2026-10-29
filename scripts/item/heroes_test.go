package main

import (
	"github.com/darkspinnet/web/scripts/internal/catalog"
	"reflect"
	"testing"
)

func TestSupportedHeroesRespectOwnerElementClassAndAnatomy(t *testing.T) {
	heroes := []compatibilityHero{
		{Name: "Orion", Slug: "orion", Variants: []compatibilityProfile{{ID: catalog.HashID("PC_Orion.Noun"), Variant: "Alpha", Class: "TEMPEST", Element: "CHRONO", HasHand: true, HasFoot: true}, {ID: catalog.HashID("PC_Orion_v1.Noun"), Variant: "Beta", Class: "TEMPEST", Element: "CHRONO", HasHand: true, HasFoot: true}}},
		{Name: "Other Tempest", Slug: "other", Variants: []compatibilityProfile{{ID: 42, Variant: "Alpha", Class: "TEMPEST", Element: "CHRONO", HasHand: false, HasFoot: false}}},
		{Name: "Wrong Class", Slug: "wrong-class", Variants: []compatibilityProfile{{ID: 43, Variant: "Alpha", Class: "RAVAGER", Element: "CHRONO", HasHand: true, HasFoot: true}}},
		{Name: "Wrong Element", Slug: "wrong-element", Variants: []compatibilityProfile{{ID: 44, Variant: "Alpha", Class: "TEMPEST", Element: "CYBER", HasHand: true, HasFoot: true}}},
	}
	base := item{Category: "Weapon", Classes: []string{"TEMPEST"}, Elements: []string{"CHRONO"}, Creatures: []string{"PC_Orion.Noun", "PC_Orion_v1.Noun"}}
	got := supportedHeroes(base, heroes)
	if len(got) != 1 || got[0].Slug != "orion" || !reflect.DeepEqual(got[0].Variants, []string{"Alpha", "Beta"}) {
		t.Fatalf("explicit owner: %+v", got)
	}
	base.Creatures = nil
	base.Category = "Defense"
	if got = supportedHeroes(base, heroes); len(got) != 2 {
		t.Fatalf("class/element matching: %+v", got)
	}
	for _, category := range []string{"Grasper", "Foot"} {
		base.Category = category
		if got = supportedHeroes(base, heroes); len(got) != 1 || got[0].Slug != "orion" {
			t.Fatalf("%s anatomy: %+v", category, got)
		}
	}
	base.Category = "Weapon"
	base.Creatures = []string{"PC_Orion_v1.Noun"}
	if got = supportedHeroes(base, heroes); len(got) != 1 || !reflect.DeepEqual(got[0].Variants, []string{"Beta"}) {
		t.Fatalf("restricted variant: %+v", got)
	}
	base.Creatures = nil
	base.Elements = nil
	if got = supportedHeroes(base, heroes); len(got) != 0 {
		t.Fatalf("unknown masks should not claim compatibility: %+v", got)
	}
}

func TestVariantDeduplicationPreservesDifferentHeroRestrictions(t *testing.T) {
	records := []itemVariantRecord{
		{ID: 1, Elements: []string{"BIO"}, SupportedHeroes: []supportedHero{{Slug: "sage", Variants: []string{"Alpha"}}}},
		{ID: 2, Elements: []string{"BIO"}, SupportedHeroes: []supportedHero{{Slug: "tork", Variants: []string{"Alpha"}}}},
	}
	if got := dedupeVariantRecords(records); len(got) != 2 {
		t.Fatalf("distinct hero restrictions collapsed: %+v", got)
	}
}
