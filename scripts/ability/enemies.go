package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/darkspinnet/web/scripts/internal/catalog"
)

type npcAbilityCatalog struct {
	Entry map[string]npcAbilityOwner `json:"entry"`
}
type npcAbilityOwner struct {
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	Element  string `json:"element"`
	Category string `json:"category"`
}
type enemyDefinition struct {
	Nodes                  []struct{ Phase *string }
	PassiveAbility         *string
	DeathAbility           *string
	FirstAlertAbility      *string
	FirstAggroAbility      *string
	FirstAggroAbility2     *string
	SubsequentAggroAbility *string
}
type enemyPhase struct{ Gambits []struct{ Ability *string } }

func enemyReferences(def enemyDefinition, phases map[string]enemyPhase) (map[string][]string, error) {
	refs := map[string][]string{}
	missing := []string{}
	add := func(name *string, role string) {
		if name == nil {
			return
		}
		value := strings.TrimSpace(*name)
		if value == "" || strings.EqualFold(value, "none") {
			return
		}
		refs[value] = appendUnique(refs[value], role)
	}
	add(def.PassiveAbility, "passive")
	add(def.DeathAbility, "death")
	add(def.FirstAlertAbility, "first_alert")
	add(def.FirstAggroAbility, "first_aggro")
	add(def.FirstAggroAbility2, "first_aggro")
	add(def.SubsequentAggroAbility, "subsequent_aggro")
	for _, node := range def.Nodes {
		if node.Phase == nil || strings.TrimSpace(*node.Phase) == "" {
			continue
		}
		phase, ok := phases[strings.ToLower(*node.Phase)]
		if !ok {
			missing = appendUnique(missing, *node.Phase)
			continue
		}
		for _, gambit := range phase.Gambits {
			add(gambit.Ability, "action")
		}
	}
	if len(missing) > 0 {
		return refs, fmt.Errorf("unresolved enemy phases: %s", strings.Join(missing, ", "))
	}
	return refs, nil
}

func mergeEnemyAbilities(result *abilityCatalog, owned npcAbilityOwner, references map[string][]string) {
	for name, roles := range references {
		index := -1
		for i := range result.Items {
			if strings.EqualFold(result.Items[i].AssetName, name) {
				index = i
				break
			}
		}
		if index < 0 {
			result.Items = append(result.Items, abilityEntry{AssetName: name, Name: catalog.DisplayName(name), Slug: catalog.Slug(name), PrimarySlot: "enemy_action", Slots: []string{"enemy_action"}, Heroes: []owner{}, Enemies: []owner{}})
			index = len(result.Items) - 1
		}
		entry := &result.Items[index]
		existing := -1
		for i := range entry.Enemies {
			if entry.Enemies[i].Slug == owned.Slug {
				existing = i
				break
			}
		}
		if existing < 0 {
			entry.Enemies = append(entry.Enemies, owner{Name: owned.Name, Slug: owned.Slug, Element: owned.Element, Class: owned.Category, Variants: []string{}, Slots: []string{}})
			existing = len(entry.Enemies) - 1
		}
		for _, role := range roles {
			before := len(entry.Enemies[existing].Slots)
			entry.Enemies[existing].Slots = appendUnique(entry.Enemies[existing].Slots, role)
			if len(entry.Enemies[existing].Slots) > before {
				entry.UsageCount++
			}
		}
		sort.Strings(entry.Enemies[existing].Slots)
	}
}

func addEnemyAbilities(database *sql.DB, npcPath string, result *abilityCatalog) error {
	data, err := os.ReadFile(npcPath)
	if err != nil {
		return err
	}
	var npcs npcAbilityCatalog
	if err = json.Unmarshal(data, &npcs); err != nil {
		return err
	}
	phases := map[string]enemyPhase{}
	rows, err := database.Query(`SELECT asset_name,decoded_asset FROM ai_asset WHERE asset_type=812813543`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var name, encoded string
		if err = rows.Scan(&name, &encoded); err != nil {
			rows.Close()
			return err
		}
		var phase enemyPhase
		if err = json.Unmarshal([]byte(encoded), &phase); err != nil {
			rows.Close()
			return err
		}
		phases[strings.ToLower(name)] = phase
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	rows, err = database.Query(`SELECT n.noun_name,a.decoded_asset FROM non_player_noun n JOIN ai_asset a ON a.content_source_resource_id=n.ai_definition_resource_id ORDER BY n.noun_name COLLATE NOCASE`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var noun, encoded string
		if err = rows.Scan(&noun, &encoded); err != nil {
			return err
		}
		owned, ok := npcs.Entry[catalog.Slug(noun)]
		if !ok {
			continue
		}
		var definition enemyDefinition
		if err = json.Unmarshal([]byte(encoded), &definition); err != nil {
			return err
		}
		references, err := enemyReferences(definition, phases)
		if err != nil {
			result.OwnershipNotes = appendUnique(result.OwnershipNotes, fmt.Sprintf("%s: %v", noun, err))
		}
		mergeEnemyAbilities(result, owned, references)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for i := range result.Items {
		sort.Slice(result.Items[i].Enemies, func(a, b int) bool {
			x, y := result.Items[i].Enemies[a], result.Items[i].Enemies[b]
			if x.Name == y.Name {
				return x.Slug < y.Slug
			}
			return x.Name < y.Name
		})
	}
	sort.Strings(result.OwnershipNotes)
	if len(result.OwnershipNotes) > 0 {
		fmt.Printf("abilities: %d nouns retain references to unavailable AI phases; known abilities preserved\n", len(result.OwnershipNotes))
	}
	return nil
}
