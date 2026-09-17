package main

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/darkspinnet/web/scripts/internal/catalog"
)

type buffSpec struct {
	Name          string
	Category      string
	Keywords      []string
	IconReference string
}

type abilityCatalog struct {
	Items []struct {
		Name        string `json:"name"`
		Slug        string `json:"slug"`
		Description string `json:"description"`
	} `json:"items"`
}

type abilityLink struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type buffEntry struct {
	Name          string        `json:"name"`
	Slug          string        `json:"slug"`
	Category      string        `json:"category"`
	Description   string        `json:"description"`
	LocaleKey     string        `json:"locale_key"`
	TableID       string        `json:"table_id"`
	IconReference string        `json:"icon_reference,omitempty"`
	IconPath      string        `json:"icon_path,omitempty"`
	Abilities     []abilityLink `json:"abilities"`
}

type buffCatalog struct {
	Count      int                  `json:"count"`
	Categories map[string]int       `json:"categories"`
	Items      []buffEntry          `json:"items"`
	Entry      map[string]buffEntry `json:"entry"`
}

var buffSpecs = []buffSpec{
	{Name: "Banished", Category: "control", Keywords: []string{"banish"}},
	{Name: "Burning", Category: "damage over time", Keywords: []string{"burning", "ignite"}},
	{Name: "Dazed", Category: "control", Keywords: []string{"dazed", "daze"}},
	{Name: "Diseased", Category: "damage over time", Keywords: []string{"disease", "diseased"}},
	{Name: "Enraged", Category: "buff", Keywords: []string{"enrage", "enraged"}},
	{Name: "Hasted", Category: "buff", Keywords: []string{"haste", "hasted"}},
	{Name: "Poisoned", Category: "damage over time", Keywords: []string{"poison", "poisoned"}, IconReference: "debuff_poison"},
	{Name: "Rooted", Category: "movement", Keywords: []string{"root", "rooted"}},
	{Name: "Shielded", Category: "defense", Keywords: []string{"shielded", "shield"}},
	{Name: "Shocked", Category: "control", Keywords: []string{"shock", "shocked"}},
	{Name: "Sleep", Category: "control", Keywords: []string{"sleep", "asleep"}},
	{Name: "Slowed", Category: "movement", Keywords: []string{"slowed", "slow"}},
	{Name: "Snared", Category: "movement", Keywords: []string{"snare", "snared"}},
	{Name: "Stunned", Category: "control", Keywords: []string{"stun", "stunned"}},
	{Name: "Suppressed", Category: "control", Keywords: []string{"suppress", "suppressed"}},
	{Name: "Taunted", Category: "control", Keywords: []string{"taunt", "taunted"}},
	{Name: "Terrified", Category: "control", Keywords: []string{"terror", "terrified"}},
}

func main() {
	databasePath := flag.String("db", catalog.DefaultDatabasePath, "content.db path")
	abilityPath := flag.String("ability", filepath.Join("data", "generated", "ability.json"), "generated ability catalog")
	outputPath := flag.String("out", filepath.Join("data", "generated", "buff.json"), "generated buff catalog")
	contentPath := flag.String("content", filepath.Join("content", "buff"), "generated buff content directory")
	flag.Parse()
	database, err := catalog.Open(*databasePath)
	if err != nil {
		fatal(err)
	}
	defer database.Close()
	abilities, err := loadAbilities(*abilityPath)
	if err != nil {
		fatal(err)
	}
	result, err := build(database, abilities)
	if err != nil {
		fatal(err)
	}
	if err = catalog.WriteJSON(*outputPath, result); err != nil {
		fatal(err)
	}
	if err = writeContent(*contentPath, result.Items); err != nil {
		fatal(err)
	}
	fmt.Printf("buff: wrote %d localized gameplay conditions to %s\n", result.Count, *outputPath)
}

func loadAbilities(path string) (abilityCatalog, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return abilityCatalog{}, fmt.Errorf("abilityRead: %w", err)
	}
	var abilities abilityCatalog
	if err = json.Unmarshal(contents, &abilities); err != nil {
		return abilityCatalog{}, fmt.Errorf("abilityDecode: %w", err)
	}
	return abilities, nil
}

func build(database *sql.DB, abilities abilityCatalog) (buffCatalog, error) {
	result := buffCatalog{Categories: make(map[string]int), Entry: make(map[string]buffEntry)}
	for _, spec := range buffSpecs {
		entry, err := loadLocalizedStatus(database, spec)
		if err != nil {
			return result, fmt.Errorf("locale[%s]: %w", spec.Name, err)
		}
		entry.Abilities = referencedAbilities(abilities, spec.Keywords)
		result.Items = append(result.Items, entry)
		result.Entry[entry.Slug] = entry
		result.Categories[entry.Category]++
	}
	sort.Slice(result.Items, func(i, j int) bool { return result.Items[i].Name < result.Items[j].Name })
	result.Count = len(result.Items)
	return result, nil
}

func loadLocalizedStatus(database *sql.DB, spec buffSpec) (buffEntry, error) {
	rows, err := database.Query(`
		SELECT table_id, locale_key FROM localization_text
		WHERE locale='en-us' AND localized_text=? COLLATE NOCASE ORDER BY table_id`, spec.Name)
	if err != nil {
		return buffEntry{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var tableID uint64
		var localeKey string
		if err = rows.Scan(&tableID, &localeKey); err != nil {
			return buffEntry{}, err
		}
		key, parseErr := strconv.ParseUint(strings.TrimPrefix(strings.ToLower(localeKey), "0x"), 16, 64)
		if parseErr != nil {
			continue
		}
		descriptionKey := fmt.Sprintf("0x%08x", key+1)
		var description string
		scanErr := database.QueryRow(`
			SELECT localized_text FROM localization_text
			WHERE locale='en-us' AND table_id=? AND locale_key=?`, tableID, descriptionKey).Scan(&description)
		if scanErr != nil || len(strings.TrimSpace(description)) < 12 {
			continue
		}
		return buffEntry{
			Name: spec.Name, Slug: catalog.Slug(spec.Name), Category: spec.Category,
			Description: strings.TrimSpace(description), LocaleKey: localeKey,
			TableID: fmt.Sprintf("0x%08x", tableID), IconReference: spec.IconReference,
			IconPath: statusIconPath(spec), Abilities: []abilityLink{},
		}, nil
	}
	return buffEntry{}, fmt.Errorf("localized pair not found")
}

func statusIconPath(spec buffSpec) string {
	if spec.IconReference == "" {
		return ""
	}
	return "/image/buff/" + catalog.Slug(spec.Name) + ".webp"
}

func referencedAbilities(abilities abilityCatalog, keywords []string) []abilityLink {
	links := make([]abilityLink, 0)
	for _, ability := range abilities.Items {
		haystack := strings.ToLower(ability.Name + " " + ability.Description)
		for _, keyword := range keywords {
			if !strings.Contains(haystack, keyword) {
				continue
			}
			links = append(links, abilityLink{Name: ability.Name, Slug: ability.Slug})
			break
		}
	}
	sort.Slice(links, func(i, j int) bool { return links[i].Name < links[j].Name })
	return links
}

func writeContent(path string, entries []buffEntry) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	for _, entry := range entries {
		contents := fmt.Sprintf("+++\ntitle = %q\nbuff = %q\ngenerated = true\n+++\n", entry.Name, entry.Slug)
		if err := catalog.WriteText(filepath.Join(path, entry.Slug+".md"), contents); err != nil {
			return fmt.Errorf("contentWrite[%s]: %w", entry.Slug, err)
		}
	}
	return nil
}

func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
