package main

import (
	"database/sql"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/darkspinnet/darkspin/content/dbpf"
	"github.com/darkspinnet/web/scripts/internal/catalog"
)

const npcPortraitGroup = uint32(0x40626200)

// Nouns store an assembled-creature asset ID, not an animation or filename hash.
// PreBaked's version-13 metadata links that ID to the portrait's resource key.
// Animation rigs are shared and must never be used as an appearance fallback.
func enrichNPCImages(database *sql.DB, result *npcCatalog, gamePath string) error {
	source, err := os.Open(filepath.Join(gamePath, "Data", "AssetData_Binary.package"))
	if err != nil {
		return err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return err
	}
	assets, err := dbpf.NewReader(source, info.Size())
	if err != nil {
		return err
	}
	sourcePNG, err := os.Open(filepath.Join(gamePath, "Data", "PreBaked.package"))
	if err != nil {
		return err
	}
	defer sourcePNG.Close()
	info, err = sourcePNG.Stat()
	if err != nil {
		return err
	}
	baked, err := dbpf.NewReader(sourcePNG, info.Size())
	if err != nil {
		return err
	}
	portraits := map[uint64]dbpf.Entry{}
	for _, entry := range baked.Entries {
		if entry.Type == 0x2f7d0004 && entry.Group == npcPortraitGroup {
			portraits[entry.Instance] = entry
		}
	}
	byAsset := map[uint64]dbpf.Entry{}
	for _, entry := range baked.Entries {
		if entry.Type != 0x030bdee3 || entry.Group != npcPortraitGroup {
			continue
		}
		payload, err := readNPCAsset(baked, entry)
		if err != nil {
			return err
		}
		assetID, ok := npcPortraitAssetID(payload, entry.Group, entry.Instance)
		if !ok {
			continue
		}
		portrait, ok := portraits[entry.Instance]
		if !ok {
			continue
		}
		if previous, exists := byAsset[assetID]; exists && previous.Instance != portrait.Instance {
			return fmt.Errorf("ambiguous NPC portrait asset %016x", assetID)
		}
		byAsset[assetID] = portrait
	}
	// Do not preserve images from older heuristic generations or slug aliases.
	for slug, detail := range result.Entry {
		detail.Image = ""
		detail.ImageModel = ""
		detail.ImageAssetID = 0
		result.Entry[slug] = detail
	}
	rows, err := database.Query(`SELECT n.noun_name,r.ordinal,r.instance_id FROM non_player_noun n JOIN content_source_resource r ON r.id=n.content_source_resource_id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var noun string
		var ordinal int
		var instance uint64
		if err = rows.Scan(&noun, &ordinal, &instance); err != nil {
			return err
		}
		slug := catalog.Slug(noun)
		detail, ok := result.Entry[slug]
		if !ok {
			continue
		}
		if ordinal < 0 || ordinal >= len(assets.Entries) || assets.Entries[ordinal].Instance != instance || assets.Entries[ordinal].Type != 0x76a8f7d8 {
			return fmt.Errorf("noun package identity mismatch: %s", noun)
		}
		payload, err := readNPCAsset(assets, assets.Entries[ordinal])
		if err != nil {
			return err
		}
		assetID, ok := npcCreatureAssetID(payload)
		if !ok {
			continue
		}
		entry, ok := byAsset[assetID]
		if !ok {
			continue
		}
		payload, err = readNPCAsset(baked, entry)
		if err != nil {
			return err
		}
		if len(payload) < 8 || string(payload[:8]) != "\x89PNG\r\n\x1a\n" {
			return fmt.Errorf("NPC portrait is not PNG: %s", noun)
		}
		output := filepath.Join("static", "image", "npc", fmt.Sprintf("%08x.png", entry.Instance))
		if err = os.MkdirAll(filepath.Dir(output), 0755); err != nil {
			return err
		}
		if err = os.WriteFile(output, payload, 0644); err != nil {
			return err
		}
		detail.Image = fmt.Sprintf("/image/npc/%08x.png", entry.Instance)
		detail.ImageModel = fmt.Sprintf("%08x:%08x", entry.Group, entry.Instance)
		detail.ImageAssetID = assetID
		result.Entry[slug] = detail
		count++
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for li := range result.Levels {
		for ni := range result.Levels[li].NPCs {
			entry := &result.Levels[li].NPCs[ni]
			entry.Image = result.Entry[entry.Slug].Image
		}
	}
	fmt.Printf("npc portraits: %d/%d nouns linked by authored creature asset ID\n", count, len(result.Entry))
	return nil
}

func npcCreatureAssetID(payload []byte) (uint64, bool) {
	if len(payload) < 160 {
		return 0, false
	}
	id := binary.LittleEndian.Uint64(payload[152:160])
	return id, id != 0 && id != ^uint64(0)
}

func npcPortraitAssetID(payload []byte, group uint32, instance uint64) (uint64, bool) {
	if len(payload) < 24 || binary.BigEndian.Uint32(payload[:4]) != 13 || instance > uint64(^uint32(0)) {
		return 0, false
	}
	if binary.BigEndian.Uint32(payload[16:20]) != group || uint64(binary.BigEndian.Uint32(payload[20:24])) != instance {
		return 0, false
	}
	id := binary.BigEndian.Uint64(payload[4:12])
	return id, id != 0 && id != ^uint64(0)
}

func readNPCAsset(reader *dbpf.Reader, entry dbpf.Entry) ([]byte, error) {
	payload, err := reader.Open(entry)
	if err != nil {
		return nil, err
	}
	return io.ReadAll(payload)
}
