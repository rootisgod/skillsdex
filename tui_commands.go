package skillsdex

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

func (m tuiModel) loadSelectedDetail() tea.Cmd {
	if !m.clientReady() {
		return nil
	}
	switch m.view {
	case tuiViewSkills, tuiViewLint:
		skill, ok := m.selectedSkill()
		if !ok {
			return nil
		}
		return tuiFetchSkillCmd(m.client, skill.OwnerSlug, skill.Slug)
	case tuiViewSets:
		set, ok := m.selectedSet()
		if !ok {
			return nil
		}
		return tuiFetchSetCmd(m.client, set.Slug)
	case tuiViewInstalled:
		row, ok := m.selectedInstallRow()
		if !ok {
			return nil
		}
		return tuiFetchSkillCmd(m.client, row.Owner, row.Slug)
	case tuiViewSync:
		result, ok := m.selectedSyncResult()
		if !ok {
			return nil
		}
		return tuiFetchSkillCmd(m.client, result.Owner, result.Slug)
	}
	return nil
}

func (m tuiModel) previewSelectedInstall() (tea.Model, tea.Cmd) {
	switch m.view {
	case tuiViewSets:
		set, ok := m.selectedSet()
		if !ok {
			return m.withStatus("No install set selected"), nil
		}
		return m, tuiCapturedActionCmd("Install set preview", func() error {
			return runInstallSet([]string{set.Slug, "--target", m.target, "--dry-run", "--yes"})
		})
	default:
		skill, ok := m.selectedSkill()
		if !ok {
			return m.withStatus("No skill selected"), nil
		}
		id := tuiIdentifier(skill.OwnerSlug, skill.Slug)
		return m, tuiCapturedActionCmd("Install preview", func() error {
			return runInstall([]string{id, "--target", m.target, "--dry-run", "--yes"})
		})
	}
}

func (m tuiModel) confirmSelectedInstall() (tea.Model, tea.Cmd) {
	switch m.view {
	case tuiViewSets:
		set, ok := m.selectedSet()
		if !ok {
			return m.withStatus("No install set selected"), nil
		}
		m.pending = &tuiPendingAction{
			Prompt: fmt.Sprintf("Install set %s to %s?", set.Slug, m.target),
			Cmd: tuiCapturedActionCmd("Install set", func() error {
				return runInstallSet([]string{set.Slug, "--target", m.target, "--yes"})
			}),
		}
		return m, nil
	default:
		skill, ok := m.selectedSkill()
		if !ok {
			return m.withStatus("No skill selected"), nil
		}
		id := tuiIdentifier(skill.OwnerSlug, skill.Slug)
		m.pending = &tuiPendingAction{
			Prompt: fmt.Sprintf("Install %s to %s?", id, m.target),
			Cmd: tuiCapturedActionCmd("Install skill", func() error {
				return runInstall([]string{id, "--target", m.target, "--yes"})
			}),
		}
		return m, nil
	}
}

func (m tuiModel) runSyncPreview() (tea.Model, tea.Cmd) {
	return m, tuiCapturedActionCmd("Sync preview", func() error {
		return runSync([]string{"--dry-run", "--strict"})
	})
}

func (m tuiModel) confirmSyncApply() (tea.Model, tea.Cmd) {
	m.pending = &tuiPendingAction{
		Prompt: "Apply sync updates to managed installs?",
		Cmd: tuiCapturedActionCmd("Sync apply", func() error {
			return runSync([]string{"--yes"})
		}),
	}
	return m, nil
}

func (m tuiModel) previewSelectedRefresh() (tea.Model, tea.Cmd) {
	skill, ok := m.selectedSkillForRefresh()
	if !ok {
		return m.withStatus("No skill selected for refresh"), nil
	}
	id := tuiIdentifier(skill.OwnerSlug, skill.Slug)
	return m, tuiCapturedActionCmd("Refresh preview", func() error {
		return runRefresh([]string{id, "--diff"})
	})
}

func (m tuiModel) confirmSelectedRefresh() (tea.Model, tea.Cmd) {
	skill, ok := m.selectedSkillForRefresh()
	if !ok {
		return m.withStatus("No skill selected for refresh"), nil
	}
	id := tuiIdentifier(skill.OwnerSlug, skill.Slug)
	m.pending = &tuiPendingAction{
		Prompt: "Apply source refresh for " + id + "?",
		Cmd: tuiCapturedActionCmd("Refresh apply", func() error {
			return runRefresh([]string{id, "--apply", "--yes"})
		}),
	}
	return m, nil
}

func (m tuiModel) runDoctor() (tea.Model, tea.Cmd) {
	return m, tuiCapturedActionCmd("Doctor", func() error {
		return runDoctor("--refresh")
	})
}

func (m tuiModel) runSelectedLint() (tea.Model, tea.Cmd) {
	skill, ok := m.selectedSkill()
	if !ok {
		return m.withStatus("No skill selected"), nil
	}
	id := tuiIdentifier(skill.OwnerSlug, skill.Slug)
	return m, tuiCapturedActionCmd("Lint skill", func() error {
		return runLint([]string{id})
	})
}

func (m tuiModel) runAllLint() (tea.Model, tea.Cmd) {
	return m, tuiCapturedActionCmd("Lint all", func() error {
		return runLint([]string{"--all"})
	})
}

func (m tuiModel) selectedSkillForRefresh() (Skill, bool) {
	switch m.view {
	case tuiViewSkills, tuiViewLint:
		return m.selectedSkill()
	case tuiViewInstalled:
		row, ok := m.selectedInstallRow()
		if !ok {
			return Skill{}, false
		}
		return Skill{OwnerSlug: row.Owner, Slug: row.Slug}, true
	case tuiViewSync:
		result, ok := m.selectedSyncResult()
		if !ok {
			return Skill{}, false
		}
		return Skill{OwnerSlug: result.Owner, Slug: result.Slug}, true
	default:
		return Skill{}, false
	}
}

func (m tuiModel) withStatus(status string) tuiModel {
	m.status = status
	return m
}
