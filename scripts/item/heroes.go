package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/darkspinnet/web/scripts/internal/catalog"
)

type compatibilityCatalog struct {
	Families []compatibilityHero `json:"families"`
}
type compatibilityHero struct {
	Name     string                 `json:"name"`
	Slug     string                 `json:"slug"`
	Variants []compatibilityProfile `json:"variants"`
}
type compatibilityProfile struct {
	ID      uint32 `json:"id"`
	Variant string `json:"variant"`
	Element string `json:"element"`
	Class   string `json:"class"`
	Image   string `json:"image"`
	HasHand bool   `json:"is_hand_present"`
	HasFoot bool   `json:"is_foot_present"`
}
type supportedHero struct {
	Name     string   `json:"name"`
	Slug     string   `json:"slug"`
	Image    string   `json:"image"`
	Variants []string `json:"variants"`
}

func loadCompatibilityHeroes(path string) ([]compatibilityHero, error) {
	payload, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var data compatibilityCatalog
	if err = json.Unmarshal(payload, &data); err != nil {
		return nil, err
	}
	if len(data.Families) == 0 {
		return nil, fmt.Errorf("empty hero catalog: %s", path)
	}
	return data.Families, nil
}

func supportedHeroes(record item, heroes []compatibilityHero) []supportedHero {
	result := []supportedHero{}
	for _, hero := range heroes {
		supported := supportedHero{Name: hero.Name, Slug: hero.Slug, Variants: []string{}}
		for _, profile := range hero.Variants {
			// Empty masks do not establish equip compatibility.
			if !tagSetMatches(record.Classes, []string{profile.Class}) || !tagSetMatches(record.Elements, []string{profile.Element}) {
				continue
			}
			if strings.EqualFold(record.Category, "Grasper") && !profile.HasHand {
				continue
			}
			if strings.EqualFold(record.Category, "Foot") && !profile.HasFoot {
				continue
			}
			if len(record.Creatures) > 0 {
				allowed := false
				for _, noun := range record.Creatures {
					if catalog.HashID(noun) == profile.ID {
						allowed = true
						break
					}
				}
				if !allowed {
					continue
				}
			}
			supported.Variants = append(supported.Variants, profile.Variant)
		}
		if len(supported.Variants) > 0 {
			// Hero pages use one canonical portrait for the family.
			if len(hero.Variants) > 0 {
				supported.Image = hero.Variants[len(hero.Variants)-1].Image
			}
			result = append(result, supported)
		}
	}
	return result
}
