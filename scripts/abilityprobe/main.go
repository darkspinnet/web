package main

import (
	"database/sql"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/darkspinnet/darkspin/content/dbpf"
	"github.com/darkspinnet/web/scripts/internal/catalog"
)

func main() {
	match := flag.String("match", "Tree of Life", "localized text or Lua constant substring")
	asset := flag.String("asset", "", "exact ability asset whose Lua constants should be checked against UI images")
	flag.Parse()
	database, err := catalog.Open(catalog.DefaultDatabasePath)
	if err != nil {
		fatal(err)
	}
	defer database.Close()
	if *asset != "" {
		probeIcons(database, *asset)
		return
	}

	pattern := "%" + strings.ToLower(strings.TrimSpace(*match)) + "%"
	rows, err := database.Query(`
		SELECT table_id, locale_key, localized_text
		FROM localization_text
		WHERE locale='en-us' AND lower(localized_text) LIKE ?
		ORDER BY table_id, locale_key LIMIT 100`, pattern)
	if err != nil {
		fatal(err)
	}
	fmt.Println("LOCALIZATION")
	tableIDs := make(map[uint64]struct{})
	for rows.Next() {
		var tableID uint64
		var localeKey, text string
		if err = rows.Scan(&tableID, &localeKey, &text); err != nil {
			fatal(err)
		}
		fmt.Printf("table=0x%08x key=%s text=%q\n", tableID, localeKey, text)
		tableIDs[tableID] = struct{}{}
	}
	if err = rows.Close(); err != nil {
		fatal(err)
	}
	for tableID := range tableIDs {
		rows, err = database.Query(`
			SELECT locale_key, localized_text FROM localization_text
			WHERE locale='en-us' AND table_id=? ORDER BY locale_key`, tableID)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("TABLE 0x%08x\n", tableID)
		for rows.Next() {
			var localeKey, text string
			if err = rows.Scan(&localeKey, &text); err != nil {
				fatal(err)
			}
			fmt.Printf("key=%s text=%q\n", localeKey, text)
		}
		if err = rows.Close(); err != nil {
			fatal(err)
		}
	}

	rows, err = database.Query(`
		SELECT lua_chunk.id, lua_chunk.source_name, lua_string_constant.ordinal, lua_string_constant.string_constant
		FROM lua_string_constant
		JOIN lua_chunk ON lua_chunk.id=lua_string_constant.lua_chunk_id
		WHERE lower(lua_string_constant.string_constant) LIKE ?
		ORDER BY lua_chunk.id, lua_string_constant.ordinal LIMIT 100`, pattern)
	if err != nil {
		fatal(err)
	}
	fmt.Println("LUA")
	for rows.Next() {
		var chunkID, ordinal int
		var sourceName, constant string
		if err = rows.Scan(&chunkID, &sourceName, &ordinal, &constant); err != nil {
			fatal(err)
		}
		fmt.Printf("chunk=%d source=%s ordinal=%d constant=%q\n", chunkID, sourceName, ordinal, constant)
	}
	if err = rows.Close(); err != nil {
		fatal(err)
	}
}

func probeIcons(database interface {
	Query(string, ...any) (*sql.Rows, error)
}, assetName string) {
	rows, err := database.Query(`
		SELECT lua_string_constant.string_constant
		FROM lua_string_constant
		WHERE lua_chunk_id IN (
			SELECT lua_chunk_id FROM lua_string_constant WHERE string_constant=? COLLATE NOCASE
		) ORDER BY lua_chunk_id, ordinal`, assetName)
	if err != nil {
		fatal(err)
	}
	defer rows.Close()
	candidates := make(map[uint32]string)
	constants := make([]struct {
		ordinal int
		text    string
	}, 0)
	for rows.Next() {
		var constant string
		if err = rows.Scan(&constant); err != nil {
			fatal(err)
		}
		constants = append(constants, struct {
			ordinal int
			text    string
		}{ordinal: len(constants), text: constant})
		for _, candidate := range []string{constant, constant + ".png"} {
			candidates[catalog.HashID(candidate)] = candidate
		}
	}
	for index, constant := range constants {
		if !strings.EqualFold(constant.text, assetName) {
			continue
		}
		start, end := max(0, index-12), min(len(constants), index+13)
		fmt.Printf("CONSTANTS around %q\n", assetName)
		for _, nearby := range constants[start:end] {
			fmt.Printf("  %d %q\n", nearby.ordinal, nearby.text)
		}
	}
	if err = rows.Err(); err != nil {
		fatal(err)
	}
	r, err := os.Open(filepath.Join(catalog.DefaultGamePath, "Data", "UI.package"))
	if err != nil {
		fatal(err)
	}
	defer r.Close()
	fi, err := r.Stat()
	if err != nil {
		fatal(err)
	}
	pkg, err := dbpf.NewReader(r, fi.Size())
	if err != nil {
		fatal(err)
	}
	for _, entry := range pkg.Entries {
		candidate, isFound := candidates[uint32(entry.Instance)]
		if !isFound || entry.Type != 0x2f7d0004 {
			continue
		}
		payload, readErr := io.ReadAll(mustOpen(pkg, entry))
		if readErr != nil {
			fatal(readErr)
		}
		isPNG := len(payload) >= 24 && string(payload[:8]) == "\x89PNG\r\n\x1a\n"
		width, height := uint32(0), uint32(0)
		if isPNG {
			width = binary.BigEndian.Uint32(payload[16:20])
			height = binary.BigEndian.Uint32(payload[20:24])
		}
		fmt.Printf("ICON candidate=%q group=0x%08x instance=0x%08x bytes=%d size=%dx%d png=%t\n", candidate, entry.Group, uint32(entry.Instance), len(payload), width, height, isPNG)
	}
}

func mustOpen(pkg *dbpf.Reader, entry dbpf.Entry) io.Reader {
	r, err := pkg.Open(entry)
	if err != nil {
		fatal(err)
	}
	return r
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
