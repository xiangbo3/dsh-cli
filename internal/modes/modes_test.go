package modes

import (
	"testing"

	"dsh-cli/internal/protocol"
)

// rosterForTests mirrors a deployment that ships the four modes (with
// localized file metadata the display layer must override) plus two
// user-authored presets sharing a prefix.
func rosterForTests() []protocol.AgentPresetEntry {
	return []protocol.AgentPresetEntry{
		{Id: "standard", Trust: "system", IsDefault: true, Name: "标准模式", Description: "功能完整的编码 Agent。"},
		{Id: "code", Trust: "system", Name: "PTC 模式", Description: "Code Mode SDK。"},
		{Id: "minimal", Trust: "system"},
		{Id: "cordis", Trust: "system"},
		{Id: "my-kit", Trust: "user", Name: "My Toolkit", Description: "user preset"},
		{Id: "my-kit-two", Trust: "user"},
	}
}

func TestLabelShort(t *testing.T) {
	cases := []struct{ id, want string }{
		{"standard", "Standard mode"},
		{"code", "PTC mode"},
		{"minimal", "Minimal mode"},
		{"cordis", "Creator mode"},
		{"my-kit", "my-kit"},
		{"", ""},
	}
	for _, c := range cases {
		if got := Label(c.id); got != c.want {
			t.Errorf("Label(%q) = %q, want %q", c.id, got, c.want)
		}
	}
	if got := Short("code"); got != "ptc" {
		t.Errorf("Short(code) = %q, want ptc", got)
	}
	if got := Short("my-kit"); got != "my-kit" {
		t.Errorf("Short(my-kit) = %q, want my-kit", got)
	}
}

func TestStaticID(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"standard", "standard"},
		{"ptc", "code"},
		{"PTC", "code"},
		{"ptc mode", "code"},
		{"PTC Mode", "code"},
		{"code", "code"},
		{"minimal", "minimal"},
		{"creator", "cordis"},
		{"Creator mode", "cordis"},
		{"cordis", "cordis"},
		{"my-custom", "my-custom"}, // roster-unknown ids pass through
		{"  ptc  ", "code"},
		{"", ""},
	}
	for _, c := range cases {
		if got := StaticID(c.in); got != c.want {
			t.Errorf("StaticID(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestResolve(t *testing.T) {
	r := rosterForTests()
	find := func(name string) (*protocol.AgentPresetEntry, string) {
		e, problem := Resolve(r, name)
		if e == nil {
			return nil, problem
		}
		return e, ""
	}
	byID := func(id string) string {
		e, problem := find(id)
		if e == nil {
			t.Fatalf("Resolve(%q) = nil: %s", id, problem)
		}
		return e.Id
	}
	// Shipped aliases and canonical names.
	for _, c := range []struct {
		name string
		id   string
	}{
		{"standard", "standard"},
		{"standard mode", "standard"},
		{"Standard Mode", "standard"},
		{"ptc", "code"},
		{"PTC", "code"},
		{"PTC mode", "code"},
		{"code", "code"},
		{"minimal", "minimal"},
		{"Minimal mode", "minimal"},
		{"creator", "cordis"},
		{"Creator mode", "cordis"},
		{"cordis", "cordis"},
		// Published names and ids, case-insensitive.
		{"my toolkit", "my-kit"},
		{"My Toolkit", "my-kit"},
		{"MY-KIT", "my-kit"},
		// Unique prefix.
		{"mini", "minimal"},
		{"cord", "cordis"},
	} {
		if got := byID(c.name); got != c.id {
			t.Errorf("Resolve(%q) = %q, want %q", c.name, got, c.id)
		}
	}
	// Ambiguous prefix lists the candidates.
	if _, problem := find("my"); problem == "" {
		t.Error("Resolve(my): want ambiguity, got none")
	} else if problem != "matches my-kit, my-kit-two — be more specific" {
		t.Errorf("Resolve(my) problem = %q", problem)
	}
	// Unknown.
	if e, problem := find("nowhere"); e != nil || problem == "" {
		t.Errorf("Resolve(nowhere) = %v, %q (want no match)", e, problem)
	}
	if e, problem := find("  "); e != nil || problem != "empty mode name" {
		t.Errorf("Resolve(whitespace) = %v, %q", e, problem)
	}
}

func TestNameDescription(t *testing.T) {
	r := rosterForTests()
	byID := map[string]protocol.AgentPresetEntry{}
	for _, e := range r {
		byID[e.Id] = e
	}
	// Shipped ids keep the canonical English copy despite localized files.
	if got := Name(byID["standard"]); got != "Standard mode" {
		t.Errorf("Name(standard) = %q", got)
	}
	if got := Description(byID["code"]); got != "All Standard mode capabilities, with tools exposed through the Code Mode SDK so the model can combine multi-step operations in one TypeScript program." {
		t.Errorf("Description(code) = %q", got)
	}
	// Shipped preset without published metadata still gets copy.
	if got := Name(byID["minimal"]); got != "Minimal mode" {
		t.Errorf("Name(minimal) = %q", got)
	}
	// User presets keep their published name; without one, the id.
	if got := Name(byID["my-kit"]); got != "My Toolkit" {
		t.Errorf("Name(my-kit) = %q", got)
	}
	if got := Description(byID["my-kit"]); got != "user preset" {
		t.Errorf("Description(my-kit) = %q", got)
	}
	if got := Name(byID["my-kit-two"]); got != "my-kit-two" {
		t.Errorf("Name(my-kit-two) = %q", got)
	}
	// A user preset may name itself like a shipped one; trust decides.
	sneak := protocol.AgentPresetEntry{Id: "standard", Trust: "user", Name: "Impostor"}
	if got := Name(sneak); got != "Impostor" {
		t.Errorf("Name(user/standard) = %q, want Impostor", got)
	}
}
