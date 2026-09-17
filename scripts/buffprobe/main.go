// Command buffprobe locates buff and modifier identifiers inside decoded DBPF resources.
package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	"github.com/darkspinnet/darkspin/content/dbpf"
	"github.com/darkspinnet/web/scripts/internal/catalog"
)

func main() {
	packagePath := flag.String("package", `d:\src\darkspin\bin\darkspinner\Data\AssetData_Binary.package`, "DBPF package path")
	match := flag.String("match", "status_poisoned", "case-insensitive decoded payload substring")
	references := flag.Bool("references", false, "search for the little-endian 32-bit hash of match")
	flag.Parse()
	if err := run(*packagePath, *match, *references); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(packagePath, match string, references bool) error {
	source, err := os.Open(packagePath)
	if err != nil {
		return err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return err
	}
	reader, err := dbpf.NewReader(source, info.Size())
	if err != nil {
		return err
	}
	needle := bytes.ToLower([]byte(match))
	if references {
		needle = make([]byte, 4)
		binary.LittleEndian.PutUint32(needle, catalog.HashID(match))
		fmt.Printf("reference hash: 0x%08x (% x)\n", catalog.HashID(match), needle)
	}
	found := 0
	for ordinal, entry := range reader.Entries {
		payloadReader, openErr := reader.Open(entry)
		if openErr != nil {
			continue
		}
		payload, readErr := io.ReadAll(payloadReader)
		haystack := bytes.ToLower(payload)
		if references {
			haystack = payload
		}
		if readErr != nil || !bytes.Contains(haystack, needle) {
			continue
		}
		found++
		fmt.Printf("ordinal=%d type=0x%08x group=0x%08x instance=0x%016x size=%d\n", ordinal, entry.Type, entry.Group, entry.Instance, len(payload))
		if len(payload) <= 4096 {
			fmt.Print(hex.Dump(payload))
		}
		if len(payload) > 64*1024 {
			continue
		}
		for _, text := range printableStrings(payload, 4) {
			fmt.Printf("  %s\n", text)
		}
	}
	if found == 0 {
		return fmt.Errorf("no decoded resources contain %q", match)
	}
	return nil
}

func printableStrings(payload []byte, minimum int) []string {
	results := make([]string, 0)
	var current strings.Builder
	flush := func() {
		if current.Len() >= minimum {
			results = append(results, current.String())
		}
		current.Reset()
	}
	for _, value := range payload {
		if value >= 0x20 && value <= 0x7e && (unicode.IsPrint(rune(value)) || value == '\t') {
			current.WriteByte(value)
			continue
		}
		flush()
	}
	flush()
	return results
}
