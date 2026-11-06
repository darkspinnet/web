package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type npcAbilityText struct {
	Label       string `json:"label"`
	ShortLabel  string `json:"short_label"`
	Description string `json:"description"`
	// Keep accepting older files supplied with -npc-text.
	Name      string `json:"name"`
	ShortName string `json:"short_name"`
}

func applyNPCAbilityText(path string, entries []abilityEntry) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read NPC ability text: %w", err)
	}
	var textByAsset map[string]npcAbilityText
	if err = json.Unmarshal(contents, &textByAsset); err != nil {
		return fmt.Errorf("decode NPC ability text: %w", err)
	}
	byAsset := make(map[string]npcAbilityText, len(textByAsset))
	for asset, text := range textByAsset {
		key := strings.ToLower(asset)
		if _, exists := byAsset[key]; exists {
			return fmt.Errorf("duplicate NPC ability text asset %q", asset)
		}
		byAsset[key] = text
	}
	matched := 0
	for i := range entries {
		entry := &entries[i]
		text, found := byAsset[strings.ToLower(entry.AssetName)]
		if !found || len(entry.Enemies) == 0 {
			continue
		}
		if text.Label != "" {
			entry.Name = text.Label
		} else if text.Name != "" {
			entry.Name = text.Name
		}
		entry.ShortName = text.ShortLabel
		if entry.ShortName == "" {
			entry.ShortName = text.ShortName
		}
		if text.Description != "" {
			entry.Description = text.Description
			entry.DescriptionBasis = ""
			entry.UnresolvedTokens = nil
		}
		matched++
	}
	// The lookup also documents behaviors and helper callbacks without an
	// authored enemy ability reference. Only the recovered catalog owns pages.
	fmt.Printf("abilities: applied curated text to %d of %d NPC lookup entries\n", matched, len(byAsset))
	return nil
}
