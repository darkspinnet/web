package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/darkspinnet/darkspin/content"
	"github.com/darkspinnet/darkspin/content/dbpf"
	"github.com/darkspinnet/web/scripts/internal/catalog"
)

const (
	pngType          = uint32(0x2f7d0004)
	itemIconGroup    = uint32(0x100d977c)
	abilityIconGroup = uint32(0x099dfb88)
	buffIconGroup    = uint32(0x099dfb95)
)

type abilityAssetCatalog struct {
	Items []struct {
		IconReference string `json:"icon_reference"`
		IconPath      string `json:"icon_path"`
	} `json:"items"`
}

type buffAssetCatalog struct {
	Items []struct {
		IconReference string `json:"icon_reference"`
		IconPath      string `json:"icon_path"`
	} `json:"items"`
}

type itemAssetCatalog struct {
	Items []struct {
		Variants []struct {
			IconReference string `json:"icon_reference"`
			IconPath      string `json:"icon_path"`
		} `json:"variants"`
	} `json:"items"`
}

type heroAssetCatalog struct {
	Families []struct {
		Variants []struct {
			ID uint32 `json:"id"`
		} `json:"variants"`
	} `json:"families"`
}

func main() {
	gamePath := flag.String("game", catalog.DefaultGamePath, "Darkspore install root")
	outputPath := flag.String("out", filepath.Join("static", "image", "hero"), "WebP output directory")
	heroDataPath := flag.String("heroes", filepath.Join("data", "generated", "hero.json"), "generated hero catalog")
	itemDataPath := flag.String("items", filepath.Join("data", "generated", "item.json"), "generated item catalog")
	itemOutputPath := flag.String("item-out", filepath.Join("static", "image", "item"), "item icon WebP output directory")
	abilityDataPath := flag.String("abilities", filepath.Join("data", "generated", "ability.json"), "generated ability catalog")
	abilityOutputPath := flag.String("ability-out", filepath.Join("static", "image", "ability"), "ability icon WebP output directory")
	buffDataPath := flag.String("buff", filepath.Join("data", "generated", "buff.json"), "generated buff catalog")
	buffOutputPath := flag.String("buff-out", filepath.Join("static", "image", "buff"), "buff icon WebP output directory")
	quality := flag.Int("quality", 84, "WebP quality")
	flag.Parse()
	err := generate(context.Background(), *gamePath, *outputPath, *heroDataPath, *itemDataPath, *itemOutputPath, *abilityDataPath, *abilityOutputPath, *buffDataPath, *buffOutputPath, *quality)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate(ctx context.Context, gamePath, outputPath, heroDataPath, itemDataPath, itemOutputPath, abilityDataPath, abilityOutputPath, buffDataPath, buffOutputPath string, quality int) error {
	if quality < 1 || quality > 100 {
		return errors.New("quality must be between 1 and 100")
	}
	magickPath, err := exec.LookPath("magick")
	if err != nil {
		return fmt.Errorf("magickLookup: %w", err)
	}
	temporaryPath, err := os.MkdirTemp("", "web-assets-*")
	if err != nil {
		return fmt.Errorf("temporaryCreate: %w", err)
	}
	defer os.RemoveAll(temporaryPath)
	err = os.MkdirAll(outputPath, 0o755)
	if err != nil {
		return fmt.Errorf("outputCreate: %w", err)
	}
	heroIDs, err := loadCanonicalHeroIDs(heroDataPath)
	if err != nil {
		return err
	}
	wanted := make(map[string]struct{}, len(heroIDs))
	missing := make([]string, 0, len(heroIDs))
	for _, heroID := range heroIDs {
		fileName := heroID + ".webp"
		wanted[fileName] = struct{}{}
		targetPath := filepath.Join(outputPath, fileName)
		if info, statErr := os.Stat(targetPath); statErr != nil || info.Size() == 0 {
			missing = append(missing, heroID)
		}
	}
	if err = pruneGeneratedWebP(outputPath, wanted); err != nil {
		return fmt.Errorf("heroPrune: %w", err)
	}
	if len(missing) > 0 {
		if err = content.PrepareWeb(ctx, content.WebOptions{GamePath: gamePath, StaticPath: temporaryPath}); err != nil {
			return fmt.Errorf("assetsExtract: %w", err)
		}
		inputPath := filepath.Join(temporaryPath, "template_png")
		for _, heroID := range missing {
			sourcePath := filepath.Join(inputPath, heroID+"_thumb.png")
			targetPath := filepath.Join(outputPath, heroID+".webp")
			command := exec.CommandContext(ctx, magickPath, sourcePath, "-strip", "-quality", fmt.Sprint(quality), "-define", "webp:method=6", targetPath)
			commandOutput, commandErr := command.CombinedOutput()
			if commandErr != nil {
				return fmt.Errorf("webpConvert[%s]: %w: %s", heroID, commandErr, strings.TrimSpace(string(commandOutput)))
			}
		}
	}
	fmt.Printf("assets: retained %d canonical hero portraits in %s\n", len(heroIDs), outputPath)
	itemCount, missingCount, err := generateItemIcons(ctx, magickPath, filepath.Join(gamePath, "Data", "UI.package"), itemDataPath, itemOutputPath, temporaryPath, quality)
	if err != nil {
		return fmt.Errorf("itemIcons: %w", err)
	}
	fmt.Printf("assets: extracted and converted %d item icons to %s (%d references unavailable)\n", itemCount, itemOutputPath, missingCount)
	abilityCount, missingAbilityCount, err := generateAbilityIcons(ctx, magickPath, filepath.Join(gamePath, "Data", "UI.package"), abilityDataPath, abilityOutputPath, temporaryPath, quality)
	if err != nil {
		return fmt.Errorf("abilityIcons: %w", err)
	}
	fmt.Printf("assets: extracted and converted %d ability icons to %s (%d references unavailable)\n", abilityCount, abilityOutputPath, missingAbilityCount)
	buffCount, missingBuffCount, err := generateBuffIcons(ctx, magickPath, filepath.Join(gamePath, "Data", "UI.package"), buffDataPath, buffOutputPath, temporaryPath, quality)
	if err != nil {
		return fmt.Errorf("buffIcons: %w", err)
	}
	fmt.Printf("assets: extracted and converted %d buff icons to %s (%d references unavailable)\n", buffCount, buffOutputPath, missingBuffCount)
	return nil
}

func generateBuffIcons(ctx context.Context, magickPath, packagePath, dataPath, outputPath, temporaryPath string, quality int) (int, int, error) {
	contents, err := os.ReadFile(dataPath)
	if err != nil {
		return 0, 0, fmt.Errorf("catalogRead: %w", err)
	}
	var buffData buffAssetCatalog
	if err = json.Unmarshal(contents, &buffData); err != nil {
		return 0, 0, fmt.Errorf("catalogDecode: %w", err)
	}
	return generateNamedIcons(ctx, magickPath, packagePath, outputPath, temporaryPath, quality, buffIconGroup, buffData.Items)
}

func generateNamedIcons(ctx context.Context, magickPath, packagePath, outputPath, temporaryPath string, quality int, group uint32, items []struct {
	IconReference string `json:"icon_reference"`
	IconPath      string `json:"icon_path"`
}) (int, int, error) {
	r, err := os.Open(packagePath)
	if err != nil {
		return 0, 0, fmt.Errorf("packageOpen: %w", err)
	}
	defer r.Close()
	fi, err := r.Stat()
	if err != nil {
		return 0, 0, fmt.Errorf("packageStat: %w", err)
	}
	pkg, err := dbpf.NewReader(r, fi.Size())
	if err != nil {
		return 0, 0, fmt.Errorf("packageIndex: %w", err)
	}
	resources := make(map[uint32]dbpf.Entry)
	for _, entry := range pkg.Entries {
		if entry.Type == pngType && entry.Group == group && entry.Instance <= uint64(^uint32(0)) {
			resources[uint32(entry.Instance)] = entry
		}
	}
	if err = os.MkdirAll(outputPath, 0o755); err != nil {
		return 0, 0, fmt.Errorf("outputCreate: %w", err)
	}
	wanted := make(map[string]struct{})
	converted, missing := 0, 0
	for _, item := range items {
		if item.IconReference == "" || item.IconPath == "" {
			continue
		}
		filename := filepath.Base(item.IconPath)
		wanted[filename] = struct{}{}
		targetPath := filepath.Join(outputPath, filename)
		if existing, statErr := os.Stat(targetPath); statErr == nil && existing.Size() > 0 {
			converted++
			continue
		}
		entry, found := resources[catalog.HashID(item.IconReference)]
		if !found {
			missing++
			continue
		}
		payloadReader, openErr := pkg.Open(entry)
		if openErr != nil {
			return converted, missing, fmt.Errorf("resourceOpen[%s]: %w", item.IconReference, openErr)
		}
		payload, readErr := io.ReadAll(payloadReader)
		if readErr != nil {
			return converted, missing, fmt.Errorf("resourceRead[%s]: %w", item.IconReference, readErr)
		}
		if len(payload) < 8 || string(payload[:8]) != "\x89PNG\r\n\x1a\n" {
			return converted, missing, fmt.Errorf("resourceFormat[%s]: not PNG", item.IconReference)
		}
		sourcePath := filepath.Join(temporaryPath, filename+".png")
		if err = os.WriteFile(sourcePath, payload, 0o644); err != nil {
			return converted, missing, fmt.Errorf("temporaryWrite[%s]: %w", item.IconReference, err)
		}
		command := exec.CommandContext(ctx, magickPath, sourcePath, "-strip", "-quality", fmt.Sprint(quality), "-define", "webp:method=6", targetPath)
		commandOutput, commandErr := command.CombinedOutput()
		_ = os.Remove(sourcePath)
		if commandErr != nil {
			return converted, missing, fmt.Errorf("webpConvert[%s]: %w: %s", item.IconReference, commandErr, strings.TrimSpace(string(commandOutput)))
		}
		converted++
	}
	if err = pruneGeneratedWebP(outputPath, wanted); err != nil {
		return converted, missing, fmt.Errorf("outputPrune: %w", err)
	}
	return converted, missing, nil
}

func generateAbilityIcons(ctx context.Context, magickPath, packagePath, dataPath, outputPath, temporaryPath string, quality int) (int, int, error) {
	contents, err := os.ReadFile(dataPath)
	if err != nil {
		return 0, 0, fmt.Errorf("catalogRead: %w", err)
	}
	var abilityData abilityAssetCatalog
	if err = json.Unmarshal(contents, &abilityData); err != nil {
		return 0, 0, fmt.Errorf("catalogDecode: %w", err)
	}
	r, err := os.Open(packagePath)
	if err != nil {
		return 0, 0, fmt.Errorf("packageOpen: %w", err)
	}
	defer r.Close()
	fi, err := r.Stat()
	if err != nil {
		return 0, 0, fmt.Errorf("packageStat: %w", err)
	}
	pkg, err := dbpf.NewReader(r, fi.Size())
	if err != nil {
		return 0, 0, fmt.Errorf("packageIndex: %w", err)
	}
	resources := make(map[uint32]dbpf.Entry)
	for _, entry := range pkg.Entries {
		if entry.Type == pngType && entry.Group == abilityIconGroup && entry.Instance <= uint64(^uint32(0)) {
			resources[uint32(entry.Instance)] = entry
		}
	}
	if err = os.MkdirAll(outputPath, 0o755); err != nil {
		return 0, 0, fmt.Errorf("outputCreate: %w", err)
	}
	wanted := make(map[string]struct{})
	converted, missing := 0, 0
	for _, ability := range abilityData.Items {
		if ability.IconReference == "" || ability.IconPath == "" {
			missing++
			continue
		}
		filename := filepath.Base(ability.IconPath)
		wanted[filename] = struct{}{}
		targetPath := filepath.Join(outputPath, filename)
		if existing, statErr := os.Stat(targetPath); statErr == nil && existing.Size() > 0 {
			converted++
			continue
		}
		entry, isFound := resources[catalog.HashID(ability.IconReference)]
		if !isFound {
			missing++
			continue
		}
		payloadReader, openErr := pkg.Open(entry)
		if openErr != nil {
			return converted, missing, fmt.Errorf("resourceOpen[%s]: %w", ability.IconReference, openErr)
		}
		payload, readErr := io.ReadAll(payloadReader)
		if readErr != nil {
			return converted, missing, fmt.Errorf("resourceRead[%s]: %w", ability.IconReference, readErr)
		}
		if len(payload) < 8 || string(payload[:8]) != "\x89PNG\r\n\x1a\n" {
			return converted, missing, fmt.Errorf("resourceFormat[%s]: not PNG", ability.IconReference)
		}
		sourcePath := filepath.Join(temporaryPath, filename+".png")
		if err = os.WriteFile(sourcePath, payload, 0o644); err != nil {
			return converted, missing, fmt.Errorf("temporaryWrite[%s]: %w", ability.IconReference, err)
		}
		command := exec.CommandContext(ctx, magickPath, sourcePath, "-strip", "-quality", fmt.Sprint(quality), "-define", "webp:method=6", targetPath)
		commandOutput, commandErr := command.CombinedOutput()
		_ = os.Remove(sourcePath)
		if commandErr != nil {
			return converted, missing, fmt.Errorf("webpConvert[%s]: %w: %s", ability.IconReference, commandErr, strings.TrimSpace(string(commandOutput)))
		}
		converted++
	}
	if err = pruneGeneratedWebP(outputPath, wanted); err != nil {
		return converted, missing, fmt.Errorf("outputPrune: %w", err)
	}
	return converted, missing, nil
}

func loadCanonicalHeroIDs(dataPath string) ([]string, error) {
	contents, err := os.ReadFile(dataPath)
	if err != nil {
		return nil, fmt.Errorf("heroCatalogRead: %w", err)
	}
	var heroData heroAssetCatalog
	if err = json.Unmarshal(contents, &heroData); err != nil {
		return nil, fmt.Errorf("heroCatalogDecode: %w", err)
	}
	ids := make([]string, 0, len(heroData.Families))
	for _, family := range heroData.Families {
		if len(family.Variants) == 0 {
			continue
		}
		ids = append(ids, fmt.Sprint(family.Variants[len(family.Variants)-1].ID))
	}
	if len(ids) == 0 {
		return nil, errors.New("hero catalog has no canonical variants")
	}
	sort.Strings(ids)
	return ids, nil
}

func pruneGeneratedWebP(outputPath string, wanted map[string]struct{}) error {
	entries, err := os.ReadDir(outputPath)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".webp" {
			continue
		}
		if _, keep := wanted[entry.Name()]; keep {
			continue
		}
		if err = os.Remove(filepath.Join(outputPath, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func generateItemIcons(ctx context.Context, magickPath, packagePath, dataPath, outputPath, temporaryPath string, quality int) (int, int, error) {
	contents, err := os.ReadFile(dataPath)
	if err != nil {
		return 0, 0, fmt.Errorf("catalogRead: %w", err)
	}
	var itemData itemAssetCatalog
	if err = json.Unmarshal(contents, &itemData); err != nil {
		return 0, 0, fmt.Errorf("catalogDecode: %w", err)
	}
	source, err := os.Open(packagePath)
	if err != nil {
		return 0, 0, fmt.Errorf("packageOpen: %w", err)
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return 0, 0, fmt.Errorf("packageStat: %w", err)
	}
	reader, err := dbpf.NewReader(source, info.Size())
	if err != nil {
		return 0, 0, fmt.Errorf("packageIndex: %w", err)
	}
	entries := make(map[uint32]dbpf.Entry)
	for _, entry := range reader.Entries {
		if entry.Type == pngType && entry.Group == itemIconGroup && entry.Instance <= uint64(^uint32(0)) {
			entries[uint32(entry.Instance)] = entry
		}
	}
	references := make(map[string]string)
	for _, family := range itemData.Items {
		for _, variant := range family.Variants {
			parts := strings.SplitN(variant.IconReference, "!", 2)
			if len(parts) != 2 || variant.IconPath == "" {
				continue
			}
			name := strings.TrimSuffix(parts[1], filepath.Ext(parts[1]))
			references[name] = filepath.Base(variant.IconPath)
		}
	}
	names := make([]string, 0, len(references))
	for name := range references {
		names = append(names, name)
	}
	sort.Strings(names)
	if err = os.MkdirAll(outputPath, 0o755); err != nil {
		return 0, 0, fmt.Errorf("outputCreate: %w", err)
	}
	converted := 0
	missing := 0
	for _, name := range names {
		targetPath := filepath.Join(outputPath, references[name])
		if existing, statErr := os.Stat(targetPath); statErr == nil && existing.Size() > 0 {
			converted++
			continue
		}
		entry, exists := entries[catalog.HashID(name)]
		if !exists {
			missing++
			continue
		}
		payload, openErr := reader.Open(entry)
		if openErr != nil {
			return converted, missing, fmt.Errorf("resourceOpen[%s]: %w", name, openErr)
		}
		png, readErr := io.ReadAll(payload)
		if readErr != nil {
			return converted, missing, fmt.Errorf("resourceRead[%s]: %w", name, readErr)
		}
		if len(png) < 8 || string(png[:8]) != "\x89PNG\r\n\x1a\n" {
			return converted, missing, fmt.Errorf("resourceFormat[%s]: not PNG", name)
		}
		sourcePath := filepath.Join(temporaryPath, references[name]+".png")
		if writeErr := os.WriteFile(sourcePath, png, 0o644); writeErr != nil {
			return converted, missing, fmt.Errorf("temporaryWrite[%s]: %w", name, writeErr)
		}
		command := exec.CommandContext(ctx, magickPath, sourcePath, "-strip", "-quality", fmt.Sprint(quality), "-define", "webp:method=6", targetPath)
		commandOutput, commandErr := command.CombinedOutput()
		_ = os.Remove(sourcePath)
		if commandErr != nil {
			return converted, missing, fmt.Errorf("webpConvert[%s]: %w: %s", name, commandErr, strings.TrimSpace(string(commandOutput)))
		}
		converted++
	}
	if converted == 0 {
		return 0, missing, errors.New("no referenced item icons found")
	}
	return converted, missing, nil
}
