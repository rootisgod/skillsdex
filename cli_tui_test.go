package skillsdex

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestParseTUIArgs(t *testing.T) {
	opts, err := parseTUIArgs([]string{"--no-alt-screen"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.NoAltScreen {
		t.Fatal("--no-alt-screen was not parsed")
	}
	if _, err := parseTUIArgs([]string{"--unknown"}); err == nil {
		t.Fatal("unknown option was accepted")
	}
}

func TestTUIRenderDeclaresNonEditorScope(t *testing.T) {
	model := newTUIModel(tuiOptions{NoAltScreen: true})
	model.width = 180
	model.height = 30
	view := model.View()
	for _, want := range []string{"SkillsDex TUI", "Stage 1 is not an", "editor.", "focus=nav", "up/down choose", "p preview install", "x sync preview"} {
		if !strings.Contains(view, want) {
			t.Fatalf("TUI view missing %q:\n%s", want, view)
		}
	}
}

func TestTUINavigationUsesUpDownForSidebar(t *testing.T) {
	model := newTUIModel(tuiOptions{NoAltScreen: true})
	if model.view != tuiViewSkills {
		t.Fatalf("initial view = %v, want %v", model.view, tuiViewSkills)
	}
	if model.focus != tuiFocusNav {
		t.Fatalf("initial focus = %v, want %v", model.focus, tuiFocusNav)
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(tuiModel)
	if model.view != tuiViewSets {
		t.Fatalf("down selected view = %v, want %v", model.view, tuiViewSets)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	model = updated.(tuiModel)
	if model.view != tuiViewSkills {
		t.Fatalf("up selected view = %v, want %v", model.view, tuiViewSkills)
	}
}

func TestTUINavigationUsesUpDownForRowsWhenListFocused(t *testing.T) {
	model := newTUIModel(tuiOptions{NoAltScreen: true})
	model.focus = tuiFocusList
	model.skills = []Skill{
		{OwnerSlug: "me", Slug: "alpha", Name: "Alpha"},
		{OwnerSlug: "me", Slug: "beta", Name: "Beta"},
	}

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyDown})
	model = updated.(tuiModel)
	if model.view != tuiViewSkills {
		t.Fatalf("view changed to %v, want %v", model.view, tuiViewSkills)
	}
	if model.cursor[tuiViewSkills] != 1 {
		t.Fatalf("skill cursor = %d, want 1", model.cursor[tuiViewSkills])
	}
}

func TestTUILeftRightChangesPaneFocusNotSelectedView(t *testing.T) {
	model := newTUIModel(tuiOptions{NoAltScreen: true})

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyRight})
	model = updated.(tuiModel)
	if model.view != tuiViewSkills {
		t.Fatalf("right changed view to %v, want %v", model.view, tuiViewSkills)
	}
	if model.focus != tuiFocusList {
		t.Fatalf("right focus = %v, want %v", model.focus, tuiFocusList)
	}

	updated, _ = model.Update(tea.KeyMsg{Type: tea.KeyLeft})
	model = updated.(tuiModel)
	if model.view != tuiViewSkills {
		t.Fatalf("left changed view to %v, want %v", model.view, tuiViewSkills)
	}
	if model.focus != tuiFocusNav {
		t.Fatalf("left focus = %v, want %v", model.focus, tuiFocusNav)
	}
}
