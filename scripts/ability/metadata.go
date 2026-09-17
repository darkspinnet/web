package main

import (
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/darkspinnet/darkspin/content/dbpf"
	"github.com/darkspinnet/darkspin/server/sim"
	"github.com/darkspinnet/web/scripts/internal/catalog"
)

const abilityIconGroup = uint32(0x099dfb88)

type combatRecord struct {
	Kind                  string   `json:"kind,omitempty"`
	CooldownSeconds       float64  `json:"cooldown_seconds,omitempty"`
	RangeMeters           float32  `json:"range_meters,omitempty"`
	PowerCost             float32  `json:"power_cost,omitempty"`
	MinimumDamage         float32  `json:"minimum_damage,omitempty"`
	MaximumDamage         float32  `json:"maximum_damage,omitempty"`
	MinimumDamagePerTick  float32  `json:"minimum_damage_per_tick,omitempty"`
	MaximumDamagePerTick  float32  `json:"maximum_damage_per_tick,omitempty"`
	DamageCoefficient     float32  `json:"damage_coefficient,omitempty"`
	MinimumHealingPerTick float32  `json:"minimum_healing_per_tick,omitempty"`
	MaximumHealingPerTick float32  `json:"maximum_healing_per_tick,omitempty"`
	MinimumFinalHealing   float32  `json:"minimum_final_healing,omitempty"`
	MaximumFinalHealing   float32  `json:"maximum_final_healing,omitempty"`
	HealingCoefficient    float32  `json:"healing_coefficient,omitempty"`
	DurationSeconds       float64  `json:"duration_seconds,omitempty"`
	TickSeconds           float64  `json:"tick_seconds,omitempty"`
	TickCount             uint32   `json:"tick_count,omitempty"`
	RadiusMeters          float32  `json:"radius_meters,omitempty"`
	ProjectileSpeed       float32  `json:"projectile_speed,omitempty"`
	ProjectileDistance    float32  `json:"projectile_distance,omitempty"`
	RootChance            float32  `json:"root_chance,omitempty"`
	Animation             string   `json:"animation,omitempty"`
	Effects               []string `json:"effects,omitempty"`
	IsNoGlobalCooldown    bool     `json:"is_no_global_cooldown,omitempty"`
}

type sourceRecord struct {
	LuaChunkID int64  `json:"lua_chunk_id"`
	LuaSource  string `json:"lua_source"`
	SHA256     string `json:"sha256"`
	Rank       int    `json:"rank"`
}

type localePair struct {
	Name        string
	Description string
}

type luaConstant struct {
	ChunkID int64
	Source  string
	SHA256  string
	Ordinal int
	Text    string
}

type decodedChunk struct {
	ID       int64
	Source   string
	SHA256   string
	Bytecode []byte
}

type compiledAbility struct {
	Definition sim.AbilityDefinition
	Chunk      decodedChunk
}

func enrichAbilities(database *sql.DB, gamePath string, entries []abilityEntry) error {
	localeByText, err := loadLocalePairs(database)
	if err != nil {
		return fmt.Errorf("localeLoad: %w", err)
	}
	iconInstance, err := loadAbilityIconInstances(filepath.Join(gamePath, "Data", "UI.package"))
	if err != nil {
		return fmt.Errorf("iconLoad: %w", err)
	}
	localizedCount, combatCount, iconCount := 0, 0, 0
	for index := range entries {
		entry := &entries[index]
		constants, constantErr := loadAbilityConstants(database, entry.AssetName)
		if constantErr != nil {
			return fmt.Errorf("constantLoad[%s]: %w", entry.AssetName, constantErr)
		}
		if localized := chooseLocalePair(constants, entry.AssetName, entry.Heroes, localeByText); localized != nil {
			entry.Name = localized.Name
			entry.Description = cleanLocalizedText(localized.Description)
			localizedCount++
		}
		compiled, compileErr := compileAbility(database, entry.AssetName)
		if compileErr == nil {
			entry.Combat = projectCombat(compiled.Definition)
			entry.Source = &sourceRecord{LuaChunkID: compiled.Chunk.ID, LuaSource: compiled.Chunk.Source, SHA256: compiled.Chunk.SHA256, Rank: 1}
			combatCount++
		}
		iconReference := ""
		if compiled != nil && iconInstance[catalog.HashID(compiled.Definition.Namespace)] {
			iconReference = compiled.Definition.Namespace
		}
		if iconReference == "" {
			iconReference = chooseIconReference(constants, entry.AssetName, iconInstance)
		}
		if iconReference != "" {
			entry.IconReference = iconReference
			entry.IconPath = "/image/ability/" + entry.Slug + ".webp"
			iconCount++
		}
		entry.Description = resolveDescription(entry.Description, entry.Combat, entry.Heroes)
	}
	fmt.Printf("abilities: enriched %d localized descriptions, %d combat records, and %d icon references\n", localizedCount, combatCount, iconCount)
	return nil
}

func loadLocalePairs(database *sql.DB) (map[string][]localePair, error) {
	rows, err := database.Query(`
		SELECT table_id, locale_key, localized_text FROM localization_text
		WHERE locale='en-us' ORDER BY table_id, locale_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	table := make(map[uint64]map[uint64]string)
	for rows.Next() {
		var tableID uint64
		var localeKey, text string
		if err = rows.Scan(&tableID, &localeKey, &text); err != nil {
			return nil, err
		}
		key, parseErr := strconv.ParseUint(strings.TrimPrefix(strings.ToLower(localeKey), "0x"), 16, 64)
		if parseErr != nil {
			continue
		}
		if table[tableID] == nil {
			table[tableID] = make(map[uint64]string)
		}
		table[tableID][key] = text
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	pairByText := make(map[string][]localePair)
	for _, record := range table {
		for key, name := range record {
			description := record[key+1]
			if len(strings.TrimSpace(name)) > 64 || len(strings.TrimSpace(description)) < 20 {
				continue
			}
			identity := strings.ToLower(strings.TrimSpace(stripPrivateUse(name)))
			pairByText[identity] = append(pairByText[identity], localePair{Name: stripPrivateUse(name), Description: description})
		}
	}
	return pairByText, nil
}

func loadAbilityIconInstances(packagePath string) (map[uint32]bool, error) {
	r, err := os.Open(packagePath)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	fi, err := r.Stat()
	if err != nil {
		return nil, err
	}
	pkg, err := dbpf.NewReader(r, fi.Size())
	if err != nil {
		return nil, err
	}
	instances := make(map[uint32]bool)
	for _, entry := range pkg.Entries {
		if entry.Type != 0x2f7d0004 || entry.Group != abilityIconGroup || entry.Instance > uint64(math.MaxUint32) {
			continue
		}
		instances[uint32(entry.Instance)] = true
	}
	return instances, nil
}

func loadAbilityConstants(database *sql.DB, assetName string) ([]luaConstant, error) {
	rows, err := database.Query(`
		SELECT lua_chunk.id, lua_chunk.source_name, lua_chunk.bytecode_sha256,
		       lua_string_constant.ordinal, lua_string_constant.string_constant
		FROM lua_string_constant
		JOIN lua_chunk ON lua_chunk.id=lua_string_constant.lua_chunk_id
		WHERE lua_chunk.id IN (
			SELECT lua_chunk_id FROM lua_string_constant WHERE string_constant=? COLLATE NOCASE
		)
		ORDER BY lua_chunk.id, lua_string_constant.ordinal`, assetName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	constants := make([]luaConstant, 0)
	for rows.Next() {
		var constant luaConstant
		if err = rows.Scan(&constant.ChunkID, &constant.Source, &constant.SHA256, &constant.Ordinal, &constant.Text); err != nil {
			return nil, err
		}
		constants = append(constants, constant)
	}
	return constants, rows.Err()
}

func chooseLocalePair(constants []luaConstant, assetName string, owners []owner, pairByText map[string][]localePair) *localePair {
	displayIdentity := strings.ToLower(catalog.DisplayName(assetName))
	if pairs := pairByText[displayIdentity]; len(pairs) > 0 {
		selected := pairs[0]
		return &selected
	}
	if strings.HasPrefix(assetName, "Cast") {
		castIdentity := strings.ToLower(catalog.DisplayName(strings.TrimPrefix(assetName, "Cast")))
		if pairs := pairByText[castIdentity]; len(pairs) > 0 {
			selected := pairs[0]
			return &selected
		}
	}
	_ = constants
	_ = owners
	return nil
}

func chooseIconReference(constants []luaConstant, assetName string, iconInstance map[uint32]bool) string {
	bestScore := math.MaxInt
	best := ""
	for _, asset := range constants {
		if !strings.EqualFold(asset.Text, assetName) {
			continue
		}
		for _, candidate := range constants {
			if candidate.ChunkID != asset.ChunkID || !iconInstance[catalog.HashID(candidate.Text)] {
				continue
			}
			score := abs(candidate.Ordinal - asset.Ordinal)
			if strings.HasPrefix(strings.ToLower(candidate.Text), "abilities_") {
				score -= 1000
			}
			if score < bestScore {
				bestScore, best = score, candidate.Text
			}
		}
	}
	return best
}

func compileAbility(database *sql.DB, assetName string) (*compiledAbility, error) {
	rows, err := database.Query(`
		SELECT DISTINCT lua_chunk.id FROM lua_string_constant
		JOIN lua_chunk ON lua_chunk.id=lua_string_constant.lua_chunk_id
		WHERE lua_string_constant.string_constant=? COLLATE NOCASE ORDER BY lua_chunk.id`, assetName)
	if err != nil {
		return nil, err
	}
	chunkIDs := make([]int64, 0)
	for rows.Next() {
		var chunkID int64
		if err = rows.Scan(&chunkID); err != nil {
			_ = rows.Close()
			return nil, err
		}
		chunkIDs = append(chunkIDs, chunkID)
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	var result *compiledAbility
	for _, chunkID := range chunkIDs {
		root, loadErr := loadDecodedChunk(database, chunkID)
		if loadErr != nil {
			continue
		}
		module, moduleErr := loadLuaModules(database, chunkID)
		if moduleErr != nil {
			continue
		}
		definition, compileErr := sim.CompileLuaAbilityDefinition(sim.LuaAbilityInput{
			Root:    sim.LuaBytecode{ChunkID: root.ID, SHA256: root.SHA256, Contents: root.Bytecode},
			Modules: module, Role: "catalogHero",
		})
		if compileErr != nil || !strings.EqualFold(definition.Name, assetName) {
			continue
		}
		if result != nil {
			return nil, errors.New("multiple matching registrations")
		}
		result = &compiledAbility{Definition: definition, Chunk: root}
	}
	if result == nil {
		return nil, errors.New("registration unavailable")
	}
	return result, nil
}

func loadDecodedChunk(database *sql.DB, chunkID int64) (decodedChunk, error) {
	var chunk decodedChunk
	var size int
	var compressed []byte
	err := database.QueryRow(`
		SELECT lua_chunk.id, lua_chunk.source_name, lua_chunk.bytecode_sha256,
		       lua_chunk.bytecode_size, server_data.decoded_payload
		FROM lua_chunk JOIN server_data ON server_data.content_source_resource_id=lua_chunk.server_data_resource_id
		WHERE lua_chunk.id=?`, chunkID).Scan(&chunk.ID, &chunk.Source, &chunk.SHA256, &size, &compressed)
	if err != nil {
		return chunk, err
	}
	chunk.Bytecode, err = decompress(compressed)
	if err != nil {
		return chunk, err
	}
	if len(chunk.Bytecode) != size || fmt.Sprintf("%x", sha256.Sum256(chunk.Bytecode)) != chunk.SHA256 {
		return chunk, errors.New("bytecode verification failed")
	}
	return chunk, nil
}

func loadLuaModules(database *sql.DB, rootID int64) (map[string]sim.LuaBytecode, error) {
	rows, err := database.Query(`
		WITH RECURSIVE module_dependency(dependency_name, target_lua_chunk_id, path) AS (
			SELECT dependency_name, target_lua_chunk_id, ',' || CAST(target_lua_chunk_id AS TEXT) || ','
			FROM lua_dependency WHERE lua_chunk_id=? AND target_lua_chunk_id IS NOT NULL
			UNION ALL
			SELECT dependency.dependency_name, dependency.target_lua_chunk_id,
			       module_dependency.path || CAST(dependency.target_lua_chunk_id AS TEXT) || ','
			FROM module_dependency JOIN lua_dependency AS dependency
			  ON dependency.lua_chunk_id=module_dependency.target_lua_chunk_id
			WHERE dependency.target_lua_chunk_id IS NOT NULL
			  AND INSTR(module_dependency.path, ',' || CAST(dependency.target_lua_chunk_id AS TEXT) || ',')=0
		)
		SELECT DISTINCT dependency_name, target_lua_chunk_id FROM module_dependency ORDER BY dependency_name`, rootID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	modules := make(map[string]sim.LuaBytecode)
	for rows.Next() {
		var name string
		var chunkID int64
		if err = rows.Scan(&name, &chunkID); err != nil {
			return nil, err
		}
		chunk, loadErr := loadDecodedChunk(database, chunkID)
		if loadErr != nil {
			return nil, loadErr
		}
		modules[name] = sim.LuaBytecode{ChunkID: chunk.ID, SHA256: chunk.SHA256, Contents: chunk.Bytecode}
	}
	return modules, rows.Err()
}

func projectCombat(definition sim.AbilityDefinition) *combatRecord {
	record := &combatRecord{
		Kind: string(definition.Kind), CooldownSeconds: seconds(definition.Cooldown), RangeMeters: definition.Range,
		PowerCost: definition.ManaCost, MinimumDamage: definition.MinimumDamage, MaximumDamage: definition.MaximumDamage,
		MinimumDamagePerTick: definition.MinimumDamagePerTick, MaximumDamagePerTick: definition.MaximumDamagePerTick,
		DamageCoefficient: definition.DamageCoefficient, MinimumHealingPerTick: definition.MinimumHealingPerTick,
		MaximumHealingPerTick: definition.MaximumHealingPerTick, MinimumFinalHealing: definition.MinimumFinalHealing,
		MaximumFinalHealing: definition.MaximumFinalHealing, HealingCoefficient: definition.HealingCoefficient,
		DurationSeconds: seconds(definition.Duration), TickSeconds: seconds(definition.TickDuration), TickCount: definition.NumberOfTicks,
		RadiusMeters: definition.Radius, ProjectileSpeed: definition.Speed, ProjectileDistance: definition.Distance,
		RootChance: definition.RootChance, Animation: definition.AnimationName, IsNoGlobalCooldown: definition.IsNoGlobalCooldown,
	}
	record.Effects = uniqueStrings([]string{definition.HitEffectName, definition.TrailEffectName, definition.ImpactEffectName,
		definition.MissEffectName, definition.MuzzleEffectName, definition.ActivationEffectName, definition.AbsorbEffectName,
		definition.HealEffectName})
	return record
}

func cleanLocalizedText(text string) string {
	text = strings.ReplaceAll(text, "~br~", "\n")
	return strings.TrimSpace(stripPrivateUse(text))
}

func resolveDescription(description string, combat *combatRecord, owners []owner) string {
	if description == "" {
		return ""
	}
	if len(owners) == 1 {
		description = strings.ReplaceAll(description, "~CHAR_NAME~", owners[0].Name)
	}
	if combat == nil {
		return description
	}
	damage := interval(combat.MinimumDamage, combat.MaximumDamage)
	healing := interval(combat.MinimumHealingPerTick, combat.MaximumHealingPerTick)
	replacement := map[string]string{
		"~cooldown~":   formatNumber(float32(combat.CooldownSeconds)),
		"~manacost~":   formatNumber(combat.PowerCost),
		"~range~":      formatNumber(combat.RangeMeters),
		"~radius~":     formatNumber(combat.RadiusMeters),
		"~duration~":   formatNumber(float32(combat.DurationSeconds)),
		"~minDamage~":  formatNumber(combat.MinimumDamage),
		"~maxDamage~":  formatNumber(combat.MaximumDamage),
		"~damage~":     damage,
		"~minHealing~": formatNumber(combat.MinimumFinalHealing),
		"~maxHealing~": formatNumber(combat.MaximumFinalHealing),
		"~healing~":    healing,
		"~numticks~":   fmt.Sprint(combat.TickCount),
	}
	for token, amount := range replacement {
		if amount != "" && amount != "0" {
			description = strings.ReplaceAll(description, token, amount)
		}
	}
	return description
}

func interval(minimum, maximum float32) string {
	if minimum == 0 && maximum == 0 {
		return ""
	}
	if minimum == maximum || maximum == 0 {
		return formatNumber(minimum)
	}
	return formatNumber(minimum) + "–" + formatNumber(maximum)
}

func formatNumber(number float32) string {
	if number == 0 {
		return ""
	}
	return strconv.FormatFloat(float64(number), 'f', -1, 32)
}

func stripPrivateUse(text string) string {
	return strings.Map(func(character rune) rune {
		if character >= 0xe000 && character <= 0xf8ff {
			return -1
		}
		return character
	}, text)
}

func decompress(compressed []byte) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return nil, err
	}
	contents, err := io.ReadAll(r)
	if err != nil {
		_ = r.Close()
		return nil, err
	}
	if err = r.Close(); err != nil {
		return nil, err
	}
	return contents, nil
}

func uniqueStrings(items []string) []string {
	seen := make(map[string]bool)
	result := make([]string, 0)
	for _, item := range items {
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		result = append(result, item)
	}
	sort.Strings(result)
	return result
}

func seconds(duration time.Duration) float64 { return float64(duration) / float64(time.Second) }
func abs(number int) int {
	if number < 0 {
		return -number
	}
	return number
}
