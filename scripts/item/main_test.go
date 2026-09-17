package main

import (
	"encoding/binary"
	"math"
	"testing"
)

func TestDecodePrefixName(t *testing.T) {
	tests := []struct {
		name       string
		attributes map[int]float32
		want       string
	}{
		{name: "primary pair", attributes: map[int]float32{0: 50, 1: 50}, want: "Strength + Dexterity"},
		{name: "mixed modifier", attributes: map[int]float32{10: 50, 72: 25}, want: "Critical Rating + Surefooted"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := make([]byte, 36+len(prefixAttributeNames)*4)
			for index, value := range test.attributes {
				binary.LittleEndian.PutUint32(payload[36+index*4:], math.Float32bits(value))
			}
			if actual := decodePrefixName(payload); actual != test.want {
				t.Fatalf("decodePrefixName() = %q, want %q", actual, test.want)
			}
		})
	}
}

func TestDecodeAffixStatsProducesLevelOneAndExactPreviews(t *testing.T) {
	payload := make([]byte, 36+len(prefixAttributeNames)*4)
	binary.LittleEndian.PutUint32(payload[36+5*4:], math.Float32bits(50))
	binary.LittleEndian.PutUint32(payload[36+45*4:], math.Float32bits(12))
	stats := decodeAffixStats(payload, "Prefix")
	if len(stats) != 2 {
		t.Fatalf("decodeAffixStats() returned %d stats, want 2", len(stats))
	}
	if stats[0].Name != "Power" || stats[0].Preview != "" || stats[0].PreviewExact {
		t.Fatalf("power stat = %#v", stats[0])
	}
	if stats[1].Name != "Damage from Bio Enemies" || stats[1].Preview != "-12% Damage from Bio Enemies" || !stats[1].PreviewExact {
		t.Fatalf("bio resistance stat = %#v", stats[1])
	}
}

func TestDecodeAffixStatsProducesExactSecondaryPreviews(t *testing.T) {
	payload := make([]byte, 36+len(prefixAttributeNames)*4)
	binary.LittleEndian.PutUint32(payload[36+22*4:], math.Float32bits(50))
	binary.LittleEndian.PutUint32(payload[36+37*4:], math.Float32bits(13))
	stats := decodeAffixStats(payload, "Prefix")
	if len(stats) != 2 {
		t.Fatalf("decodeAffixStats() returned %d stats, want 2", len(stats))
	}
	if stats[0].Preview != "+50% Critical Damage" || !stats[0].PreviewExact {
		t.Fatalf("critical damage stat = %#v", stats[0])
	}
	if stats[1].Preview != "+13% Area Effect Damage" || !stats[1].PreviewExact {
		t.Fatalf("area damage stat = %#v", stats[1])
	}
}

func TestDecodeAffixStatsUsesSuffixVectorOffset(t *testing.T) {
	payload := make([]byte, 56+len(prefixAttributeNames)*4)
	binary.LittleEndian.PutUint32(payload[56+10*4:], math.Float32bits(50))
	stats := decodeAffixStats(payload, "Suffix")
	if len(stats) != 1 || stats[0].Name != "Critical Rating" {
		t.Fatalf("decodeAffixStats() = %#v", stats)
	}
}
