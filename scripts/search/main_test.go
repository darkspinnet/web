package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAppendOptionalItemsIndexesBaseFamily(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "item.json")
	contents := `{"items":[{"name":"Safeguard","slug":"safeguard-defense","category":"Defense","variant_count":16}]}`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	rows := make([][]string, 0)
	if err := appendOptionalItems(path, &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	want := []string{"Safeguard", "ITEM", "Defense / ITEM FAMILY", "/item/safeguard-defense/", "safeguard 16 defense"}
	for index := range want {
		if rows[0][index] != want[index] {
			t.Fatalf("row[%d] = %q, want %q", index, rows[0][index], want[index])
		}
	}
}
