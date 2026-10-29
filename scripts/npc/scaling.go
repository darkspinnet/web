package main

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/server/game"
	"github.com/darkspinnet/darkspin/server/sim"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	"github.com/darkspinnet/web/scripts/internal/catalog"
)

type npcDifficulty struct {
	Stage  int     `json:"stage"`
	Health float32 `json:"health"`
	Damage float32 `json:"damage"`
}
type npcScaling struct {
	Difficulties []npcDifficulty `json:"difficulties"`
	PartyDamage  []float32       `json:"party_damage"`
}
type npcAbilityPreview struct {
	AssetName         string  `json:"asset_name"`
	MinimumDamage     float32 `json:"minimum_damage,omitempty"`
	MaximumDamage     float32 `json:"maximum_damage,omitempty"`
	MinimumHealing    float32 `json:"minimum_healing,omitempty"`
	MaximumHealing    float32 `json:"maximum_healing,omitempty"`
	MinimumTickDamage float32 `json:"minimum_tick_damage,omitempty"`
	MaximumTickDamage float32 `json:"maximum_tick_damage,omitempty"`
	Cooldown          float64 `json:"cooldown,omitempty"`
	Range             float32 `json:"range,omitempty"`
	Duration          float64 `json:"duration,omitempty"`
	TickSeconds       float64 `json:"tick_seconds,omitempty"`
}

func enrichNPCCombat(database *sql.DB, result *npcCatalog) (npcScaling, error) {
	scaling := npcScaling{Difficulties: []npcDifficulty{}, PartyDamage: []float32{}}
	rows, err := database.Query(`SELECT difficulty,health_multiplier,damage_multiplier FROM difficulty_tuning ORDER BY difficulty`)
	if err != nil {
		return scaling, err
	}
	for rows.Next() {
		var d npcDifficulty
		if err = rows.Scan(&d.Stage, &d.Health, &d.Damage); err != nil {
			rows.Close()
			return scaling, err
		}
		if d.Stage != len(scaling.Difficulties)+1 || d.Health <= 0 || d.Damage <= 0 {
			rows.Close()
			return scaling, fmt.Errorf("invalid difficulty stage %d", d.Stage)
		}
		scaling.Difficulties = append(scaling.Difficulties, d)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return scaling, err
	}
	rows.Close()
	if len(scaling.Difficulties) != 72 {
		return scaling, fmt.Errorf("expected 72 difficulty stages, got %d", len(scaling.Difficulties))
	}
	neutral := sim.DifficultyCombatTuning{StarHealthBase: 1, StarDamageBase: 1, HealthMultiplier: []float32{1}, DamageMultiplier: []float32{1}}
	for players := uint32(1); players <= 4; players++ {
		v, err := sim.ProjectNonPlayerDamageForParty(1, 1, 0, players, neutral)
		if err != nil {
			return scaling, err
		}
		scaling.PartyDamage = append(scaling.PartyDamage, v)
	}
	rows, err = database.Query(`SELECT n.noun_name,c.npc_rank,c.player_count_health_scale,c.mind,c.npc_type FROM non_player_noun n JOIN non_player_class c ON c.content_source_resource_id=n.non_player_class_resource_id`)
	if err != nil {
		return scaling, err
	}
	defer rows.Close()
	for rows.Next() {
		var noun string
		var rank int32
		var coefficient, mind float32
		var npcType uint32
		if err = rows.Scan(&noun, &rank, &coefficient, &mind, &npcType); err != nil {
			return scaling, err
		}
		slug := catalog.Slug(noun)
		entry, ok := result.Entry[slug]
		if !ok {
			continue
		}
		plan := zonenpc.SpawnPlan{NounName: noun, NPCProfile: game.CampaignNPCProfile{NPCRank: rank, Mind: mind, NPCType: npcType}}
		entry.NPCRank = int(zonenpc.EffectiveNPCRank(plan))
		entry.PlayerCountHealthScale = coefficient
		entry.HealthScaling = "run_and_party"
		if entry.Category == "destructables" {
			entry.HealthScaling = game.DefaultEnemyPopulationRules().FixtureHealthScaling
		}
		previews := []npcAbilityPreview{}
		seen := map[string]bool{}
		add := func(profile zonenpc.ActionProfile) {
			profile = zonenpc.RankedActionProfile(profile, plan)
			if profile.AbilityName == "" || seen[profile.AbilityName] {
				return
			}
			seen[profile.AbilityName] = true
			previews = append(previews, previewNPCAbility(plan, profile))
		}
		if profile, ok := zonenpc.RecoveredActionProfileForPlan(plan); ok {
			add(profile)
			// Barracuda's phase action is Blink; its shared action family also
			// owns the separate ranged shot used between teleports.
			if profile.Family == zonenpc.ActionZelemRanged {
				add(zonenpc.ZelemRangedShotProfile())
			}
			if profile.Family == zonenpc.ActionZelemHaster {
				add(zonenpc.ZelemHasterAttackProfile())
			}
		}
		for _, spell := range zonenpc.SupportSpells(plan) {
			add(spell.Profile)
		}
		if profile, ok := zonenpc.MerakStormProfile(plan); ok {
			add(profile)
		}
		entry.AbilityPreviews = previews
		result.Entry[slug] = entry
	}
	if err := rows.Err(); err != nil {
		return scaling, err
	}
	linkNPCVariantFamilies(result)
	return scaling, nil
}

func linkNPCVariantFamilies(result *npcCatalog) {
	for slug, entry := range result.Entry {
		entry.FamilySlug = slug
		stem := strings.TrimSuffix(strings.ToLower(entry.AssetName), ".noun")
		if (entry.NPCRank == 2 && strings.HasSuffix(stem, "_2")) || (entry.NPCRank == 3 && strings.HasSuffix(stem, "_3")) {
			baseSlug := catalog.Slug(stem[:len(stem)-2] + ".Noun")
			if base, ok := result.Entry[baseSlug]; ok && base.NPCRank == 1 && base.Category == entry.Category {
				entry.FamilySlug = baseSlug
			}
		}
		result.Entry[slug] = entry
	}
}

func previewNPCAbility(plan zonenpc.SpawnPlan, p zonenpc.ActionProfile) npcAbilityPreview {
	result := npcAbilityPreview{AssetName: p.AbilityName, Cooldown: p.Cooldown.Seconds(), Range: p.Range, Duration: p.ModifierDuration.Seconds(), TickSeconds: p.ModifierTickDuration.Seconds()}
	snapshot := zonenpc.Snapshot{Plan: plan}
	damageProfile := zonenpc.DamageProfileAt(snapshot, time.Time{})
	project := func(minimum, maximum, coefficient float32, descriptor uint32, kind, source uint8) (float32, float32) {
		if minimum <= 0 || maximum < minimum {
			return 0, 0
		}
		damage, err := game.ResolveAbilityDamageRange(game.AbilityDamage{Minimum: minimum, Maximum: maximum, Coefficient: coefficient, Descriptor: descriptor, DamageType: kind, DamageSource: source, IsDescriptorFound: true, IsDamageTypeFound: true, IsDamageSourceFound: true}, damageProfile)
		if err != nil {
			return 0, 0
		}
		return damage.Minimum, damage.Maximum
	}
	descriptor, kind, source := uint32(1|1<<6), uint8(0), uint8(0)
	if p.IsDamageProfileKnown {
		descriptor, kind, source = p.DescriptorMask, p.DamageType, p.DamageSource
	}
	result.MinimumDamage, result.MaximumDamage = project(p.MinimumDamage, p.MaximumDamage, p.DamageCoefficient, descriptor, kind, source)
	if p.IsModifierDamageProfileKnown {
		minimum, maximum := p.ModifierMinimumTickDamage, p.ModifierMaximumTickDamage
		if minimum == 0 && p.ModifierTickDamage > 0 {
			minimum = p.ModifierTickDamage
			maximum = minimum
		}
		result.MinimumTickDamage, result.MaximumTickDamage = project(minimum, maximum, p.ModifierTickDamageCoefficient, p.ModifierDescriptorMask, p.ModifierDamageType, p.ModifierDamageSource)
	}
	healing := func(amount float32) float32 {
		if amount <= 0 {
			return 0
		}
		v, err := game.ResolveAbilityHealing(game.AbilityHealing{Amount: amount, Coefficient: p.HealingCoefficient, Descriptor: p.DescriptorMask, IsDescriptorFound: true}, game.HealingProfile{PrimaryAttribute: plan.NPCProfile.Mind, IsPrimaryAttributeFound: true})
		if err != nil {
			return 0
		}
		return v
	}
	result.MinimumHealing = healing(p.MinimumHealing)
	result.MaximumHealing = healing(p.MaximumHealing)
	return result
}
