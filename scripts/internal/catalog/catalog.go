package catalog

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	_ "modernc.org/sqlite"
)

var publicTrademarkPattern = regexp.MustCompile(`(?i)darkspore`)

const (
	DefaultDatabasePath = `d:\src\darkspin\bin\darkspinner\darkspin\cache\content.db`
	DefaultGamePath     = `d:\src\darkspin\bin\darkspinner`
)

func Open(databasePath string) (*sql.DB, error) {
	if databasePath == "" {
		return nil, errors.New("empty database path")
	}
	absolutePath, err := filepath.Abs(databasePath)
	if err != nil {
		return nil, fmt.Errorf("databasePath: %w", err)
	}
	_, err = os.Stat(absolutePath)
	if err != nil {
		return nil, fmt.Errorf("databaseStat: %w", err)
	}
	database, err := sql.Open("sqlite", "file:"+filepath.ToSlash(absolutePath)+"?mode=ro&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("databaseOpen: %w", err)
	}
	// Level reports stream the base catalog while resolving small related lists.
	// Multiple read-only connections keep those nested lookups from waiting on
	// the active row iterator.
	database.SetMaxOpenConns(4)
	database.SetMaxIdleConns(4)
	err = database.Ping()
	if err != nil {
		_ = database.Close()
		return nil, fmt.Errorf("databasePing: %w", err)
	}
	return database, nil
}

func WriteJSON(path string, payload any) error {
	contents, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("jsonMarshal: %w", err)
	}
	return writeContents(path, append(contents, '\n'))
}

func WriteCompactJSON(path string, payload any) error {
	contents, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("jsonMarshal: %w", err)
	}
	return writeContents(path, append(contents, '\n'))
}

func WriteText(path, contents string) error {
	return writeContents(path, []byte(contents))
}

func writeContents(path string, contents []byte) error {
	contents = publicTrademarkPattern.ReplaceAll(contents, []byte("corrupted"))
	err := os.MkdirAll(filepath.Dir(path), 0o755)
	if err != nil {
		return fmt.Errorf("outputMkdir: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".catalog-*.json")
	if err != nil {
		return fmt.Errorf("outputCreate: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	_, err = temporary.Write(contents)
	if err != nil {
		_ = temporary.Close()
		return fmt.Errorf("outputWrite: %w", err)
	}
	err = temporary.Close()
	if err != nil {
		return fmt.Errorf("outputClose: %w", err)
	}
	err = os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("outputReplace: %w", err)
	}
	err = os.Rename(temporaryPath, path)
	if err != nil {
		return fmt.Errorf("outputInstall: %w", err)
	}
	return nil
}

func HashID(name string) uint32 {
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

func DisplayName(assetName string) string {
	assetName = publicTrademarkPattern.ReplaceAllString(assetName, "corrupted")
	name := strings.TrimSuffix(assetName, ".Noun")
	name = strings.ReplaceAll(name, "_", " ")
	var result strings.Builder
	for index, character := range name {
		if index > 0 && character >= 'A' && character <= 'Z' {
			previous := rune(name[index-1])
			if previous >= 'a' && previous <= 'z' {
				result.WriteRune(' ')
			}
		}
		result.WriteRune(character)
	}
	return strings.TrimSpace(result.String())
}

func Slug(name string) string {
	name = publicTrademarkPattern.ReplaceAllString(name, "corrupted")
	name = strings.ToLower(name)
	var result strings.Builder
	isDash := false
	for _, character := range name {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') {
			result.WriteRune(character)
			isDash = false
			continue
		}
		if result.Len() > 0 && !isDash {
			result.WriteRune('-')
			isDash = true
		}
	}
	return strings.Trim(result.String(), "-")
}
