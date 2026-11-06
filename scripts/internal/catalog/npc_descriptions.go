package catalog

import (
	"bytes"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/darkspinnet/darkspin/content/dbpf"
)

const npcAssetStringsTable = 1882572231

// LoadNPCDescriptions reads the authored tooltip paragraphs by NonPlayerClass
// instance ID. A database-only build can still use the imported short text.
func LoadNPCDescriptions(database *sql.DB, gamePath string) (map[uint32][]string, error) {
	file, err := os.Open(filepath.Join(gamePath, "Data", "AssetData_Binary.package"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	pkg, err := dbpf.NewReader(file, info.Size())
	if err != nil {
		return nil, err
	}
	rows, err := database.Query(`SELECT locale_key, localized_text FROM localization_text
		WHERE locale='en-us' AND table_id=?`, npcAssetStringsTable)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	translations := make(map[string]string)
	for rows.Next() {
		var key, description string
		if err = rows.Scan(&key, &description); err != nil {
			return nil, err
		}
		translations[key] = strings.TrimSpace(description)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}

	descriptions := make(map[uint32][]string)
	for _, entry := range pkg.Entries {
		if entry.Type != 0xd117afca || entry.Group != 0 {
			continue
		}
		reader, err := pkg.Open(entry)
		if err != nil {
			return nil, fmt.Errorf("NPC class %08x: %w", entry.Instance, err)
		}
		payload, err := io.ReadAll(reader)
		if err != nil {
			return nil, fmt.Errorf("NPC class %08x: %w", entry.Instance, err)
		}
		paragraphs, err := decodeNPCDescriptions(payload, translations)
		if err != nil {
			return nil, fmt.Errorf("NPC class %08x: %w", entry.Instance, err)
		}
		if len(paragraphs) > 0 {
			descriptions[uint32(entry.Instance)] = paragraphs
		}
	}
	return descriptions, nil
}

func decodeNPCDescriptions(payload []byte, translations map[string]string) ([]string, error) {
	const headerSize, descriptionSize = 0x7c, 20
	if len(payload) < headerSize {
		return nil, errors.New("truncated NPC class header")
	}
	count := binary.LittleEndian.Uint32(payload[108:])
	if count == 0 {
		return nil, nil
	}
	tail := payload[headerSize:]
	readReference := func(field []byte) (string, error) {
		if binary.LittleEndian.Uint32(field) == 0 {
			return "", nil
		}
		end := bytes.IndexByte(tail, 0)
		if end < 0 {
			return "", errors.New("unterminated NPC class reference")
		}
		value := string(tail[:end])
		tail = tail[end+1:]
		return value, nil
	}
	// The packed tail holds the header's references, the drop IDs, then all
	// fixed description records followed by each record's text references.
	for _, offset := range []int{16, 20, 12, 8, 80, 84} {
		if _, err := readReference(payload[offset:]); err != nil {
			return nil, err
		}
	}
	drops := binary.LittleEndian.Uint32(payload[44:])
	if uint64(drops)*4+uint64(count)*descriptionSize > uint64(len(tail)) {
		return nil, errors.New("truncated NPC class description records")
	}
	records := tail[int(drops)*4:][:int(count)*descriptionSize]
	tail = tail[int(drops)*4+len(records):]
	var paragraphs []string
	for offset := 0; offset < len(records); offset += descriptionSize {
		record := records[offset : offset+descriptionSize]
		inline, err := readReference(record)
		if err != nil {
			return nil, err
		}
		if _, err = readReference(record[4:]); err != nil {
			return nil, err
		}
		paragraph := strings.TrimSpace(inline)
		if binary.LittleEndian.Uint32(record[16:]) == npcAssetStringsTable {
			key := fmt.Sprintf("0x%08x", binary.LittleEndian.Uint32(record[8:]))
			if localized := translations[key]; localized != "" {
				paragraph = localized
			}
		}
		if paragraph != "" {
			paragraphs = append(paragraphs, paragraph)
		}
	}
	return paragraphs, nil
}
