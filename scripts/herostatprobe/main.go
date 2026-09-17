package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/darkspinnet/darkspin/content/dbpf"
	"github.com/darkspinnet/web/scripts/internal/catalog"
)

func main() {
	gamePath := flag.String("game", catalog.DefaultGamePath, "Darkspore install root")
	match := flag.String("match", "Sage", "case-insensitive PlayerClass text match")
	asset := flag.String("asset", "", "exact ClassAttributes asset name")
	statScan := flag.Bool("stat-scan", false, "scan matching PlayerClass payloads for Sage screenshot stat values")
	classSummary := flag.Bool("class-summary", false, "summarize ClassAttributes resource sizes")
	classStatScan := flag.Bool("class-stat-scan", false, "find ClassAttributes resources matching Sage stat rows")
	flag.Parse()
	packagePath := filepath.Join(*gamePath, "Data", "AssetData_Binary.package")
	file, err := os.Open(packagePath)
	if err != nil {
		fatal(err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		fatal(err)
	}
	reader, err := dbpf.NewReader(file, info.Size())
	if err != nil {
		fatal(err)
	}
	needle := strings.ToLower(*match)
	assetID := catalog.HashID(*asset)
	if *asset != "" {
		fmt.Printf("target %s = %08x\n", *asset, assetID)
	}
	found := 0
	sizes := make(map[int]int)
	for _, entry := range reader.Entries {
		payloadReader, openErr := reader.Open(entry)
		if openErr != nil {
			fatal(openErr)
		}
		payload, readErr := io.ReadAll(payloadReader)
		if readErr != nil {
			fatal(readErr)
		}
		if *classSummary {
			if entry.Type == 0xd117afca {
				sizes[len(payload)]++
			}
			continue
		}
		if *classStatScan {
			if entry.Type == 0xd117afca {
				scanClassStatCandidate(entry, payload)
			}
			continue
		}
		if *asset != "" {
			if uint32(entry.Instance) != assetID {
				continue
			}
		} else {
			if len(payload) < 256 || !strings.Contains(strings.ToLower(printable(payload[256:])), needle) {
				continue
			}
		}
		found++
		fmt.Printf("\nTYPE %08x GROUP %08x INSTANCE %016x SIZE %d\n", entry.Type, entry.Group, entry.Instance, len(payload))
		fmt.Printf("STRINGS %q\n", printable(payload[256:]))
		if *statScan {
			scanStats(payload)
			continue
		}
		limit := min(len(payload), 256)
		if *asset != "" {
			limit = len(payload)
		}
		for offset := 0; offset+4 <= limit; offset += 4 {
			bits := binary.LittleEndian.Uint32(payload[offset : offset+4])
			value := math.Float32frombits(bits)
			fmt.Printf("%03d  %08x  u=%-10d f=%g\n", offset, bits, bits, value)
		}
	}
	if *classSummary {
		for size, count := range sizes {
			fmt.Printf("SIZE %d COUNT %d\n", size, count)
		}
		return
	}
	if *classStatScan {
		return
	}
	fmt.Printf("\nmatched %d resources\n", found)
}

func scanClassStatCandidate(entry dbpf.Entry, payload []byte) {
	variants := map[string][]uint32{
		"alpha": {156, 148, 14, 13, 23, 102, 128, 288},
		"beta":  {165, 147, 13, 15, 22, 110, 150, 282},
		"gamma": {164, 149, 16, 11, 24, 94, 116, 294},
		"delta": {148, 150, 12, 14, 25, 106, 144, 300},
	}
	for variant, values := range variants {
		hits := make([]string, 0, len(values))
		for _, target := range values {
			for offset := 0; offset+4 <= len(payload); offset++ {
				bits := binary.LittleEndian.Uint32(payload[offset : offset+4])
				if bits == target || math.Float32frombits(bits) == float32(target) {
					hits = append(hits, fmt.Sprintf("%d@%d", target, offset))
					break
				}
			}
		}
		if len(hits) >= 6 {
			fmt.Printf("CANDIDATE %s INSTANCE %08x SIZE %d HITS %s\n", variant, uint32(entry.Instance), len(payload), strings.Join(hits, ","))
		}
	}
}

func scanStats(payload []byte) {
	values := []uint32{11, 12, 13, 14, 15, 16, 22, 23, 24, 25, 94, 102, 106, 110, 116, 128, 144, 147, 148, 149, 150, 156, 164, 165, 282, 288, 294, 300}
	for _, target := range values {
		hits := make([]string, 0)
		for offset := 0; offset+4 <= len(payload); offset++ {
			bits := binary.LittleEndian.Uint32(payload[offset : offset+4])
			if bits == target {
				hits = append(hits, fmt.Sprintf("u32@%d", offset))
			}
			if math.Float32frombits(bits) == float32(target) {
				hits = append(hits, fmt.Sprintf("f32@%d", offset))
			}
		}
		if len(hits) > 0 {
			fmt.Printf("STAT %d %s\n", target, strings.Join(hits, ","))
		}
	}
}

func printable(payload []byte) string {
	var result strings.Builder
	for _, value := range payload {
		if value == 0 {
			result.WriteByte('|')
		} else if value >= 0x20 && value <= 0x7e {
			result.WriteByte(value)
		} else {
			result.WriteByte('.')
		}
	}
	return result.String()
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
