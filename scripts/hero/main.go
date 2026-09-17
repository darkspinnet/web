package main

import (
	"database/sql"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/darkspinnet/darkspin/content/dbpf"
	"github.com/darkspinnet/web/scripts/internal/catalog"
)

const playerClassType = uint32(0xe51118c3)
const playerClassStatType = uint32(0x474940a5)
const playerClassStatSize = 88

type heroStats struct {
	Strength       float64 `json:"strength"`
	Dexterity      float64 `json:"dexterity"`
	Mind           float64 `json:"mind"`
	Health         float64 `json:"health"`
	Power          float64 `json:"power"`
	CriticalRating float64 `json:"critical_rating"`
	DodgeRating    float64 `json:"dodge_rating"`
	ResistRating   float64 `json:"resist_rating"`
}

type heroMetadata struct {
	UnlockLevel int
	Stats       heroStats
}

type ability struct {
	Slot      string `json:"slot"`
	AssetName string `json:"asset_name"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
}

type hero struct {
	ID              uint32    `json:"id"`
	Name            string    `json:"name"`
	Family          string    `json:"family"`
	Variant         string    `json:"variant"`
	Element         string    `json:"element"`
	Class           string    `json:"class"`
	WeaponMinDamage float64   `json:"weapon_min_damage"`
	WeaponMaxDamage float64   `json:"weapon_max_damage"`
	IsHandPresent   bool      `json:"is_hand_present"`
	IsFootPresent   bool      `json:"is_foot_present"`
	NameLocaleKey   string    `json:"name_locale_key"`
	UnlockLevel     int       `json:"unlock_level"`
	Stats           heroStats `json:"stats"`
	Image           string    `json:"image"`
	Abilities       []ability `json:"abilities"`
}

type heroCatalog struct {
	Count       int                   `json:"count"`
	FamilyCount int                   `json:"family_count"`
	Elements    map[string]int        `json:"elements"`
	Classes     map[string]int        `json:"classes"`
	Families    []heroFamily          `json:"families"`
	Family      map[string]heroFamily `json:"family"`
	Heroes      []hero                `json:"heroes"`
}

type heroFamily struct {
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	Element   string `json:"element"`
	Class     string `json:"class"`
	Title     string `json:"title"`
	Biography string `json:"biography"`
	Variants  []hero `json:"variants"`
}

func main() {
	databasePath := flag.String("db", catalog.DefaultDatabasePath, "content.db path")
	gamePath := flag.String("game", catalog.DefaultGamePath, "Darkspore install root")
	outputPath := flag.String("out", filepath.Join("data", "generated", "hero.json"), "generated JSON path")
	contentPath := flag.String("content", filepath.Join("content", "hero"), "generated hero content directory")
	flag.Parse()
	database, err := catalog.Open(*databasePath)
	if err != nil {
		fatal(err)
	}
	defer database.Close()
	metadataByLocaleKey, err := loadHeroMetadata(filepath.Join(*gamePath, "Data", "AssetData_Binary.package"))
	if err != nil {
		fatal(err)
	}
	heroes, err := loadHeroes(database, metadataByLocaleKey)
	if err != nil {
		fatal(err)
	}
	families := groupFamilies(heroes)
	err = loadHeroLore(database, families)
	if err != nil {
		fatal(err)
	}
	familyBySlug := make(map[string]heroFamily, len(families))
	for _, family := range families {
		familyBySlug[family.Slug] = family
	}
	result := heroCatalog{Count: len(heroes), FamilyCount: len(families), Elements: map[string]int{}, Classes: map[string]int{}, Families: families, Family: familyBySlug, Heroes: heroes}
	for _, hero := range heroes {
		result.Elements[hero.Element]++
		result.Classes[hero.Class]++
	}
	err = catalog.WriteJSON(*outputPath, result)
	if err != nil {
		fatal(err)
	}
	err = writeContent(*contentPath, families)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("heroes: wrote %d records to %s\n", len(heroes), *outputPath)
}

func loadHeroLore(database *sql.DB, families []heroFamily) error {
	for familyIndex := range families {
		family := &families[familyIndex]
		keys := make([]string, 0, len(family.Variants))
		seen := make(map[string]struct{}, len(family.Variants))
		for _, variant := range family.Variants {
			if _, exists := seen[variant.NameLocaleKey]; exists {
				continue
			}
			seen[variant.NameLocaleKey] = struct{}{}
			keys = append(keys, variant.NameLocaleKey)
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(keys)), ",")
		arguments := make([]any, len(keys))
		for index, key := range keys {
			arguments[index] = key
		}
		rows, err := database.Query(`
			SELECT table_id, localized_text
			FROM localization_text
			WHERE locale = 'en-us' AND locale_key IN (`+placeholders+`)
			ORDER BY table_id`, arguments...)
		if err != nil {
			return fmt.Errorf("heroLoreNameQuery[%s]: %w", family.Name, err)
		}
		var tableID uint64
		for rows.Next() {
			var candidateID uint64
			var localizedName string
			if err = rows.Scan(&candidateID, &localizedName); err != nil {
				_ = rows.Close()
				return fmt.Errorf("heroLoreNameScan[%s]: %w", family.Name, err)
			}
			cleanName := stripPrivateUse(localizedName)
			if len(cleanName) < len(family.Name) || !strings.EqualFold(cleanName[:len(family.Name)], family.Name) {
				continue
			}
			tableID = candidateID
			family.Title = strings.TrimSpace(strings.TrimLeft(cleanName[len(family.Name):], ","))
			break
		}
		if err = rows.Close(); err != nil {
			return fmt.Errorf("heroLoreNameClose[%s]: %w", family.Name, err)
		}
		if tableID == 0 {
			return fmt.Errorf("heroLoreName[%s]: localized record not found", family.Name)
		}
		err = database.QueryRow(`
			SELECT localized_text FROM localization_text
			WHERE locale = 'en-us' AND table_id = ? AND locale_key = '0x0acaf252'`, tableID).Scan(&family.Biography)
		if err != nil {
			return fmt.Errorf("heroLoreBiography[%s]: %w", family.Name, err)
		}
		family.Biography = strings.TrimSpace(strings.ReplaceAll(family.Biography, "~br~", "\n"))
	}
	return nil
}

func stripPrivateUse(value string) string {
	return strings.Map(func(character rune) rune {
		if character >= 0xe000 && character <= 0xf8ff {
			return -1
		}
		return character
	}, value)
}

func writeContent(path string, families []heroFamily) error {
	for _, family := range families {
		contents := fmt.Sprintf("+++\ntitle = %q\nhero = %q\n+++\n", family.Name, family.Slug)
		err := catalog.WriteText(filepath.Join(path, family.Slug+".md"), contents)
		if err != nil {
			return fmt.Errorf("contentWrite[%s]: %w", family.Slug, err)
		}
	}
	return nil
}

func groupFamilies(heroes []hero) []heroFamily {
	familyByName := make(map[string]*heroFamily, 25)
	for _, entry := range heroes {
		family := familyByName[entry.Family]
		if family == nil {
			family = &heroFamily{Name: entry.Family, Slug: catalog.Slug(entry.Family), Element: entry.Element, Class: entry.Class}
			familyByName[entry.Family] = family
		}
		family.Variants = append(family.Variants, entry)
	}
	families := make([]heroFamily, 0, len(familyByName))
	for _, family := range familyByName {
		sort.Slice(family.Variants, func(left, right int) bool {
			return variantOrder(family.Variants[left].Variant) < variantOrder(family.Variants[right].Variant)
		})
		families = append(families, *family)
	}
	sort.Slice(families, func(left, right int) bool {
		return strings.ToLower(families[left].Name) < strings.ToLower(families[right].Name)
	})
	return families
}

func variantOrder(variant string) int {
	orders := map[string]int{"Alpha": 0, "Beta": 1, "Gamma": 2, "Delta": 3}
	order, isFound := orders[variant]
	if !isFound {
		return len(orders)
	}
	return order
}

func loadHeroes(database *sql.DB, metadataByLocaleKey map[string]heroMetadata) ([]hero, error) {
	abilityByHero, err := loadAbilities(database)
	if err != nil {
		return nil, fmt.Errorf("abilitiesLoad: %w", err)
	}
	rows, err := database.Query(`
		SELECT id, name, element_type, class_type, weapon_min_damage, weapon_max_damage,
		       is_hand_present, is_foot_present, name_locale_key
		FROM creature_template ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, fmt.Errorf("heroQuery: %w", err)
	}
	defer rows.Close()
	heroes := make([]hero, 0, 100)
	for rows.Next() {
		var entry hero
		err = rows.Scan(&entry.ID, &entry.Name, &entry.Element, &entry.Class,
			&entry.WeaponMinDamage, &entry.WeaponMaxDamage, &entry.IsHandPresent,
			&entry.IsFootPresent, &entry.NameLocaleKey)
		if err != nil {
			return nil, fmt.Errorf("heroScan: %w", err)
		}
		entry.Family, entry.Variant = splitVariant(entry.Name)
		metadata := metadataByLocaleKey[heroLocaleIdentity(entry.Family, entry.NameLocaleKey)]
		entry.UnlockLevel = metadata.UnlockLevel
		entry.Stats = metadata.Stats
		entry.Image = fmt.Sprintf("/image/hero/%d.webp", entry.ID)
		entry.Abilities = abilityByHero[entry.ID]
		if entry.Abilities == nil {
			entry.Abilities = []ability{}
		}
		heroes = append(heroes, entry)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("heroRows: %w", err)
	}
	return heroes, nil
}

func loadHeroMetadata(packagePath string) (map[string]heroMetadata, error) {
	source, err := os.Open(packagePath)
	if err != nil {
		return nil, fmt.Errorf("playerClassPackageOpen: %w", err)
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return nil, fmt.Errorf("playerClassPackageStat: %w", err)
	}
	reader, err := dbpf.NewReader(source, info.Size())
	if err != nil {
		return nil, fmt.Errorf("playerClassPackageIndex: %w", err)
	}
	statByInstance := make(map[uint32]heroStats, 100)
	for _, resource := range reader.Entries {
		if resource.Type != playerClassStatType {
			continue
		}
		payloadReader, openErr := reader.Open(resource)
		if openErr != nil {
			return nil, fmt.Errorf("playerClassStatOpen[%x]: %w", resource.Instance, openErr)
		}
		payload, readErr := io.ReadAll(payloadReader)
		if readErr != nil {
			return nil, fmt.Errorf("playerClassStatRead[%x]: %w", resource.Instance, readErr)
		}
		stats, isStat := decodeHeroStats(payload)
		if isStat {
			statByInstance[uint32(resource.Instance)] = stats
		}
	}
	metadata := make(map[string]heroMetadata, 100)
	for _, resource := range reader.Entries {
		if resource.Type != playerClassType {
			continue
		}
		payloadReader, openErr := reader.Open(resource)
		if openErr != nil {
			return nil, fmt.Errorf("playerClassOpen[%x]: %w", resource.Instance, openErr)
		}
		payload, readErr := io.ReadAll(payloadReader)
		if readErr != nil {
			return nil, fmt.Errorf("playerClassRead[%x]: %w", resource.Instance, readErr)
		}
		if len(payload) <= 256 || binary.LittleEndian.Uint32(payload[4:8]) > 4 || binary.LittleEndian.Uint32(payload[72:76]) > 2 {
			continue
		}
		fields, fieldErr := readNullStrings(payload[256:], 10)
		if fieldErr != nil || !strings.HasSuffix(fields[9], ".ClassAttributes") {
			continue
		}
		localeKey := fmt.Sprintf("0x%08x", binary.LittleEndian.Uint32(payload[20:24]))
		statName := strings.TrimSuffix(fields[9], ".ClassAttributes")
		stats, isFound := statByInstance[hashID(statName)]
		if !isFound {
			return nil, fmt.Errorf("playerClassStatMissing[%s]", statName)
		}
		metadata[heroLocaleIdentity(fields[3], localeKey)] = heroMetadata{
			UnlockLevel: int(binary.LittleEndian.Uint32(payload[80:84])),
			Stats:       stats,
		}
	}
	if len(metadata) != 100 {
		return nil, fmt.Errorf("playerClassCount: got %d, want 100", len(metadata))
	}
	return metadata, nil
}

func decodeHeroStats(payload []byte) (heroStats, bool) {
	if len(payload) != playerClassStatSize {
		return heroStats{}, false
	}
	field := func(index int) float64 {
		offset := index * 4
		return float64(math.Float32frombits(binary.LittleEndian.Uint32(payload[offset : offset+4])))
	}
	baseHealth, basePower := field(0), field(1)
	strength, dexterity, mind := field(2), field(3), field(4)
	baseDodge, baseResist, baseCritical := field(5), field(7), field(8)
	values := []float64{baseHealth, basePower, strength, dexterity, mind, baseDodge, baseResist, baseCritical}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 100000 {
			return heroStats{}, false
		}
	}
	health := math.Max(0, baseHealth+(strength-10)*5)
	return heroStats{
		Strength: math.Round(strength), Dexterity: math.Round(dexterity), Mind: math.Round(mind),
		Health: math.Round(health), Power: math.Round(basePower + mind),
		CriticalRating: math.Round(baseCritical + dexterity*4),
		DodgeRating:    math.Round(baseDodge + dexterity*6),
		ResistRating:   math.Round(baseResist + mind*6),
	}, true
}

func hashID(name string) uint32 {
	hash := uint32(0x811c9dc5)
	for index := 0; index < len(name); index++ {
		hash *= 0x01000193
		character := name[index]
		if character >= 'A' && character <= 'Z' {
			character += 'a' - 'A'
		}
		hash ^= uint32(character)
	}
	return hash
}

func heroLocaleIdentity(family, localeKey string) string {
	return strings.ToLower(family) + "|" + strings.ToLower(localeKey)
}

func readNullStrings(payload []byte, count int) ([]string, error) {
	fields := make([]string, 0, count)
	for len(fields) < count {
		end := 0
		for end < len(payload) && payload[end] != 0 {
			end++
		}
		if end == len(payload) {
			return nil, fmt.Errorf("unterminated string %d", len(fields))
		}
		fields = append(fields, string(payload[:end]))
		payload = payload[end+1:]
	}
	return fields, nil
}

func loadAbilities(database *sql.DB) (map[uint32][]ability, error) {
	rows, err := database.Query(`
		SELECT creature_template_id, slot, asset_name
		FROM creature_template_ability ORDER BY creature_template_id, slot`)
	if err != nil {
		return nil, fmt.Errorf("abilityQuery: %w", err)
	}
	defer rows.Close()
	abilityByHero := make(map[uint32][]ability, 100)
	for rows.Next() {
		var heroID uint32
		var entry ability
		err = rows.Scan(&heroID, &entry.Slot, &entry.AssetName)
		if err != nil {
			return nil, fmt.Errorf("abilityScan: %w", err)
		}
		entry.Name = catalog.DisplayName(entry.AssetName)
		entry.Slug = catalog.Slug(entry.AssetName)
		abilityByHero[heroID] = append(abilityByHero[heroID], entry)
	}
	err = rows.Err()
	if err != nil {
		return nil, fmt.Errorf("abilityRows: %w", err)
	}
	for heroID := range abilityByHero {
		sort.Slice(abilityByHero[heroID], func(left, right int) bool {
			return abilityByHero[heroID][left].Slot < abilityByHero[heroID][right].Slot
		})
	}
	return abilityByHero, nil
}

func splitVariant(name string) (string, string) {
	parts := strings.Fields(name)
	if len(parts) < 2 {
		return name, ""
	}
	return strings.Join(parts[:len(parts)-1], " "), parts[len(parts)-1]
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
