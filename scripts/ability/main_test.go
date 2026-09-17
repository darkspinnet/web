package main

import "testing"

func TestBuildGroupsAbilityOwnersAndVariants(t *testing.T) {
	heroes := []hero{
		{Family: "Sage", Variant: "Alpha", Element: "BIO", Class: "TEMPEST", Abilities: []ability{{Slot: "basic", AssetName: "SupportHealerBasic", Name: "Support Healer Basic", Slug: "supporthealerbasic"}}},
		{Family: "Sage", Variant: "Beta", Element: "BIO", Class: "TEMPEST", Abilities: []ability{{Slot: "basic", AssetName: "SupportHealerBasic", Name: "Support Healer Basic", Slug: "supporthealerbasic"}}},
	}
	result := build(heroes)
	if result.Count != 1 {
		t.Fatalf("count = %d, want 1", result.Count)
	}
	entry := result.Entry["supporthealerbasic"]
	if entry.UsageCount != 2 {
		t.Fatalf("usage = %d, want 2", entry.UsageCount)
	}
	if len(entry.Heroes) != 1 || len(entry.Heroes[0].Variants) != 2 {
		t.Fatalf("owners = %#v", entry.Heroes)
	}
}

func TestResolveDescriptionUsesDecodedRankData(t *testing.T) {
	description := resolveDescription(
		"Places a tree within ~radius~m for ~healing~ health. After ~duration~ seconds it heals ~minHealing~-~maxHealing~ health.",
		&combatRecord{RadiusMeters: 8, MinimumHealingPerTick: 3, MaximumHealingPerTick: 3, DurationSeconds: 6, MinimumFinalHealing: 12, MaximumFinalHealing: 18},
		[]owner{{Name: "Sage"}},
	)
	want := "Places a tree within 8m for 3 health. After 6 seconds it heals 12-18 health."
	if description != want {
		t.Fatalf("description = %q, want %q", description, want)
	}
}
