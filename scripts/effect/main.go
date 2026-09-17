package main

import (
	"database/sql"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/darkspinnet/darkspin/content/dbpf"
	"github.com/darkspinnet/web/scripts/internal/catalog"
)

const effectType = uint32(0x92ea4aac)

var effectNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*(?:_[A-Za-z0-9]+)+$`)

type effectEntry struct {
	Name           string   `json:"name"`
	Slug           string   `json:"slug"`
	Category       string   `json:"category"`
	Instance       string   `json:"instance,omitempty"`
	Packaged       bool     `json:"packaged"`
	ResourceCount  int      `json:"resource_count"`
	LuaReferences  int      `json:"lua_references"`
	Consumption    string   `json:"consumption"`
	Verification   string   `json:"verification"`
	Usable         bool     `json:"in_game_usable"`
	RequiredFields []string `json:"required_fields,omitempty"`
	OwnerTypes     []string `json:"owner_types,omitempty"`
	Cleanup        string   `json:"cleanup"`
	Consumer       string   `json:"consumer"`
	Source         string   `json:"source"`
	Build          int      `json:"build"`
	Confidence     string   `json:"confidence"`
	Note           string   `json:"note,omitempty"`
	Tags           []string `json:"tags,omitempty"`
}

type effectCatalog struct {
	Count        int            `json:"count"`
	UsableCount  int            `json:"usable_count"`
	Categories   map[string]int `json:"categories"`
	Consumption  map[string]int `json:"consumption"`
	Verification map[string]int `json:"verification"`
	Items        []effectEntry  `json:"items"`
}

type effectEvidence struct {
	Consumption    string
	Verification   string
	Usable         bool
	RequiredFields []string
	OwnerTypes     []string
	Cleanup        string
	Consumer       string
	Source         string
	Confidence     string
	Note           string
	Tags           []string
}

// This table is intentionally conservative. Entries are added only when a
// build-103 consumer and its packet shape were recovered. Asset/package names
// and Lua string constants never promote an effect into the usable set.
var recoveredEvidence = map[string]effectEvidence{
	"LT_jetpack_effect_lvl1": lightspeedTempestEffect(),
	"LT_jetpack_effect_lvl2": lightspeedTempestEffect(),
	"LT_jetpack_effect_lvl3": lightspeedTempestEffect(),
	"LT_jetpack_effect_lvl4": lightspeedTempestEffect(),
	"LT_jetpack_effect_lvl5": lightspeedTempestEffect(),
	"overdrive_start": {
		Consumption: "client-local", Verification: "confirmed", Usable: false,
		RequiredFields: []string{"local SP_Graphics owner", "graphics slot 20", "active overdrive state"}, OwnerTypes: []string{"local player graphics presentation"},
		Cleanup: "SP_Graphics tears the effect down when overdrive state clears", Consumer: "SP_Graphics sub_508AB0 -> sub_507700",
		Source: "Darkspore.c sub_508AB0 (hard-coded asset)", Confidence: "native call site", Tags: []string{"OVERDRIVE", "GRAPHICS"},
		Note: "Client-local presentation; the server should not send it as a standalone preview.",
	},
	"bounce_effect": {
		Consumption: "object-bound", Verification: "confirmed", Usable: true,
		RequiredFields: []string{"asset", "bouncing object"}, OwnerTypes: []string{"physics projectile / bouncing game object"},
		Cleanup: "one-shot event; no managed cleanup", Consumer: "physics update sub_A30D60 -> nEvent.Notify",
		Source: "Darkspore.c sub_A30D60 (hard-coded asset + object ID)", Confidence: "native call site + event shape", Tags: []string{"PHYSICS", "BOUNCE"},
	},
	"fadeaway_bio": {
		Consumption: "attached", Verification: "confirmed", Usable: true,
		RequiredFields: []string{"managed slot (1..16)", "asset", "object"}, OwnerTypes: []string{"replicated game object", "combatant corpse"},
		Cleanup: "RemoveEffect / RemoveEffectIndex, or object deletion", Consumer: "nGameObject.AddEffect / RemoveEffect",
		Source: "native sub_A0B410 + sub_A01330; Behavior_Death", Confidence: "native + packet shape",
	},
	"life_disease_spore_projectile": {
		Consumption: "attached", Verification: "confirmed", Usable: true,
		RequiredFields: []string{"managed slot (1..16)", "asset", "projectile object"}, OwnerTypes: []string{"Ability_Fireball projectile"},
		Cleanup: "projectile ObjectDelete owns slot teardown", Consumer: "nGameObject.AddEffect",
		Source: "TutorialPoisonCloud bytecode + native AddEffect", Confidence: "bytecode + native packet shape",
	},
	"generic_spawn": {
		Consumption: "sequence-only", Verification: "negative", Usable: false,
		RequiredFields: []string{"spawn modifier owner", "managed slot", "horde_beam_in animation", "0.5s lifecycle"}, OwnerTypes: []string{"horde spawn actor"},
		Cleanup: "SpawnModifier deactivate removes effect and resets animation", Consumer: "SpawnModifier AddEffect / RemoveEffect",
		Source: "Lua chunk 655 (SpawnModifier)", Confidence: "bytecode; isolated live playback invisible",
		Note: "Valid only as part of the authored spawn sequence; do not expose as a standalone preview.",
	},
	"character_beam_in_plasma_electric":              positioned("tutorial squad switch sender"),
	"character_beam_out_plasma_electric":             positioned("tutorial squad switch sender"),
	"character_beam_in_bio":                          positioned("tutorial squad switch sender"),
	"character_beam_out_bio":                         positioned("tutorial squad switch sender"),
	"character_teleport_beam_out":                    positioned("tutorial character beam-out sender"),
	"life_disease_spore_cloud":                       positioned("TutorialPoisonCloud impact/miss bytecode"),
	"plasma_common_electric_hit_small_effect":        positioned("TutorialPlasmaLightning impact bytecode"),
	"plasma_common_electric_hit_medium_effect":       positioned("BurstShot impact bytecode"),
	"ineffective_common_small":                       positioned("shared projectile miss bytecode"),
	"life_common_melee_hit":                          objectHit("TutorialPoisonMelee shared melee template"),
	"charge_impact_small_effect":                     objectHit("TailZap shared melee template"),
	"plasma_common_electric_hit_small_player_effect": objectHit("LightningBasic shared melee template"),
	"life_healer_pet_hit":                            objectHit("SupportHealerPetBasic shared melee template"),
}

func positioned(source string) effectEvidence {
	return effectEvidence{
		Consumption: "positioned", Verification: "confirmed", Usable: true,
		RequiredFields: []string{"asset", "world position", "facing"}, OwnerTypes: []string{"world position (no owner object)"},
		Cleanup: "one-shot event; no managed cleanup", Consumer: "nEvent.Notify fields 6/10/11",
		Source: source, Confidence: "bytecode/native call site + packet shape",
	}
}

func lightspeedTempestEffect() effectEvidence {
	return effectEvidence{
		Consumption: "attached", Verification: "confirmed", Usable: true,
		RequiredFields: []string{"agent object", "managed effect slot", "health percentage"}, OwnerTypes: []string{"Orion / Lightspeed Tempest agent"},
		Cleanup: "RemoveEffectIndex on health-band change and modifier deactivation", Consumer: "nModifier_LightspeedTempest_Passive UpdateHaste",
		Source: "Lua chunk 671, Modifiers/0xFE172B14.lua", Confidence: "bytecode AddEffect + RemoveEffectIndex call sites",
		Tags: []string{"ABILITY", "PASSIVE", "HASTE", "HEALTH-SCALED"},
	}
}

func objectHit(source string) effectEvidence {
	return effectEvidence{
		Consumption: "object-hit", Verification: "confirmed", Usable: true,
		RequiredFields: []string{"asset", "target object", "attacker object", "facing", "critical state (optional)"}, OwnerTypes: []string{"combatant target", "combatant attacker"},
		Cleanup: "one-shot event; no managed cleanup", Consumer: "nEvent.Notify object-hit fields 5/6/7/9/11",
		Source: source, Confidence: "bytecode/native call site + packet shape",
	}
}

func main() {
	gamePath := flag.String("game", catalog.DefaultGamePath, "Darkspore install root")
	databasePath := flag.String("db", catalog.DefaultDatabasePath, "content.db path")
	outputPath := flag.String("out", filepath.Join("data", "generated", "effect.json"), "generated effect catalog")
	flag.Parse()
	database, err := catalog.Open(*databasePath)
	if err != nil {
		fatal(err)
	}
	defer database.Close()
	result, err := build(filepath.Join(*gamePath, "Data", "AssetData_Binary.package"), database)
	if err != nil {
		fatal(err)
	}
	if err = catalog.WriteJSON(*outputPath, result); err != nil {
		fatal(err)
	}
	fmt.Printf("effect: wrote %d packaged, Lua-referenced, and consumer-proven VFX identifiers to %s\n", result.Count, *outputPath)
}

func build(packagePath string, database *sql.DB) (effectCatalog, error) {
	result := effectCatalog{Categories: make(map[string]int), Consumption: make(map[string]int), Verification: make(map[string]int)}
	source, err := os.Open(packagePath)
	if err != nil {
		return result, fmt.Errorf("packageOpen: %w", err)
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return result, fmt.Errorf("packageStat: %w", err)
	}
	reader, err := dbpf.NewReader(source, info.Size())
	if err != nil {
		return result, fmt.Errorf("packageIndex: %w", err)
	}
	byName := make(map[string]effectEntry)
	recoveredByInstance := make(map[uint64]string, len(recoveredEvidence))
	for name := range recoveredEvidence {
		recoveredByInstance[uint64(catalog.HashID(name))] = name
	}
	for _, resource := range reader.Entries {
		if resource.Type != effectType {
			continue
		}
		payloadReader, openErr := reader.Open(resource)
		if openErr != nil {
			return result, fmt.Errorf("resourceOpen[0x%016x]: %w", resource.Instance, openErr)
		}
		payload, readErr := io.ReadAll(payloadReader)
		if readErr != nil {
			return result, fmt.Errorf("resourceRead[0x%016x]: %w", resource.Instance, readErr)
		}
		names := effectNames(payload)
		if recoveredName, found := recoveredByInstance[resource.Instance]; found {
			names = appendUnique(names, recoveredName)
		}
		for _, name := range names {
			entry, found := byName[name]
			if !found {
				category, _, _ := strings.Cut(name, "_")
				category = strings.ToLower(category)
				entry = effectEntry{Name: name, Slug: catalog.Slug(name), Category: category}
			}
			entry.Packaged = true
			entry.ResourceCount++
			if entry.Instance == "" {
				entry.Instance = fmt.Sprintf("0x%016x", resource.Instance)
			}
			byName[name] = entry
		}
	}
	if err = mergeLuaReferences(database, byName); err != nil {
		return result, err
	}
	if err = mergeExactLuaEffectReferences(database, byName); err != nil {
		return result, err
	}
	mergeRecoveredEvidence(byName)
	for _, entry := range byName {
		entry = applyEvidence(entry)
		result.Items = append(result.Items, entry)
		result.Categories[entry.Category]++
		result.Consumption[entry.Consumption]++
		result.Verification[entry.Verification]++
		if entry.Usable {
			result.UsableCount++
		}
	}
	sort.Slice(result.Items, func(i, j int) bool { return result.Items[i].Name < result.Items[j].Name })
	result.Count = len(result.Items)
	return result, nil
}

func mergeRecoveredEvidence(byName map[string]effectEntry) {
	for name := range recoveredEvidence {
		if _, found := byName[name]; found {
			continue
		}
		category, _, _ := strings.Cut(name, "_")
		byName[name] = effectEntry{Name: name, Slug: catalog.Slug(name), Category: strings.ToLower(category)}
	}
}

func applyEvidence(entry effectEntry) effectEntry {
	entry.Build = 103
	entry.Consumption = "unverified"
	entry.Verification = "unverified"
	entry.Cleanup = "unknown"
	entry.Consumer = "none recovered"
	entry.Confidence = "name/hash only"
	if entry.Packaged && entry.LuaReferences > 0 {
		entry.Source = "package resource + Lua string constant"
	} else if entry.Packaged {
		entry.Source = "package resource only"
	} else {
		entry.Source = "Lua string constant only"
	}
	entry.Note = "No recovered build-103 consumer; not safe for server preview."
	if evidence, found := recoveredEvidence[entry.Name]; found {
		entry.Consumption = evidence.Consumption
		entry.Verification = evidence.Verification
		entry.Usable = evidence.Usable
		entry.RequiredFields = evidence.RequiredFields
		entry.OwnerTypes = evidence.OwnerTypes
		entry.Cleanup = evidence.Cleanup
		entry.Consumer = evidence.Consumer
		entry.Source = evidence.Source
		entry.Confidence = evidence.Confidence
		entry.Note = evidence.Note
		entry.Tags = appendUnique(entry.Tags, evidence.Tags...)
	}
	return entry
}

func appendUnique(values []string, candidates ...string) []string {
	seen := make(map[string]bool, len(values)+len(candidates))
	for _, value := range values {
		seen[value] = true
	}
	for _, candidate := range candidates {
		if candidate != "" && !seen[candidate] {
			seen[candidate] = true
			values = append(values, candidate)
		}
	}
	return values
}

func mergeExactLuaEffectReferences(database *sql.DB, byName map[string]effectEntry) error {
	rows, err := database.Query(`
		SELECT lua_string_constant.string_constant, server_data.resource_group,
		       COUNT(DISTINCT lua_string_constant.lua_chunk_id)
		FROM lua_string_constant
		JOIN lua_chunk ON lua_chunk.id=lua_string_constant.lua_chunk_id
		JOIN server_data ON server_data.content_source_resource_id=lua_chunk.server_data_resource_id
		WHERE lower(lua_string_constant.string_constant) LIKE '%.servereventdef'
		GROUP BY lua_string_constant.string_constant, server_data.resource_group`)
	if err != nil {
		return fmt.Errorf("luaExactEffectQuery: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var reference, group string
		var count int
		if err = rows.Scan(&reference, &group, &count); err != nil {
			return fmt.Errorf("luaExactEffectScan: %w", err)
		}
		name := reference[:len(reference)-len(".ServerEventDef")]
		if !effectNamePattern.MatchString(name) {
			continue
		}
		entry, found := byName[name]
		if !found {
			category, _, _ := strings.Cut(name, "_")
			entry = effectEntry{Name: name, Slug: catalog.Slug(name), Category: strings.ToLower(category)}
		}
		entry.LuaReferences += count
		switch strings.ToLower(group) {
		case "abilities":
			entry.Tags = appendUnique(entry.Tags, "ABILITY DATA")
		case "modifiers":
			entry.Tags = appendUnique(entry.Tags, "MODIFIER DATA")
		case "behaviors":
			entry.Tags = appendUnique(entry.Tags, "BEHAVIOR DATA")
		default:
			entry.Tags = appendUnique(entry.Tags, "SCRIPT DATA")
		}
		byName[name] = entry
	}
	return rows.Err()
}

func mergeLuaReferences(database *sql.DB, byName map[string]effectEntry) error {
	rows, err := database.Query(`
		SELECT candidate.string_constant, COUNT(DISTINCT candidate.lua_chunk_id)
		FROM lua_string_constant candidate
		WHERE EXISTS (
			SELECT 1 FROM lua_string_constant marker
			WHERE marker.lua_chunk_id=candidate.lua_chunk_id
			  AND marker.string_constant='ServerEventDefs'
		)
		GROUP BY candidate.string_constant`)
	if err != nil {
		return fmt.Errorf("luaEffectQuery: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		var references int
		if err = rows.Scan(&name, &references); err != nil {
			return fmt.Errorf("luaEffectScan: %w", err)
		}
		if !effectNamePattern.MatchString(name) {
			continue
		}
		entry, found := byName[name]
		if !found {
			category, _, _ := strings.Cut(name, "_")
			category = strings.ToLower(category)
			entry = effectEntry{Name: name, Slug: catalog.Slug(name), Category: category}
		}
		entry.LuaReferences = references
		byName[name] = entry
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("luaEffectRows: %w", err)
	}
	return nil
}

func effectNames(payload []byte) []string {
	seen := make(map[string]bool)
	result := make([]string, 0, 1)
	for _, field := range strings.FieldsFunc(string(payload), func(value rune) bool {
		return !((value >= 'a' && value <= 'z') || (value >= 'A' && value <= 'Z') || (value >= '0' && value <= '9') || value == '_')
	}) {
		if effectNamePattern.MatchString(field) && !seen[field] {
			seen[field] = true
			result = append(result, field)
		}
	}
	return result
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
