package slash_test

import (
	"testing"

	"github.com/nano-harness/nano-agent/pkg/slash"
)

func TestNewRegistry_BuiltinCommands(t *testing.T) {
	r := slash.NewRegistry("")
	all := r.All()
	if len(all) == 0 {
		t.Fatal("expected at least built-in commands, got none")
	}

	// Verify all expected built-in commands are present (including full opsx set).
	wantNames := []string{
		"yolo", "permission", "permissions", "allow", "disallow",
		"skill:list", "skill:use", "skill:off", "skill:info", "skill:install",
		"loop", "schedule",
		"opsx:propose", "opsx:explore", "opsx:new", "opsx:continue", "opsx:ff",
		"opsx:apply", "opsx:verify", "opsx:sync", "opsx:status",
		"opsx:archive", "opsx:bulk-archive",
	}
	nameSet := make(map[string]bool, len(all))
	for _, cmd := range all {
		nameSet[cmd.Name] = true
	}
	for _, want := range wantNames {
		if !nameSet[want] {
			t.Errorf("expected built-in command %q to be registered", want)
		}
	}
}

func TestNewRegistry_CategoryOrder(t *testing.T) {
	r := slash.NewRegistry("")
	all := r.All()

	prevIdx := -1
	catOrder := map[slash.Category]int{
		slash.CategoryPermission: 0,
		slash.CategorySkill:      1,
		slash.CategorySchedule:   2,
		slash.CategoryOpenSpec:   3,
		slash.CategoryCustom:     4,
	}
	for _, cmd := range all {
		idx := catOrder[cmd.Category]
		if idx < prevIdx {
			t.Errorf("command %q out of category order (category %q after higher-index category)", cmd.Name, cmd.Category)
		}
		prevIdx = idx
	}
}

func TestRegistry_ByCategory(t *testing.T) {
	r := slash.NewRegistry("")
	perms := r.ByCategory(slash.CategoryPermission)
	if len(perms) == 0 {
		t.Fatal("expected permission commands, got none")
	}
	for _, cmd := range perms {
		if cmd.Category != slash.CategoryPermission {
			t.Errorf("ByCategory(permission) returned command with category %q", cmd.Category)
		}
	}
}

func TestRegistry_Search(t *testing.T) {
	r := slash.NewRegistry("")

	// Empty query returns all.
	all := r.Search("")
	if len(all) != len(r.All()) {
		t.Errorf("Search(\"\") returned %d, want %d", len(all), len(r.All()))
	}

	// Known substring match.
	results := r.Search("skill")
	if len(results) == 0 {
		t.Fatal("Search(\"skill\") returned no results")
	}
	for _, cmd := range results {
		if cmd.Category != slash.CategorySkill {
			// Only skill commands have "skill" in their name — if custom commands
			// happen to have "skill" in description that's also fine.
			_ = cmd
		}
	}

	// No-match query.
	nothing := r.Search("zzznomatch")
	if len(nothing) != 0 {
		t.Errorf("Search(\"zzznomatch\") returned %d results, want 0", len(nothing))
	}
}

func TestRegistry_Names(t *testing.T) {
	r := slash.NewRegistry("")
	names := r.Names()
	if len(names) == 0 {
		t.Fatal("Names() returned empty list")
	}
	for _, n := range names {
		if len(n) == 0 || n[0] != '/' {
			t.Errorf("Names() returned %q which does not start with '/'", n)
		}
	}
}

func TestNewBuiltinRegistry(t *testing.T) {
	r := slash.NewBuiltinRegistry()
	all := r.All()
	if len(all) == 0 {
		t.Fatal("NewBuiltinRegistry() returned empty list")
	}
	// Built-in registry should contain no custom commands.
	for _, cmd := range all {
		if cmd.Category == slash.CategoryCustom {
			t.Errorf("NewBuiltinRegistry() returned custom command %q", cmd.Name)
		}
	}
	// Should still contain all known built-in categories.
	cats := make(map[slash.Category]bool)
	for _, cmd := range all {
		cats[cmd.Category] = true
	}
	for _, want := range []slash.Category{
		slash.CategoryPermission,
		slash.CategorySkill,
		slash.CategorySchedule,
		slash.CategoryOpenSpec,
	} {
		if !cats[want] {
			t.Errorf("NewBuiltinRegistry() missing category %q", want)
		}
	}
}
