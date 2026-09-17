package main

import (
	"slices"
	"testing"
)

func TestEffectNamesPreservesRawSnakeCaseIdentifiers(t *testing.T) {
	names := effectNames([]byte("\x00character_beam_in_plasma_electric\x00ignored\x00status_poisoned\x00"))
	for _, expected := range []string{"character_beam_in_plasma_electric", "status_poisoned"} {
		if !slices.Contains(names, expected) {
			t.Fatalf("effectNames() = %v, missing %q", names, expected)
		}
	}
}
