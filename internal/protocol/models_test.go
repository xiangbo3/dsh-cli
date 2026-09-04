// Built with AI-assisted development (Deepseek Harness)
// Copyright (C) 2026 xiangbo3

package protocol

import "testing"

// TestDisplayName pins the catalog resolution behind the top bar's model
// readout: a provider-named group wins, any other group is the fallback
// (the context's provider may not be among the advertised groups), and the
// raw id stands in when the catalog is nil, unknown, or nameless.
func TestDisplayName(t *testing.T) {
	cat := &SessionModels{
		Groups: []ModelProviderGroup{
			{Id: "acme", Name: "Acme", Models: []ModelCatalogModel{
				{Id: "acme-alpha", Name: "Acme Alpha"},
				{Id: "acme-beta"}, // id without a name
			}},
			{Id: "other", Name: "Other", Models: []ModelCatalogModel{
				{Id: "other-gamma", Name: "Gamma"},
			}},
		},
	}
	cases := []struct{ provider, model, want string }{
		{"acme", "acme-alpha", "Acme Alpha"},  // provider match
		{"other", "acme-alpha", "Acme Alpha"}, // provider exists, model elsewhere: cross-group
		{"ghost", "acme-alpha", "Acme Alpha"}, // provider not advertised: cross-group
		{"acme", "other-gamma", "Gamma"},      // model lives in another group
		{"acme", "acme-beta", "acme-beta"},    // nameless id: raw id
		{"acme", "acme-gamma", "acme-gamma"},  // unknown id: raw id
		{"", "acme-alpha", "Acme Alpha"},      // no provider hint: cross-group
	}
	for _, c := range cases {
		if got := cat.DisplayName(c.provider, c.model); got != c.want {
			t.Errorf("DisplayName(%q,%q) = %q, want %q", c.provider, c.model, got, c.want)
		}
	}
	var nilCat *SessionModels
	if got := nilCat.DisplayName("acme", "acme-alpha"); got != "acme-alpha" {
		t.Errorf("nil catalog = %q, want the raw id", got)
	}
	if got := cat.DisplayName("acme", ""); got != "" {
		t.Errorf("empty model = %q, want empty", got)
	}
}
