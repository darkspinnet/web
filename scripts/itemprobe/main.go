package main

import (
	"bytes"
	"compress/zlib"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/darkspinnet/darkspin/content/dbpf"
	"github.com/darkspinnet/darkspin/content/lua51"
	"github.com/darkspinnet/web/scripts/internal/catalog"
)

func main() {
	prefixID := flag.Int("prefix", 0, "inspect one generated LootPrefix record and exit")
	suffixID := flag.Int("suffix", 0, "inspect one generated LootSuffix record and exit")
	rigID := flag.Int("rig", 0, "inspect one generated LootRigblock record and exit")
	localeText := flag.String("locale", "", "find English localization rows containing this text and exit")
	tableName := flag.String("table", "", "print one SQLite table definition and exit")
	assetName := flag.String("asset", "", "inspect resources matching one hashed asset name and exit")
	assetReferences := flag.String("asset-refs", "", "scan package payloads for references to one hashed asset name and exit")
	luaChunkID := flag.Int64("lua-chunk", 0, "disassemble one indexed Lua chunk and exit")
	instanceID := flag.Uint("instance", 0, "inspect resources matching one instance ID and exit")
	tuningCandidates := flag.Bool("tuning-candidates", false, "list singleton AssetData resources that may contain loot tuning")
	flag.Parse()
	database, err := catalog.Open(catalog.DefaultDatabasePath)
	if err != nil {
		fatal(err)
	}
	defer database.Close()
	if *tableName != "" {
		if err := inspectTable(database, *tableName); err != nil {
			fatal(err)
		}
		return
	}
	if *assetName != "" {
		if err := inspectAsset(database, *assetName); err != nil {
			fatal(err)
		}
		return
	}
	if *assetReferences != "" {
		if err := inspectPackageReferences(database, *assetReferences); err != nil {
			fatal(err)
		}
		return
	}
	if *luaChunkID > 0 {
		if err := inspectLuaChunk(database, *luaChunkID); err != nil {
			fatal(err)
		}
		return
	}
	if *instanceID != 0 {
		if err := inspectInstance(database, uint32(*instanceID)); err != nil {
			fatal(err)
		}
		return
	}
	if *tuningCandidates {
		if err := inspectTuningCandidates(database); err != nil {
			fatal(err)
		}
		return
	}
	if *localeText != "" {
		if err := inspectLocale(database, *localeText); err != nil {
			fatal(err)
		}
		return
	}
	if *rigID > 0 {
		if err := inspectAffix(database, *rigID, 0x1bced3d7, "rig"); err != nil {
			fatal(err)
		}
		return
	}
	if *prefixID > 0 {
		if err := inspectAffix(database, *prefixID, 0x6a1812c6, "prefix"); err != nil {
			fatal(err)
		}
		return
	}
	if *suffixID > 0 {
		if err := inspectAffix(database, *suffixID, 0x447dc2e5, "suffix"); err != nil {
			fatal(err)
		}
		return
	}

	fmt.Println("RESOURCE GROUPS")
	rows, err := database.Query(`
		SELECT resource_group, COUNT(*), SUM(decoded_size), SUM(is_compiled_lua)
		FROM server_data GROUP BY resource_group ORDER BY COUNT(*) DESC`)
	if err != nil {
		fatal(err)
	}
	for rows.Next() {
		var group string
		var count, size, lua int64
		if err = rows.Scan(&group, &count, &size, &lua); err != nil {
			fatal(err)
		}
		fmt.Printf("%s\t%d\t%d\t%d lua\n", group, count, size, lua)
	}
	_ = rows.Close()

	fmt.Println("\nNAMED CANDIDATES")
	rows, err = database.Query(`
		SELECT resource_group, resource_name, format, decoded_size, is_compiled_lua
		FROM server_data
		WHERE lower(resource_group || '/' || resource_name) LIKE '%item%'
		   OR lower(resource_group || '/' || resource_name) LIKE '%loot%'
		   OR lower(resource_group || '/' || resource_name) LIKE '%drop%'
		   OR lower(resource_group || '/' || resource_name) LIKE '%gear%'
		   OR lower(resource_group || '/' || resource_name) LIKE '%weapon%'
		   OR lower(resource_group || '/' || resource_name) LIKE '%armor%'
		ORDER BY resource_group, resource_name LIMIT 1000`)
	if err != nil {
		fatal(err)
	}
	for rows.Next() {
		var group, name, format string
		var size int64
		var lua bool
		if err = rows.Scan(&group, &name, &format, &size, &lua); err != nil {
			fatal(err)
		}
		fmt.Printf("%s/%s\t%s\t%d\tlua=%t\n", group, name, format, size, lua)
	}
	_ = rows.Close()

	fmt.Println("\nITEM ICON SAMPLE")
	iconName := "ce_grasper_fireRavager_01-symmetric.png"
	iconID := catalog.HashID(iconName)
	fmt.Printf("icon reference %s hash=0x%08x\n", iconName, iconID)
	for _, candidate := range []string{
		strings.TrimSuffix(iconName, ".png"),
		"0x100d977c!" + iconName,
		"creature_editor/" + iconName,
		"editor_rigblock/" + iconName,
	} {
		fmt.Printf("icon candidate %s hash=0x%08x\n", candidate, catalog.HashID(candidate))
	}
	rows, err = database.Query(`
		SELECT content_source_package.package_name, content_source_resource.type_id,
		       content_source_resource.group_id, content_source_resource.instance_id,
		       content_source_resource.decoded_size, content_source_resource.compression
		FROM content_source_resource
		JOIN content_source_package ON content_source_package.id=content_source_resource.content_source_package_id
		WHERE content_source_resource.instance_id=?`, iconID)
	if err != nil {
		fatal(err)
	}
	for rows.Next() {
		var packageName string
		var typeID, groupID, instanceID, size uint32
		var compression uint16
		if err = rows.Scan(&packageName, &typeID, &groupID, &instanceID, &size, &compression); err != nil {
			fatal(err)
		}
		fmt.Printf("%s hash=0x%08x package=%s type=0x%08x group=0x%08x size=%d compression=0x%04x\n", iconName, iconID, packageName, typeID, groupID, size, compression)
	}
	_ = rows.Close()
	rows, err = database.Query(`
		SELECT content_source_package.package_name, content_source_resource.type_id,
		       COUNT(*), MIN(content_source_resource.decoded_size), MAX(content_source_resource.decoded_size)
		FROM content_source_resource
		JOIN content_source_package ON content_source_package.id=content_source_resource.content_source_package_id
		WHERE content_source_resource.group_id=0x100d977c
		GROUP BY content_source_package.package_name, content_source_resource.type_id`)
	if err != nil {
		fatal(err)
	}
	for rows.Next() {
		var packageName string
		var typeID uint32
		var count, minimum, maximum int64
		if err = rows.Scan(&packageName, &typeID, &count, &minimum, &maximum); err != nil {
			fatal(err)
		}
		fmt.Printf("group inventory package=%s type=0x%08x count=%d size=%d..%d\n", packageName, typeID, count, minimum, maximum)
	}
	_ = rows.Close()

	fmt.Println("\nPACKAGES")
	rows, err = database.Query(`
		SELECT package_name, resource_count, file_size FROM content_source_package
		WHERE lower(package_name) LIKE '%item%'
		   OR lower(package_name) LIKE '%loot%'
		   OR lower(package_name) LIKE '%gear%'
		ORDER BY package_name`)
	if err != nil {
		fatal(err)
	}
	for rows.Next() {
		var name string
		var count, size int64
		if err = rows.Scan(&name, &count, &size); err != nil {
			fatal(err)
		}
		fmt.Printf("%s\t%d\t%d\n", name, count, size)
	}
	_ = rows.Close()

	fmt.Println("\nASSET DATA TYPES")
	rows, err = database.Query(`
		SELECT type_id, COUNT(*), MIN(decoded_size), MAX(decoded_size), CAST(AVG(decoded_size) AS INTEGER)
		FROM content_source_resource WHERE content_source_package_id=1
		GROUP BY type_id ORDER BY COUNT(*) DESC`)
	if err != nil {
		fatal(err)
	}
	for rows.Next() {
		var typeID uint32
		var count, minimum, maximum, average int64
		if err = rows.Scan(&typeID, &count, &minimum, &maximum, &average); err != nil {
			fatal(err)
		}
		fmt.Printf("0x%08x\t%d\tmin=%d\tmax=%d\tavg=%d\n", typeID, count, minimum, maximum, average)
	}
	_ = rows.Close()

	fmt.Println("\nKNOWN GENERATED ASSETS")
	names := []string{
		"_Generated/LootRigblock1.LootRigblock",
		"_Generated/LootRigblock268.LootRigblock",
		"_Generated/LootRigblock1573.LootRigblock",
		"_Generated/LootRigblock10001.LootRigblock",
		"_Generated/LootPrefix1.LootPrefix",
		"_Generated/LootSuffix1.LootSuffix",
	}
	for _, name := range names {
		instanceID := catalog.HashID(name)
		var typeID, groupID uint32
		var size int64
		var payload []byte
		err = database.QueryRow(`
			SELECT type_id, group_id, decoded_size, raw_payload
			FROM content_source_resource WHERE content_source_package_id=1 AND instance_id=?`, instanceID).
			Scan(&typeID, &groupID, &size, &payload)
		if err != nil {
			fmt.Printf("%s hash=0x%08x missing: %v\n", name, instanceID, err)
			continue
		}
		prefix := payload
		if len(prefix) > 96 {
			prefix = prefix[:96]
		}
		fmt.Printf("%s hash=0x%08x type=0x%08x group=0x%08x size=%d prefix=%s\n", name, instanceID, typeID, groupID, size, hex.EncodeToString(prefix))
	}

	fmt.Println("\nITEM TYPE SAMPLES")
	itemTypes := []uint32{0x1bced3d7, 0x6a1812c6, 0xd117afca, 0x447dc2e5, 0xeeeb0e31}
	for _, wantedType := range itemTypes {
		fmt.Printf("TYPE 0x%08x\n", wantedType)
		rows, err = database.Query(`
			SELECT ordinal, group_id, instance_id, stored_size, decoded_size, compression, raw_payload
			FROM content_source_resource
			WHERE content_source_package_id=1 AND type_id=?
			ORDER BY ordinal LIMIT 8`, wantedType)
		if err != nil {
			fatal(err)
		}
		for rows.Next() {
			var ordinal int
			var groupID, instanceID uint32
			var storedSize, size int64
			var compression uint16
			var payload []byte
			if err = rows.Scan(&ordinal, &groupID, &instanceID, &storedSize, &size, &compression, &payload); err != nil {
				fatal(err)
			}
			decoded, decodeErr := dbpf.Decode(dbpf.Entry{StoredSize: uint32(storedSize), Size: uint32(size), Compression: compression}, payload)
			if decodeErr != nil {
				fatal(decodeErr)
			}
			prefix := decoded
			if len(prefix) > 128 {
				prefix = prefix[:128]
			}
			fmt.Printf("  ord=%d group=0x%08x instance=0x%08x size=%d prefix=%s strings=%q\n", ordinal, groupID, instanceID, size, hex.EncodeToString(prefix), printableStrings(decoded))
		}
		_ = rows.Close()
	}

	for _, wantedType := range []uint32{0x1bced3d7, 0x6a1812c6, 0x447dc2e5} {
		if err := summarizeItemType(database, wantedType); err != nil {
			fatal(err)
		}
	}

	fmt.Println("\nLOOT LUA STRINGS")
	rows, err = database.Query(`
		SELECT DISTINCT server_data.resource_group, lua_chunk.source_name, lua_string_constant.string_constant
		FROM lua_string_constant
		JOIN lua_chunk ON lua_chunk.id=lua_string_constant.lua_chunk_id
		JOIN server_data ON server_data.content_source_resource_id=lua_chunk.server_data_resource_id
		WHERE lower(lua_string_constant.string_constant) LIKE '%loot%'
		   OR lower(lua_string_constant.string_constant) LIKE '%drop%'
		   OR lower(lua_string_constant.string_constant) LIKE '%rigblock%'
		   OR lower(lua_string_constant.string_constant) LIKE '%prefix%'
		   OR lower(lua_string_constant.string_constant) LIKE '%suffix%'
		ORDER BY server_data.resource_group, lua_chunk.source_name, lua_string_constant.string_constant
		LIMIT 500`)
	if err != nil {
		fatal(err)
	}
	for rows.Next() {
		var group, source, value string
		if err = rows.Scan(&group, &source, &value); err != nil {
			fatal(err)
		}
		fmt.Printf("%s\t%s\t%q\n", group, source, value)
	}
	_ = rows.Close()
}

func inspectPackageReferences(database *sql.DB, name string) error {
	var rawHash, eventHash [4]byte
	rawID := catalog.HashID(name)
	eventID := catalog.HashID(name + ".ServerEventDef")
	binary.LittleEndian.PutUint32(rawHash[:], rawID)
	binary.LittleEndian.PutUint32(eventHash[:], eventID)
	rows, err := database.Query(`
		SELECT package.package_name, resource.type_id, resource.group_id, resource.instance_id,
		       resource.stored_size, resource.decoded_size, resource.compression, resource.raw_payload,
		       server_data.resource_group, server_data.resource_name, lua_chunk.id, lua_chunk.source_name
		FROM content_source_resource resource
		JOIN content_source_package package ON package.id=resource.content_source_package_id
		LEFT JOIN server_data ON server_data.content_source_resource_id=resource.id
		LEFT JOIN lua_chunk ON lua_chunk.server_data_resource_id=server_data.content_source_resource_id
		ORDER BY package.id, resource.id`)
	if err != nil {
		return fmt.Errorf("packageReferenceQuery[%s]: %w", name, err)
	}
	defer rows.Close()
	found := 0
	for rows.Next() {
		var packageName string
		var typeID, groupID, instanceID, storedSize, decodedSize uint32
		var compression uint16
		var payload []byte
		var resourceGroup, resourceName, sourceName sql.NullString
		var chunkID sql.NullInt64
		if err := rows.Scan(&packageName, &typeID, &groupID, &instanceID, &storedSize, &decodedSize, &compression, &payload, &resourceGroup, &resourceName, &chunkID, &sourceName); err != nil {
			return fmt.Errorf("packageReferenceScan[%s]: %w", name, err)
		}
		if instanceID == rawID && typeID == effectTypeID {
			continue
		}
		decoded, err := dbpf.Decode(dbpf.Entry{StoredSize: storedSize, Size: decodedSize, Compression: compression}, payload)
		if err != nil {
			return fmt.Errorf("packageReferenceDecode[%s/0x%08x]: %w", packageName, instanceID, err)
		}
		if !bytes.Contains(decoded, []byte(name)) && !bytes.Contains(decoded, rawHash[:]) && !bytes.Contains(decoded, eventHash[:]) {
			continue
		}
		found++
		values := printableStrings(decoded)
		if len(values) > 8 {
			values = values[:8]
		}
		fmt.Printf("reference package=%s type=0x%08x group=0x%08x instance=0x%08x size=%d", packageName, typeID, groupID, instanceID, len(decoded))
		if resourceGroup.Valid {
			fmt.Printf(" resource=%s/%s", resourceGroup.String, resourceName.String)
		}
		if chunkID.Valid {
			fmt.Printf(" chunk=%d source=%s", chunkID.Int64, sourceName.String)
		}
		fmt.Printf(" strings=%q\n", values)
	}
	if found == 0 {
		fmt.Printf("package references: none for %q raw=0x%08x event=0x%08x\n", name, rawID, eventID)
	}
	return rows.Err()
}

const effectTypeID = uint32(0x92ea4aac)

func inspectLuaChunk(database *sql.DB, id int64) error {
	var source string
	var compressed []byte
	if err := database.QueryRow(`
		SELECT lua_chunk.source_name, server_data.decoded_payload
		FROM lua_chunk JOIN server_data
		  ON server_data.content_source_resource_id=lua_chunk.server_data_resource_id
		WHERE lua_chunk.id=?`, id).Scan(&source, &compressed); err != nil {
		return fmt.Errorf("luaChunkQuery[%d]: %w", id, err)
	}
	reader, err := zlib.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return fmt.Errorf("luaChunkZlib[%d]: %w", id, err)
	}
	bytecode, err := io.ReadAll(reader)
	if err != nil {
		_ = reader.Close()
		return fmt.Errorf("luaChunkRead[%d]: %w", id, err)
	}
	if err := reader.Close(); err != nil {
		return fmt.Errorf("luaChunkClose[%d]: %w", id, err)
	}
	chunk, err := lua51.Inspect(bytecode)
	if err != nil {
		return fmt.Errorf("luaChunkInspect[%d]: %w", id, err)
	}
	fmt.Printf("chunk=%d source=%s bytecode=%d\n", id, source, len(bytecode))
	return lua51.WriteDisassembly(os.Stdout, chunk)
}

func inspectInstance(database *sql.DB, instanceID uint32) error {
	rows, err := database.Query(`
		SELECT content_source_package.package_name, type_id, group_id, stored_size,
		       decoded_size, compression, raw_payload
		FROM content_source_resource
		JOIN content_source_package ON content_source_package.id=content_source_resource.content_source_package_id
		WHERE instance_id=?`, instanceID)
	if err != nil {
		return fmt.Errorf("instanceQuery[0x%08x]: %w", instanceID, err)
	}
	defer rows.Close()
	for rows.Next() {
		var packageName string
		var typeID, groupID, storedSize, decodedSize uint32
		var compression uint16
		var payload []byte
		if err := rows.Scan(&packageName, &typeID, &groupID, &storedSize, &decodedSize, &compression, &payload); err != nil {
			return fmt.Errorf("instanceScan[0x%08x]: %w", instanceID, err)
		}
		decoded, err := dbpf.Decode(dbpf.Entry{StoredSize: storedSize, Size: decodedSize, Compression: compression}, payload)
		if err != nil {
			return fmt.Errorf("instanceDecode[0x%08x]: %w", instanceID, err)
		}
		fmt.Printf("instance=0x%08x package=%s type=0x%08x group=0x%08x size=%d strings=%q\n", instanceID, packageName, typeID, groupID, len(decoded), printableStrings(decoded))
		for offset := 0; offset+4 <= len(decoded); offset += 4 {
			bits := binary.LittleEndian.Uint32(decoded[offset : offset+4])
			value := math.Float32frombits(bits)
			if bits != 0 {
				fmt.Printf("%04d  0x%08x  uint=%-10d float=%v\n", offset, bits, bits, value)
			}
		}
	}
	return rows.Err()
}

func inspectTuningCandidates(database *sql.DB) error {
	rows, err := database.Query(`
		SELECT resource.type_id, resource.group_id, resource.instance_id,
		       resource.stored_size, resource.decoded_size, resource.compression, resource.raw_payload
		FROM content_source_resource resource
		JOIN content_source_package package ON package.id=resource.content_source_package_id
		JOIN (
			SELECT type_id FROM content_source_resource candidate
			JOIN content_source_package candidate_package ON candidate_package.id=candidate.content_source_package_id
			WHERE candidate_package.package_name='AssetData_Binary.package'
			GROUP BY type_id HAVING COUNT(*)=1
		) singleton ON singleton.type_id=resource.type_id
		WHERE package.package_name='AssetData_Binary.package'
		  AND resource.decoded_size BETWEEN 900 AND 20000
		ORDER BY resource.decoded_size`)
	if err != nil {
		return fmt.Errorf("tuningCandidatesQuery: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var typeID, groupID, instanceID, storedSize, decodedSize uint32
		var compression uint16
		var payload []byte
		if err := rows.Scan(&typeID, &groupID, &instanceID, &storedSize, &decodedSize, &compression, &payload); err != nil {
			return fmt.Errorf("tuningCandidatesScan: %w", err)
		}
		decoded, err := dbpf.Decode(dbpf.Entry{StoredSize: storedSize, Size: decodedSize, Compression: compression}, payload)
		if err != nil {
			return fmt.Errorf("tuningCandidateDecode[0x%08x]: %w", instanceID, err)
		}
		values := printableStrings(decoded)
		if len(values) > 10 {
			values = values[:10]
		}
		fmt.Printf("type=0x%08x group=0x%08x instance=0x%08x size=%d strings=%q\n", typeID, groupID, instanceID, len(decoded), values)
	}
	return rows.Err()
}

func inspectAsset(database *sql.DB, name string) error {
	instanceID := catalog.HashID(name)
	rows, err := database.Query(`
		SELECT content_source_package.package_name, content_source_resource.type_id,
		       content_source_resource.group_id, content_source_resource.stored_size,
		       content_source_resource.decoded_size, content_source_resource.compression,
		       content_source_resource.raw_payload
		FROM content_source_resource
		JOIN content_source_package ON content_source_package.id=content_source_resource.content_source_package_id
		WHERE content_source_resource.instance_id=?`, instanceID)
	if err != nil {
		return fmt.Errorf("assetQuery[%s]: %w", name, err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		found = true
		var packageName string
		var typeID, groupID uint32
		var storedSize, decodedSize uint32
		var compression uint16
		var payload []byte
		if err := rows.Scan(&packageName, &typeID, &groupID, &storedSize, &decodedSize, &compression, &payload); err != nil {
			return fmt.Errorf("assetScan[%s]: %w", name, err)
		}
		decoded, err := dbpf.Decode(dbpf.Entry{StoredSize: storedSize, Size: decodedSize, Compression: compression}, payload)
		if err != nil {
			return fmt.Errorf("assetDecode[%s]: %w", name, err)
		}
		fmt.Printf("asset=%q hash=0x%08x package=%s type=0x%08x group=0x%08x size=%d strings=%q\n", name, instanceID, packageName, typeID, groupID, len(decoded), printableStrings(decoded))
	}
	if !found {
		fmt.Printf("asset=%q hash=0x%08x not found\n", name, instanceID)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return inspectServerDataReferences(database, name)
}

func inspectServerDataReferences(database *sql.DB, name string) error {
	var rawHash, eventHash [4]byte
	binary.LittleEndian.PutUint32(rawHash[:], catalog.HashID(name))
	binary.LittleEndian.PutUint32(eventHash[:], catalog.HashID(name+".ServerEventDef"))
	rows, err := database.Query(`
		SELECT server_data.resource_group, server_data.resource_name, server_data.format,
		       server_data.is_compiled_lua, lua_chunk.id, lua_chunk.source_name
		FROM server_data
		LEFT JOIN lua_chunk ON lua_chunk.server_data_resource_id=server_data.content_source_resource_id
		WHERE instr(server_data.decoded_payload, ?) > 0
		   OR instr(server_data.decoded_payload, ?) > 0
		   OR instr(server_data.decoded_payload, ?) > 0
		ORDER BY server_data.resource_group, server_data.resource_name`, []byte(name), rawHash[:], eventHash[:])
	if err != nil {
		return fmt.Errorf("assetReferenceQuery[%s]: %w", name, err)
	}
	defer rows.Close()
	found := false
	for rows.Next() {
		found = true
		var group, resource, format string
		var isLua bool
		var chunkID sql.NullInt64
		var source sql.NullString
		if err := rows.Scan(&group, &resource, &format, &isLua, &chunkID, &source); err != nil {
			return fmt.Errorf("assetReferenceScan[%s]: %w", name, err)
		}
		fmt.Printf("reference group=%s resource=%s format=%s lua=%t", group, resource, format, isLua)
		if chunkID.Valid {
			fmt.Printf(" chunk=%d source=%s", chunkID.Int64, source.String)
		}
		fmt.Println()
	}
	if !found {
		fmt.Println("references: none in decoded ServerData payloads")
	}
	return rows.Err()
}

func inspectTable(database *sql.DB, name string) error {
	if name == "*" {
		rows, err := database.Query(`SELECT name FROM sqlite_master WHERE type='table' ORDER BY name`)
		if err != nil {
			return fmt.Errorf("tableList: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			var table string
			if err := rows.Scan(&table); err != nil {
				return fmt.Errorf("tableListScan: %w", err)
			}
			fmt.Println(table)
		}
		return rows.Err()
	}
	var definition string
	if err := database.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&definition); err != nil {
		return fmt.Errorf("tableDefinition[%s]: %w", name, err)
	}
	fmt.Println(definition)
	return nil
}

func inspectLocale(database *sql.DB, text string) error {
	rows, err := database.Query(`
		SELECT table_id, locale_key, localized_text FROM localization_text
		WHERE locale='en-us' AND lower(localized_text) LIKE '%' || lower(?) || '%'
		ORDER BY table_id, locale_key LIMIT 100`, text)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var tableID uint64
		var key, localized string
		if err = rows.Scan(&tableID, &key, &localized); err != nil {
			return err
		}
		fmt.Printf("table=0x%08x key=%s text=%q\n", tableID, key, localized)
	}
	return rows.Err()
}

func inspectAffix(database *sql.DB, id int, typeID uint32, kind string) error {
	rows, err := database.Query(`
		SELECT instance_id, stored_size, decoded_size, compression, raw_payload
		FROM content_source_resource
		WHERE content_source_package_id=(SELECT id FROM content_source_package WHERE package_name='AssetData_Binary.package')
		  AND type_id=?`, typeID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var instanceID uint32
	var payload []byte
	for rows.Next() {
		var candidateID, storedSize, decodedSize uint32
		var compression uint16
		var raw []byte
		if err = rows.Scan(&candidateID, &storedSize, &decodedSize, &compression, &raw); err != nil {
			return err
		}
		decoded, decodeErr := dbpf.Decode(dbpf.Entry{StoredSize: storedSize, Size: decodedSize, Compression: compression}, raw)
		if decodeErr != nil {
			return decodeErr
		}
		if len(decoded) >= 4 && binary.LittleEndian.Uint32(decoded[:4]) == uint32(id) {
			instanceID, payload = candidateID, decoded
			break
		}
	}
	if len(payload) == 0 {
		return fmt.Errorf("%s %d not found", kind, id)
	}
	fmt.Printf("%s=%d instance=0x%08x size=%d strings=%q\n", kind, id, instanceID, len(payload), printableStrings(payload))
	for offset := 0; offset+4 <= len(payload); offset += 4 {
		word := binary.LittleEndian.Uint32(payload[offset : offset+4])
		value := math.Float32frombits(word)
		if word != 0 {
			fmt.Printf("%03d  0x%08x  uint=%-10d float=%g\n", offset, word, word, value)
		}
	}
	return nil
}

type stringCount struct {
	value string
	count int
}

func summarizeItemType(database interface {
	Query(string, ...any) (*sql.Rows, error)
}, wantedType uint32) error {
	rows, err := database.Query(`
		SELECT stored_size, decoded_size, compression, raw_payload
		FROM content_source_resource WHERE content_source_package_id=1 AND type_id=?`, wantedType)
	if err != nil {
		return err
	}
	defer rows.Close()
	counts := make(map[string]int)
	ids := make([]uint32, 0)
	for rows.Next() {
		var storedSize, size uint32
		var compression uint16
		var payload []byte
		if err := rows.Scan(&storedSize, &size, &compression, &payload); err != nil {
			return err
		}
		decoded, err := dbpf.Decode(dbpf.Entry{StoredSize: storedSize, Size: size, Compression: compression}, payload)
		if err != nil {
			return err
		}
		if len(decoded) >= 4 {
			ids = append(ids, binary.LittleEndian.Uint32(decoded[:4]))
		}
		for _, value := range printableStrings(decoded) {
			if len(value) <= 48 {
				counts[value]++
			}
		}
	}
	ordered := make([]stringCount, 0, len(counts))
	for value, count := range counts {
		ordered = append(ordered, stringCount{value: value, count: count})
	}
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].count != ordered[j].count {
			return ordered[i].count > ordered[j].count
		}
		return ordered[i].value < ordered[j].value
	})
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	fmt.Printf("\nSUMMARY 0x%08x records=%d id_min=%d id_max=%d\n", wantedType, len(ids), ids[0], ids[len(ids)-1])
	for index, entry := range ordered {
		if index >= 40 || entry.count < 2 {
			break
		}
		fmt.Printf("  %d\t%q\n", entry.count, entry.value)
	}
	return rows.Err()
}

func printableStrings(data []byte) []string {
	result := make([]string, 0)
	var current strings.Builder
	flush := func() {
		if current.Len() >= 4 {
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

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
