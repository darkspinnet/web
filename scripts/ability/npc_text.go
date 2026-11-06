package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type npcAbilityText struct {
	Name        string `json:"name"`
	ShortName   string `json:"short_name"`
	Description string `json:"description"`
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
	for i := range entries {
		entry := &entries[i]
		text, found := textByAsset[entry.AssetName]
		if !found {
			continue
		}
		if len(entry.Enemies) == 0 || len(entry.Heroes) > 0 {
			return fmt.Errorf("NPC ability text %q requires an enemy-only ability", entry.AssetName)
		}
		if text.Name != "" {
			entry.Name = text.Name
		}
		entry.ShortName = text.ShortName
		if text.Description != "" {
			entry.Description = text.Description
			entry.DescriptionBasis = ""
			entry.UnresolvedTokens = nil
		}
		delete(textByAsset, entry.AssetName)
	}
	for asset := range textByAsset {
		return fmt.Errorf("NPC ability text references unknown ability %q", asset)
	}
	return nil
}
