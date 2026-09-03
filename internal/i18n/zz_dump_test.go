package i18n

import (
	"encoding/json"
	"os"
	"testing"
)

// TestDumpENFile is a maintenance hook: regen locales/en.json from the
// built-in table (kept byte-identical to the en.go catalog).
//
//go:generate -ignored
func TestDumpENFile(t *testing.T) {
	b, err := json.MarshalIndent(en, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("../../locales/en.json", append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestDumpZHFile is a maintenance hook: regen locales/zh.json from the
// built-in Chinese table (kept byte-identical to the zh.go catalog).
//
//go:generate -ignored
func TestDumpZHFile(t *testing.T) {
	b, err := json.MarshalIndent(zh, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("../../locales/zh.json", append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}
