// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package i18n

import (
	"os"
	"testing"
)

// TestDumpENFile is a maintenance hook: regen locales/en.json from the
// built-in table (kept byte-identical to what Boot writes: the en.go
// catalog plus the version marker).
//
//go:generate -ignored
func TestDumpENFile(t *testing.T) {
	b, err := catalogFile(en)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("../../locales/en.json", b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestDumpZHFile is a maintenance hook: regen locales/zh.json from the
// built-in Chinese table (kept byte-identical to what Boot writes: the
// zh.go catalog plus the version marker).
//
//go:generate -ignored
func TestDumpZHFile(t *testing.T) {
	b, err := catalogFile(zh)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("../../locales/zh.json", b, 0o644); err != nil {
		t.Fatal(err)
	}
}
