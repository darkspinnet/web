package main

import (
	"database/sql"
	"encoding/binary"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/darkspinnet/darkspin/content/dbpf"
	"github.com/darkspinnet/web/scripts/internal/catalog"
)

const (
	rigblockType = uint32(0x1bced3d7)
	prefixType   = uint32(0x6a1812c6)
	suffixType   = uint32(0x447dc2e5)
)

type itemCatalog struct {
	Count          int                   `json:"count"`
	RigblockCount  int                   `json:"rigblock_count"`
	LocalizedCount int                   `json:"localized_count"`
	ComponentCount int                   `json:"component_count"`
	PrefixCount    int                   `json:"prefix_count"`
	SuffixCount    int                   `json:"suffix_count"`
	Status         string                `json:"status"`
	Categories     map[string]int        `json:"categories"`
	LootRules      lootRules             `json:"loot_rules"`
	Items          []itemFamily          `json:"items"`
	Prefixes       []affix               `json:"prefixes"`
	Suffixes       []affix               `json:"suffixes"`
	Entry          map[string]itemFamily `json:"entry"`
}

type item struct {
	ID             uint32   `json:"id"`
	InstanceID     uint32   `json:"instance_id"`
	Name           string   `json:"name"`
	NameLocaleKey  string   `json:"name_locale_key"`
	Category       string   `json:"category"`
	AssetReference string   `json:"asset_reference,omitempty"`
	IconReference  string   `json:"icon_reference,omitempty"`
	Palette        string   `json:"palette,omitempty"`
	Classes        []string `json:"classes"`
	Elements       []string `json:"elements"`
	Creatures      []string `json:"creatures"`
}

type itemFamily struct {
	Name           string        `json:"name"`
	Slug           string        `json:"slug"`
	Category       string        `json:"category"`
	RecordCount    int           `json:"record_count"`
	VariantCount   int           `json:"variant_count"`
	Classes        []string      `json:"classes"`
	Elements       []string      `json:"elements"`
	Creatures      []string      `json:"creatures"`
	Variants       []itemVariant `json:"variants"`
	PrefixCount    int           `json:"compatible_prefix_count"`
	SuffixCount    int           `json:"compatible_suffix_count"`
	PrefixOptions  []affixRef    `json:"prefix_options"`
	SuffixOptions  []affixRef    `json:"suffix_options"`
	SuffixExamples []affixRef    `json:"suffix_examples"`
}

type itemVariant struct {
	Name           string              `json:"name"`
	Style          string              `json:"style"`
	AssetReference string              `json:"asset_reference"`
	IconReference  string              `json:"icon_reference,omitempty"`
	IconPath       string              `json:"icon_path,omitempty"`
	Palette        string              `json:"palette,omitempty"`
	RecordIDs      []uint32            `json:"record_ids"`
	Classes        []string            `json:"classes"`
	Elements       []string            `json:"elements"`
	Records        []itemVariantRecord `json:"records"`
}

type itemVariantRecord struct {
	ID       uint32   `json:"id"`
	Elements []string `json:"elements"`
}

type affixRef struct {
	ID   uint32 `json:"id"`
	Name string `json:"name"`
}

type affix struct {
	ID            uint32      `json:"id"`
	InstanceID    uint32      `json:"instance_id"`
	Name          string      `json:"name"`
	NameLocaleKey string      `json:"name_locale_key,omitempty"`
	Classes       []string    `json:"classes"`
	Elements      []string    `json:"elements"`
	Slots         []string    `json:"slots"`
	Stats         []affixStat `json:"stats"`
}

type affixStat struct {
	Index         int     `json:"index"`
	Name          string  `json:"name"`
	AuthoredValue float64 `json:"authored_value"`
	Preview       string  `json:"preview,omitempty"`
	PreviewExact  bool    `json:"preview_exact"`
}

type lootRules struct {
	Source                        string `json:"source"`
	GoldMultiplier                int    `json:"gold_multiplier"`
	SilverMultiplier              int    `json:"silver_multiplier"`
	BronzeMultiplier              int    `json:"bronze_multiplier"`
	NoMedalMultiplier             int    `json:"no_medal_multiplier"`
	GoldValue                     int    `json:"gold_value"`
	SilverValue                   int    `json:"silver_value"`
	BronzeValue                   int    `json:"bronze_value"`
	NoMedalValue                  int    `json:"no_medal_value"`
	ChainBonusPerAdditionalPlanet int    `json:"chain_bonus_per_additional_planet"`
	EpicChance                    []int  `json:"epic_chance"`
}

type decodedResource struct {
	InstanceID uint32
	Payload    []byte
}

func main() {
	databasePath := flag.String("db", catalog.DefaultDatabasePath, "content.db path")
	outputPath := flag.String("out", filepath.Join("data", "generated", "item.json"), "generated JSON path")
	lookupPath := flag.String("lookup", filepath.Join("static", "data", "item", "index.json"), "compact lookup path")
	contentPath := flag.String("content", filepath.Join("content", "item"), "generated item content directory")
	flag.Parse()

	database, err := catalog.Open(*databasePath)
	if err != nil {
		fatal(err)
	}
	defer database.Close()

	result, err := loadItems(database)
	if err != nil {
		fatal(err)
	}
	if err = catalog.WriteJSON(*outputPath, result); err != nil {
		fatal(err)
	}
	if err = writeLookup(*lookupPath, result.Items); err != nil {
		fatal(err)
	}
	if err = writeContent(*contentPath, result.Items); err != nil {
		fatal(err)
	}
	fmt.Printf("items: wrote %d families from %d rigblocks, %d prefixes, and %d suffixes to %s\n", result.Count, result.RigblockCount, result.PrefixCount, result.SuffixCount, *outputPath)
}

func loadItems(database *sql.DB) (itemCatalog, error) {
	rigblockNames, err := loadLocalization(database, catalog.HashID("LootRigblockNames"))
	if err != nil {
		return itemCatalog{}, fmt.Errorf("rigblockNamesLoad: %w", err)
	}
	prefixNames, err := loadLocalization(database, catalog.HashID("LootPrefixNames"))
	if err != nil {
		return itemCatalog{}, fmt.Errorf("prefixNamesLoad: %w", err)
	}
	suffixNames, err := loadLocalization(database, catalog.HashID("LootSuffixNames"))
	if err != nil {
		return itemCatalog{}, fmt.Errorf("suffixNamesLoad: %w", err)
	}
	rigblocks, err := loadResources(database, rigblockType)
	if err != nil {
		return itemCatalog{}, fmt.Errorf("rigblocksLoad: %w", err)
	}
	prefixes, err := loadResources(database, prefixType)
	if err != nil {
		return itemCatalog{}, fmt.Errorf("prefixesLoad: %w", err)
	}
	suffixes, err := loadResources(database, suffixType)
	if err != nil {
		return itemCatalog{}, fmt.Errorf("suffixesLoad: %w", err)
	}
	result := itemCatalog{
		Status:     "Decoded from AssetData_Binary.package",
		Categories: make(map[string]int),
		Items:      make([]itemFamily, 0, 400),
		Prefixes:   make([]affix, 0, len(prefixes)),
		Suffixes:   make([]affix, 0, len(suffixes)),
		Entry:      make(map[string]itemFamily, 400),
		LootRules: lootRules{
			Source: "lua/0x30DDBF3C.lua (nLootRules)", GoldMultiplier: 6, SilverMultiplier: 3,
			BronzeMultiplier: 1, NoMedalMultiplier: 0, GoldValue: 4, SilverValue: 3,
			BronzeValue: 2, NoMedalValue: 1, ChainBonusPerAdditionalPlanet: 4,
			EpicChance: []int{0, 0, 2, 4, 6, 8, 10, 12, 14, 16, 18, 20, 21, 22, 24, 26, 28, 30},
		},
	}
	decodedItems := make([]item, 0, len(rigblocks))
	for _, resource := range rigblocks {
		entry := decodeItem(resource, rigblockNames)
		decodedItems = append(decodedItems, entry)
		if entry.NameLocaleKey != "" {
			result.LocalizedCount++
		}
	}
	for _, resource := range prefixes {
		result.Prefixes = append(result.Prefixes, decodeAffix(resource, prefixNames, "Prefix"))
	}
	for _, resource := range suffixes {
		result.Suffixes = append(result.Suffixes, decodeAffix(resource, suffixNames, "Suffix"))
	}
	result.Items = groupItems(decodedItems)
	for index := range result.Items {
		enrichVariations(&result.Items[index], result.Prefixes, result.Suffixes)
		result.Entry[result.Items[index].Slug] = result.Items[index]
		result.Categories[result.Items[index].Category]++
	}
	sort.Slice(result.Items, func(i, j int) bool {
		if result.Items[i].Name == result.Items[j].Name {
			return result.Items[i].Category < result.Items[j].Category
		}
		return result.Items[i].Name < result.Items[j].Name
	})
	sort.Slice(result.Prefixes, func(i, j int) bool { return result.Prefixes[i].ID < result.Prefixes[j].ID })
	sort.Slice(result.Suffixes, func(i, j int) bool { return result.Suffixes[i].ID < result.Suffixes[j].ID })
	result.Count = len(result.Items)
	result.RigblockCount = len(decodedItems)
	result.PrefixCount = len(result.Prefixes)
	result.SuffixCount = len(result.Suffixes)
	result.ComponentCount = result.RigblockCount + result.PrefixCount + result.SuffixCount
	return result, nil
}

func enrichVariations(entry *itemFamily, prefixes, suffixes []affix) {
	for _, candidate := range prefixes {
		if affixApplies(entry.Category, entry.Classes, entry.Elements, candidate) {
			entry.PrefixCount++
			entry.PrefixOptions = append(entry.PrefixOptions, affixRef{ID: candidate.ID, Name: candidate.Name})
		}
	}
	for _, candidate := range suffixes {
		if !affixApplies(entry.Category, entry.Classes, entry.Elements, candidate) {
			continue
		}
		entry.SuffixCount++
		entry.SuffixOptions = append(entry.SuffixOptions, affixRef{ID: candidate.ID, Name: candidate.Name})
		if candidate.NameLocaleKey != "" && len(entry.SuffixExamples) < 8 {
			entry.SuffixExamples = append(entry.SuffixExamples, affixRef{ID: candidate.ID, Name: candidate.Name})
		}
	}
}

func affixApplies(category string, classes, elements []string, candidate affix) bool {
	return tagSetMatches(classes, candidate.Classes) &&
		tagSetMatches(elements, candidate.Elements) &&
		tagSetMatches([]string{category}, candidate.Slots)
}

func tagSetMatches(itemTags, affixTags []string) bool {
	if len(affixTags) == 0 {
		return true
	}
	for _, itemTag := range itemTags {
		for _, affixTag := range affixTags {
			if strings.EqualFold(itemTag, affixTag) {
				return true
			}
		}
	}
	return false
}

func groupItems(items []item) []itemFamily {
	grouped := make(map[string][]item)
	for _, entry := range items {
		if entry.NameLocaleKey == "" {
			continue
		}
		name := baseFamilyName(entry.Name)
		key := strings.ToLower(entry.Category + "\x00" + name)
		grouped[key] = append(grouped[key], entry)
	}
	keys := make([]string, 0, len(grouped))
	for key := range grouped {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	families := make([]itemFamily, 0, len(keys))
	for _, key := range keys {
		families = append(families, buildFamily(baseFamilyName(grouped[key][0].Name), grouped[key]))
	}
	return families
}

func baseFamilyName(name string) string {
	fields := strings.Fields(name)
	if len(fields) == 0 {
		return name
	}
	if len(fields) > 2 {
		return name
	}
	return fields[len(fields)-1]
}

func buildFamily(name string, records []item) itemFamily {
	family := itemFamily{
		Name: name, Category: records[0].Category,
		Slug:        catalog.Slug(name + "-" + records[0].Category),
		RecordCount: len(records),
	}
	variantMap := make(map[string]*itemVariant)
	for _, record := range records {
		family.Classes = appendUnique(family.Classes, record.Classes...)
		family.Elements = appendUnique(family.Elements, record.Elements...)
		family.Creatures = appendUnique(family.Creatures, record.Creatures...)
		key := strings.ToLower(record.AssetReference)
		if key == "" {
			key = fmt.Sprintf("record:%d", record.ID)
		}
		variant := variantMap[key]
		if variant == nil {
			style := strings.TrimSpace(strings.TrimSuffix(record.Name, name))
			if style == "" {
				style = record.Name
			}
			variant = &itemVariant{
				Name: record.Name, Style: style, AssetReference: record.AssetReference,
				IconReference: record.IconReference, IconPath: itemIconPath(record.IconReference), Palette: record.Palette,
			}
			variantMap[key] = variant
		}
		variant.RecordIDs = append(variant.RecordIDs, record.ID)
		variant.Records = append(variant.Records, itemVariantRecord{ID: record.ID, Elements: append([]string(nil), record.Elements...)})
		variant.Classes = appendUnique(variant.Classes, record.Classes...)
		variant.Elements = appendUnique(variant.Elements, record.Elements...)
	}
	for _, variant := range variantMap {
		sort.Slice(variant.RecordIDs, func(i, j int) bool { return variant.RecordIDs[i] < variant.RecordIDs[j] })
		variant.Records = dedupeVariantRecords(variant.Records)
		sort.Strings(variant.Classes)
		sort.Strings(variant.Elements)
		family.Variants = append(family.Variants, *variant)
	}
	sort.Slice(family.Variants, func(i, j int) bool {
		if family.Variants[i].Name == family.Variants[j].Name {
			return family.Variants[i].AssetReference < family.Variants[j].AssetReference
		}
		return family.Variants[i].Name < family.Variants[j].Name
	})
	sort.Strings(family.Classes)
	sort.Strings(family.Elements)
	sort.Strings(family.Creatures)
	family.VariantCount = len(family.Variants)
	return family
}

func dedupeVariantRecords(records []itemVariantRecord) []itemVariantRecord {
	byElement := make(map[string]itemVariantRecord)
	for _, record := range records {
		elements := append([]string(nil), record.Elements...)
		sort.Strings(elements)
		key := strings.Join(elements, "\x00")
		existing, found := byElement[key]
		if !found || record.ID < existing.ID {
			record.Elements = elements
			byElement[key] = record
		}
	}
	result := make([]itemVariantRecord, 0, len(byElement))
	for _, record := range byElement {
		result = append(result, record)
	}
	sort.Slice(result, func(i, j int) bool {
		left := strings.Join(result[i].Elements, "/")
		right := strings.Join(result[j].Elements, "/")
		if left == right {
			return result[i].ID < result[j].ID
		}
		return left < right
	})
	return result
}

func itemIconPath(reference string) string {
	parts := strings.SplitN(reference, "!", 2)
	if len(parts) != 2 {
		return ""
	}
	name := strings.TrimSuffix(parts[1], filepath.Ext(parts[1]))
	if name == "" {
		return ""
	}
	return "/image/item/" + catalog.Slug(name) + ".webp"
}

func appendUnique(values []string, additions ...string) []string {
	for _, addition := range additions {
		found := false
		for _, value := range values {
			if strings.EqualFold(value, addition) {
				found = true
				break
			}
		}
		if !found {
			values = append(values, addition)
		}
	}
	return values
}

func loadResources(database *sql.DB, typeID uint32) ([]decodedResource, error) {
	rows, err := database.Query(`
		SELECT instance_id, stored_size, decoded_size, compression, raw_payload
		FROM content_source_resource
		WHERE content_source_package_id=(SELECT id FROM content_source_package WHERE package_name='AssetData_Binary.package')
		  AND type_id=? ORDER BY ordinal`, typeID)
	if err != nil {
		return nil, fmt.Errorf("resourceQuery: %w", err)
	}
	defer rows.Close()
	resources := make([]decodedResource, 0)
	for rows.Next() {
		var instanceID, storedSize, decodedSize uint32
		var compression uint16
		var raw []byte
		if err = rows.Scan(&instanceID, &storedSize, &decodedSize, &compression, &raw); err != nil {
			return nil, fmt.Errorf("resourceScan: %w", err)
		}
		payload, decodeErr := dbpf.Decode(dbpf.Entry{StoredSize: storedSize, Size: decodedSize, Compression: compression}, raw)
		if decodeErr != nil {
			return nil, fmt.Errorf("resourceDecode[%08x]: %w", instanceID, decodeErr)
		}
		resources = append(resources, decodedResource{InstanceID: instanceID, Payload: payload})
	}
	return resources, rows.Err()
}

func loadLocalization(database *sql.DB, tableID uint32) (map[uint32]string, error) {
	rows, err := database.Query(`
		SELECT locale_key, localized_text FROM localization_text
		WHERE locale='en-us' AND table_id=?`, tableID)
	if err != nil {
		return nil, fmt.Errorf("localeQuery: %w", err)
	}
	defer rows.Close()
	result := make(map[uint32]string)
	for rows.Next() {
		var key, text string
		if err = rows.Scan(&key, &text); err != nil {
			return nil, fmt.Errorf("localeScan: %w", err)
		}
		var id uint32
		if _, scanErr := fmt.Sscanf(key, "0x%x", &id); scanErr == nil {
			result[id] = text
		}
	}
	return result, rows.Err()
}

func decodeItem(resource decodedResource, names map[uint32]string) item {
	id := payloadID(resource.Payload)
	name := strings.TrimSpace(names[id])
	nameLocaleKey := ""
	if name == "" {
		name = fmt.Sprintf("Rigblock %d", id)
	} else {
		nameLocaleKey = fmt.Sprintf("LootRigblockNames!0x%08x", id)
	}
	values := printableStrings(resource.Payload, 3)
	entry := item{
		ID: id, InstanceID: resource.InstanceID, Name: name,
		NameLocaleKey: nameLocaleKey,
		Category:      firstTag(values, []string{"Defense", "Offense", "Utility", "Weapon", "Grasper", "Foot"}, "Other"),
		Classes:       collectTags(values, []string{"tempest", "sentinel", "ravager"}),
		Elements:      collectTags(values, []string{"bio", "cyber", "plasma", "chrono", "necro"}),
	}
	for _, value := range values {
		switch {
		case strings.HasPrefix(value, "@creature_rigblock!"):
			entry.AssetReference = value
		case strings.HasSuffix(strings.ToLower(value), ".png"):
			entry.IconReference = value
		case strings.HasPrefix(value, "@palette_config!"):
			entry.Palette = value
		case strings.HasSuffix(value, ".Noun") && strings.HasPrefix(value, "PC_"):
			entry.Creatures = append(entry.Creatures, value)
		}
	}
	return entry
}

func decodeAffix(resource decodedResource, names map[uint32]string, kind string) affix {
	id := payloadID(resource.Payload)
	name := strings.TrimSpace(names[id])
	if name == "" {
		if kind == "Prefix" {
			name = decodePrefixName(resource.Payload)
		}
		if name == "" {
			name = fmt.Sprintf("%s %d", kind, id)
		}
	}
	values := printableStrings(resource.Payload, 3)
	return affix{
		ID: id, InstanceID: resource.InstanceID, Name: name,
		NameLocaleKey: localizedKey(kind, id, names[id] != ""),
		Classes:       collectTags(values, []string{"tempest", "sentinel", "ravager"}),
		Elements:      collectTags(values, []string{"bio", "cyber", "plasma", "chrono", "necro"}),
		Slots:         collectTags(values, []string{"defense", "offense", "utility", "weapon", "grasper", "foot"}),
		Stats:         decodeAffixStats(resource.Payload, kind),
	}
}

var prefixAttributeNames = []string{
	"Strength", "Dexterity", "Mind", "Max Health Increase", "Max Health", "Max Power",
	"Damage Reduction", "Physical Defense", "Physical Damage Reduction", "Energy Defense", "Critical Rating",
	"Non-combat Speed", "Combat Speed", "Damage Buff", "Silence", "Immobilized", "Basic Damage Defense",
	"Physical Damage Increase", "Flat Physical Damage Increase", "Automatic Critical", "Rear Direct Damage",
	"Rear or Side Direct Damage", "Critical Damage", "Attack Speed", "Cooldown Scale", "Frozen",
	"Projectile Speed", "Area Resistance", "Energy Damage Buff", "Intangible", "Healing Reduction",
	"Energy Damage Increase", "Flat Energy Damage Increase", "Immunity", "Stealth Detection", "Life Steal",
	"Reject Modifier", "Area Damage", "Cyber Damage", "Chrono Damage", "Bio Damage", "Plasma Damage", "Necro Damage",
	"Cyber Resistance", "Chrono Resistance", "Bio Resistance", "Plasma Resistance", "Necro Resistance",
	"Movement Speed", "Debuff Immunity", "Buff Duration", "Debuff Duration", "Power Steal", "Debuff Duration Increase",
	"Energy Damage Reduction", "Incorporeal", "Damage over Time", "Mind Control", "Swap Disabled",
	"Random Teleport Immunity", "Banish Immunity", "Knockback Immunity", "Area Radius", "Pet Damage", "Pet Health",
	"Crystal Find", "DNA Dropped", "Range", "Orb Effectiveness", "Overdrive Buildup", "Overdrive Duration", "Loot Find",
	"Surefooted", "Stun Immunity", "Shock Immunity", "Sleep Immunity", "Taunt Immunity", "Terror Immunity",
	"Silence Immunity", "Curse Immunity", "Poison or Disease Immunity", "Burning Immunity", "Root Immunity",
	"Slow Immunity", "Pull Immunity", "Damage over Time Increase", "Aggro Increase", "Aggro Decrease",
	"Physical Damage Done", "Physical Ability Damage", "Energy Damage Done", "Energy Ability Damage",
	"Channel Time Reduction", "Crowd Control Reduction", "Damage over Time Duration Reduction", "Area Duration",
	"Healing", "Lockdown", "Healing over Time", "Projectile Damage", "Squad Damage Distribution",
	"Deploy Invincibility", "Flat Physical Damage Reduction", "Flat Energy Damage Reduction", "Minimum Weapon Damage",
	"Maximum Weapon Damage", "Minimum Weapon Damage Percent", "Maximum Weapon Damage Percent", "Direct Attack Damage",
	"Direct Attack Damage Percent", "Hit Animation Disabled", "XP Boost", "Security Teleporter Invisibility", "Body Scale",
	"Physical Damage Defense",
}

func decodePrefixName(payload []byte) string {
	modifiers := make([]string, 0, 2)
	for _, stat := range decodeAffixStats(payload, "Prefix") {
		modifiers = append(modifiers, stat.Name)
	}
	return strings.Join(modifiers, " + ")
}

func decodeAffixStats(payload []byte, kind string) []affixStat {
	offset := 36
	if strings.EqualFold(kind, "Suffix") {
		offset = 56
	}
	stats := make([]affixStat, 0, 2)
	for index, name := range prefixAttributeNames {
		fieldOffset := offset + index*4
		if fieldOffset+4 > len(payload) {
			break
		}
		value := math.Float32frombits(binary.LittleEndian.Uint32(payload[fieldOffset : fieldOffset+4]))
		if value == 0 || math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			continue
		}
		stat := affixStat{Index: index, Name: authoredStatName(index, name), AuthoredValue: float64(value)}
		stat.Preview, stat.PreviewExact = levelOneStatPreview(index, value, stat.Name)
		stats = append(stats, stat)
	}
	return stats
}

func authoredStatName(index int, fallback string) string {
	switch index {
	case 5:
		return "Power"
	case 22:
		return "Critical Damage"
	case 37:
		return "Area Effect Damage"
	case 43:
		return "Damage from Cyber Enemies"
	case 44:
		return "Damage from Quantum Enemies"
	case 45:
		return "Damage from Bio Enemies"
	case 46:
		return "Damage from Plasma Enemies"
	case 47:
		return "Damage from Necro Enemies"
	default:
		return fallback
	}
}

func levelOneStatPreview(index int, value float32, name string) (string, bool) {
	if index >= 43 && index <= 47 {
		return fmt.Sprintf("-%s%% %s", compactFloat(value), name), true
	}
	switch index {
	case 22, 37:
		return fmt.Sprintf("+%s%% %s", compactFloat(value), name), true
	default:
		return "", false
	}
}

func compactFloat(value float32) string {
	return strconv.FormatFloat(float64(value), 'f', -1, 32)
}

func localizedKey(kind string, id uint32, isLocalized bool) string {
	if !isLocalized {
		return ""
	}
	return fmt.Sprintf("Loot%sNames!0x%08x", kind, id)
}

func payloadID(payload []byte) uint32 {
	if len(payload) < 4 {
		return 0
	}
	return binary.LittleEndian.Uint32(payload[:4])
}

func printableStrings(data []byte, minimum int) []string {
	result := make([]string, 0, 16)
	var current strings.Builder
	flush := func() {
		if current.Len() >= minimum {
			result = append(result, current.String())
		}
		current.Reset()
	}
	for _, value := range data {
		if value >= 0x20 && value <= 0x7e {
			current.WriteByte(value)
		} else {
			flush()
		}
	}
	flush()
	return result
}

func firstTag(values, tags []string, fallback string) string {
	for _, tag := range tags {
		for _, value := range values {
			if value == tag {
				return tag
			}
		}
	}
	return fallback
}

func collectTags(values, tags []string) []string {
	result := make([]string, 0, len(tags))
	for _, tag := range tags {
		for _, value := range values {
			if strings.EqualFold(value, tag) {
				result = append(result, strings.ToUpper(tag))
				break
			}
		}
	}
	return result
}

func writeLookup(path string, items []itemFamily) error {
	rows := make([][]any, 0, len(items))
	for _, entry := range items {
		rows = append(rows, []any{entry.VariantCount, entry.Name, entry.Category, entry.Classes, entry.Elements, "", "", entry.Slug})
	}
	return catalog.WriteCompactJSON(path, rows)
}

func writeContent(path string, items []itemFamily) error {
	wanted := make(map[string]struct{}, len(items))
	for _, entry := range items {
		wanted[entry.Slug+".md"] = struct{}{}
	}
	files, err := os.ReadDir(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("contentRead: %w", err)
	}
	for _, file := range files {
		if file.IsDir() || file.Name() == "_index.md" || filepath.Ext(file.Name()) != ".md" {
			continue
		}
		if _, keep := wanted[file.Name()]; keep {
			continue
		}
		stalePath := filepath.Join(path, file.Name())
		contents, readErr := os.ReadFile(stalePath)
		if readErr != nil {
			return fmt.Errorf("staleRead[%s]: %w", file.Name(), readErr)
		}
		if !strings.Contains(string(contents), "generated = true") {
			continue
		}
		if removeErr := os.Remove(stalePath); removeErr != nil {
			return fmt.Errorf("staleRemove[%s]: %w", file.Name(), removeErr)
		}
	}
	for _, entry := range items {
		contents := fmt.Sprintf("+++\ntitle = %q\nitem = %q\ngenerated = true\n+++\n", entry.Name, entry.Slug)
		if err := catalog.WriteText(filepath.Join(path, entry.Slug+".md"), contents); err != nil {
			return fmt.Errorf("contentWrite[%s]: %w", entry.Slug, err)
		}
	}
	return nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
