// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package i18n

import "testing"

// TestCatalogParity: the built-in Chinese table must carry exactly the
// English key set — a zh gap leaks English into the Chinese UI with no
// other test to catch it (TestMissingKeys covers user files, not zh.go).
func TestCatalogParity(t *testing.T) {
	for k := range en {
		if _, ok := zh[k]; !ok {
			t.Errorf("zh table missing key %q", k)
		}
	}
	for k := range zh {
		if _, ok := en[k]; !ok {
			t.Errorf("zh table carries %q, absent from the English source", k)
		}
	}
}
