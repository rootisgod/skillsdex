package skillsdex

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m tuiModel) renderHelpModal(width, bodyHeight int) string {
	modalWidth := minInt(96, maxInt(52, width-8))
	modalHeight := maxInt(10, bodyHeight-2)
	contentWidth := maxInt(32, modalWidth-4)
	contentHeight := maxInt(6, modalHeight-4)

	var lines []string
	lines = append(lines, tuiTitleStyle.Render("SkillsDex Help"))
	lines = append(lines, m.renderHelpTabs())
	lines = append(lines, "")
	if m.helpTab == 0 {
		lines = append(lines, tuiHelpKeysLines()...)
	} else {
		lines = append(lines, tuiHelpCommandLines()...)
	}
	lines = append(lines, "")
	lines = append(lines, tuiMutedStyle.Render("tab/right next tab  left previous tab  ?/esc/q close"))
	lines = truncateLines(lines, contentWidth, contentHeight)

	box := tuiBoxStyle.Width(contentWidth).Height(modalHeight).Render(strings.Join(lines, "\n"))
	return lipgloss.Place(width, bodyHeight, lipgloss.Center, lipgloss.Center, box)
}

func (m tuiModel) renderHelpTabs() string {
	keyStyle := tuiMutedStyle
	commandStyle := tuiMutedStyle
	if m.helpTab == 0 {
		keyStyle = tuiActiveStyle
	} else {
		commandStyle = tuiActiveStyle
	}
	return keyStyle.Render(" Keys ") + "  " + commandStyle.Render(" Commands ")
}

func tuiHelpKeysLines() []string {
	return []string{
		"Navigation",
		"  up/down, k/j        choose a section or row",
		"  left/right          move focus between section list and rows",
		"  tab                 toggle section/list focus",
		"  enter               load selected details, or enter the row pane",
		"  /                   filter visible skills",
		"",
		"Actions",
		"  p                   preview selected install",
		"  i                   install selected skill or set",
		"  x                   run sync preview",
		"  s                   apply sync after confirmation",
		"  r / R               preview / apply source refresh",
		"  d                   run doctor",
		"  l / L               lint selected skill / all visible skills",
		"  u                   reload dashboard data",
		"  q                   quit, or close this help popup",
	}
}

func tuiHelpCommandLines() []string {
	return []string{
		"Setup and status",
		"  skillsdex login --server URL",
		"  skillsdex whoami",
		"  skillsdex setup --dry-run",
		"  skillsdex doctor --strict --refresh",
		"  skillsdex installed --refresh",
		"",
		"Find and inspect skills",
		"  skillsdex skills",
		"  skillsdex search QUERY --limit 10",
		"  skillsdex show owner/slug",
		"  skillsdex lint --file ./path/to/SKILL.md",
		"",
		"Install and maintain",
		"  skillsdex install owner/slug --dry-run",
		"  skillsdex install-set daily --dry-run",
		"  skillsdex sync --dry-run --strict",
		"  skillsdex sync --yes",
		"  skillsdex uninstall owner/slug",
		"",
		"Authoring outside the TUI",
		"  skillsdex new ./path/to/skill --template workflow",
		"  skillsdex publish ./path/to/skill --dry-run",
		"  skillsdex publish ./path/to/skill --apply --yes",
		"  skillsdex refresh owner/slug --dry-run --diff",
		"",
		"Backups and exports",
		"  skillsdex backup export --out skillsdex-backup.zip",
		"  skillsdex backup inspect --file skillsdex-backup.zip",
		"  skillsdex github export --dir ./skills-export",
	}
}

func truncateLines(lines []string, width, height int) []string {
	if width > 0 {
		for i, line := range lines {
			lines[i] = truncate(line, width)
		}
	}
	if height > 0 && len(lines) > height {
		lines = lines[:height]
		if len(lines) > 0 {
			lines[len(lines)-1] = truncate(tuiMutedStyle.Render("More commands are available in skillsdex --help."), width)
		}
	}
	return lines
}
