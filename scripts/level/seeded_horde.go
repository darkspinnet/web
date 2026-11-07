package main

import (
	"fmt"
	"sort"

	"github.com/darkspinnet/darkspin/server/game"
	zonehorde "github.com/darkspinnet/darkspin/server/zone/horde"
	zonenpc "github.com/darkspinnet/darkspin/server/zone/npc"
	zonepopulation "github.com/darkspinnet/darkspin/server/zone/population"
	"github.com/darkspinnet/web/scripts/internal/catalog"
)

type seededMapHorde struct {
	Number int               `json:"number"`
	X      int               `json:"x"`
	Y      int               `json:"y"`
	Total  int               `json:"total"`
	Waves  []seededHordeWave `json:"waves"`
}

type seededHordeWave struct {
	Label   string             `json:"label,omitempty"`
	Number  int                `json:"number"`
	Count   int                `json:"count"`
	Enemies []seededHordeEnemy `json:"enemies"`
}

type seededBossWaves struct {
	Name  string            `json:"name"`
	Total int               `json:"total"`
	Waves []seededHordeWave `json:"waves"`
}

func summarizeSeededWave(number int, plans []zonenpc.SpawnPlan) seededHordeWave {
	wave := seededHordeWave{Number: number, Count: len(plans)}
	counts := make(map[string]int)
	names := make(map[string]string)
	for _, plan := range plans {
		slug := catalog.Slug(plan.NounName)
		counts[slug]++
		if plan.IsBoss {
			names[slug] = plan.BossIdentity.DisplayName
		}
	}
	for slug, count := range counts {
		wave.Enemies = append(wave.Enemies, seededHordeEnemy{Slug: slug, Count: count, Name: names[slug]})
	}
	sort.Slice(wave.Enemies, func(i, j int) bool { return wave.Enemies[i].Slug < wave.Enemies[j].Slug })
	return wave
}

type seededHordeEnemy struct {
	Name  string `json:"name,omitempty"`
	Slug  string `json:"slug"`
	Count int    `json:"count"`
}

func addSeededMapHordes(levelMap *campaignMap, nav seededMapGeometry, director game.CampaignDirector, sections []zonepopulation.SectionRoster, completedStages uint32) error {
	number := 0
	for _, set := range director.MarkerSets {
		for _, trigger := range set.Triggers {
			isHorde := false
			for _, event := range trigger.Events {
				if event.CallbackName == "HordeTrigger_OnEnterPlayer" && event.EventName == "horde triggered" {
					isHorde = true
				}
			}
			if !isHorde {
				continue
			}
			// Resolve listeners with the same selected-layout director session as
			// gameplay, rather than collecting unselected authored alternatives.
			session, err := game.NewCampaignDirectorSession(director)
			if err != nil {
				return err
			}
			publications, err := session.Advance(trigger.Position, trigger.Position)
			if err != nil {
				return err
			}
			for _, publication := range publications {
				if publication.TriggerMarkerID != trigger.MarkerID || !zonehorde.IsTrigger(publication) {
					continue
				}
				waves, err := planSeededHorde(director, publication, sections, completedStages)
				if err != nil {
					return fmt.Errorf("horde %s: %w", set.Name, err)
				}
				section, point, ok := projectSeededMapPoint(nav, trigger.Position)
				if !ok {
					return fmt.Errorf("horde %s: trigger cannot be mapped", set.Name)
				}
				number++
				horde := seededMapHorde{Number: number, X: point.X, Y: point.Y, Waves: waves}
				for _, wave := range waves {
					horde.Total += wave.Count
				}
				levelMap.Sections[section].Hordes = append(levelMap.Sections[section].Hordes, horde)
			}
		}
	}
	return nil
}

func planSeededHorde(director game.CampaignDirector, publication game.CampaignDirectorPublication, sections []zonepopulation.SectionRoster, completedStages uint32) ([]seededHordeWave, error) {
	session := zonehorde.NewCampaignSession(completedStages)
	policy := session.Policy()
	plans, nextID, err := zonehorde.PlanFirstWaveWithPolicy(director, publication, 1, 1, sections, policy)
	if err != nil {
		return nil, err
	}
	if err = session.AdmitFirstWave(publication, plans); err != nil {
		return nil, err
	}
	var waves []seededHordeWave
	for ordinal := 1; ordinal <= max(2, policy.WaveCount); ordinal++ {
		waves = append(waves, summarizeSeededWave(ordinal, plans))
		var transition zonehorde.Transition
		for _, plan := range plans {
			transition, err = session.Defeat(zonenpc.DamageResult{
				ObjectID: plan.ObjectID, MarkerSetName: plan.MarkerSetName, IsDefeated: true,
			})
			if err != nil {
				return nil, err
			}
			if transition.IsComplete || transition.NextWaveActorCount > 0 {
				break
			}
		}
		if transition.IsComplete {
			return waves, nil
		}
		if transition.NextWaveActorCount == 0 {
			return nil, fmt.Errorf("wave %d produced no completion or followup", ordinal)
		}
		plans, nextID, err = zonehorde.PlanWaveWithPolicy(director, publication, nextID, 1,
			transition.NextWaveActorCount, transition.NextWaveOrdinal, sections, policy)
		if err != nil {
			return nil, err
		}
		if err = session.AdmitNextWave(publication.MarkerSetName, plans); err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("horde did not complete within wave policy")
}
