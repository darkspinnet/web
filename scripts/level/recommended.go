package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"

	"github.com/darkspinnet/darkspin/content/dbpf"
)

// Build 103's MapRoomUI.SetRecommendedGearScoreText receives the 72 float
// pairs at DifficultyTuning +24 (count +28), indexed by total difficulty.
// The serialized table starts at 968. ExpectedAvatarLevel is a separate
// integer table at 1544 and is not the mission screen's recommendation.
func enrichRecommendedSquadLevels(gamePath string, levels []levelReport) error {
	file, err := os.Open(filepath.Join(gamePath, "Data", "AssetData_Binary.package"))
	if err != nil {
		return fmt.Errorf("recommended squad levels: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	pkg, err := dbpf.NewReader(file, info.Size())
	if err != nil {
		return err
	}
	for _, entry := range pkg.Entries {
		if entry.Type != 0x8e94f44c || entry.Group != 0 || entry.Instance != 0x02ca2581 {
			continue
		}
		reader, err := pkg.Open(entry)
		if err != nil {
			return err
		}
		payload, err := io.ReadAll(reader)
		if err != nil {
			return err
		}
		if len(payload) != 2120 || binary.LittleEndian.Uint32(payload[28:32]) != 72 {
			return fmt.Errorf("recommended squad levels: unsupported DifficultyTuning layout")
		}
		var ranges [72][2]float32
		for i := range ranges {
			offset := 968 + i*8
			minimum := math.Float32frombits(binary.LittleEndian.Uint32(payload[offset : offset+4]))
			maximum := math.Float32frombits(binary.LittleEndian.Uint32(payload[offset+4 : offset+8]))
			if math.IsNaN(float64(minimum)) || math.IsInf(float64(minimum), 0) ||
				math.IsNaN(float64(maximum)) || math.IsInf(float64(maximum), 0) ||
				minimum < 0 || maximum < minimum {
				return fmt.Errorf("recommended squad levels: invalid range at difficulty %d", i+1)
			}
			ranges[i] = [2]float32{minimum, maximum}
		}
		for i := range levels {
			if campaign := levels[i].Campaign; campaign != nil {
				if campaign.Difficulty < 1 || campaign.Difficulty > len(ranges) {
					return fmt.Errorf("recommended squad levels: invalid difficulty %d", campaign.Difficulty)
				}
				selected := ranges[campaign.Difficulty-1]
				campaign.RecommendedSquadMin = selected[0]
				campaign.RecommendedSquadMax = selected[1]
			}
		}
		return nil
	}
	return fmt.Errorf("recommended squad levels: DifficultyTuning asset unavailable")
}
