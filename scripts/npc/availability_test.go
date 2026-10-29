package main

import "testing"

func TestVariantFamiliesRequireAnAuthoredBaseAndMatchingRank(t *testing.T) {
	result := npcCatalog{Entry: map[string]npcDetail{
		"cryosboss-noun":   {AssetName: "CryosBoss.Noun", NPCRank: 1, Category: "combat"},
		"cryosboss-2-noun": {AssetName: "CryosBoss_2.Noun", NPCRank: 2, Category: "combat"},
		"cryosboss-3-noun": {AssetName: "CryosBoss_3.noun", NPCRank: 3, Category: "combat"},
		"unrelated-2-noun": {AssetName: "Unrelated_2.Noun", NPCRank: 2, Category: "combat"},
		"fixture-noun":     {AssetName: "Fixture.Noun", NPCRank: 1, Category: "destructables"},
		"fixture-2-noun":   {AssetName: "Fixture_2.Noun", NPCRank: 2, Category: "combat"},
		"same-rank-2-noun": {AssetName: "SameRank_2.Noun", NPCRank: 1, Category: "combat"},
	}}
	linkNPCVariantFamilies(&result)
	for _, slug := range []string{"cryosboss-noun", "cryosboss-2-noun", "cryosboss-3-noun"} {
		if got := result.Entry[slug].FamilySlug; got != "cryosboss-noun" {
			t.Fatalf("%s family=%s", slug, got)
		}
	}
	for _, slug := range []string{"unrelated-2-noun", "fixture-2-noun", "same-rank-2-noun"} {
		if got := result.Entry[slug].FamilySlug; got != slug {
			t.Fatalf("unrelated variant grouped: %s -> %s", slug, got)
		}
	}
}
