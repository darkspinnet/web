package main

import (
	"bytes"
	"compress/zlib"
	"database/sql"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
)

func loadLevelTitles(database *sql.DB) (map[int64]string, error) {
	textRows, err := database.Query(`SELECT locale_key, localized_text FROM localization_text
		WHERE locale='en-us' AND table_id=3532538498`)
	if err != nil {
		return nil, err
	}
	translations := make(map[string]string)
	for textRows.Next() {
		var key, name string
		if err = textRows.Scan(&key, &name); err != nil {
			textRows.Close()
			return nil, err
		}
		translations[key] = strings.TrimSpace(name)
	}
	textRows.Close()
	if err = textRows.Err(); err != nil {
		return nil, err
	}
	rows, err := database.Query("SELECT id, source_payload FROM level")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	titles := make(map[int64]string)
	for rows.Next() {
		var id int64
		var compressed []byte
		if err = rows.Scan(&id, &compressed); err != nil {
			return nil, err
		}
		reader, err := zlib.NewReader(bytes.NewReader(compressed))
		if err != nil {
			return nil, fmt.Errorf("levelTitle[%d]: %w", id, err)
		}
		// The build 103 Level header stores the display text key at 0x68.
		var header [0x6c]byte
		_, err = io.ReadFull(reader, header[:])
		reader.Close()
		if err != nil {
			return nil, fmt.Errorf("levelTitle[%d]: %w", id, err)
		}
		key := binary.LittleEndian.Uint32(header[0x68:])
		titles[id] = translations[fmt.Sprintf("0x%08x", key)]
	}
	return titles, rows.Err()
}
