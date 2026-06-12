package skillsdex

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	tuiTitleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	tuiMutedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	tuiActiveStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")).Background(lipgloss.Color("62"))
	tuiSelectedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("238"))
	tuiWarnStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	tuiErrorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	tuiOKStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	tuiBoxStyle      = lipgloss.NewStyle().Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("240")).Padding(0, 1)
)

func (m tuiModel) View() string {
	width := m.width
	if width <= 0 {
		width = 100
	}
	height := m.height
	if height <= 0 {
		height = 30
	}
	sidebarWidth := 26
	contentWidth := maxInt(40, width-sidebarWidth-4)
	bodyHeight := maxInt(10, height-7)

	header := m.renderHeader(width)
	sidebar := tuiBoxStyle.Width(sidebarWidth).Height(bodyHeight).Render(m.renderSidebar())
	listWidth := minInt(44, maxInt(28, contentWidth/2))
	list := tuiBoxStyle.Width(listWidth).Height(bodyHeight).Render(m.renderList(bodyHeight - 2))
	detailText := m.renderDetail()
	vp := m.viewport
	vp.Width = maxInt(28, contentWidth-listWidth-4)
	vp.Height = bodyHeight
	vp.SetContent(detailText)
	detail := tuiBoxStyle.Width(vp.Width).Height(bodyHeight).Render(vp.View())
	main := lipgloss.JoinHorizontal(lipgloss.Top, sidebar, list, detail)
	if m.helpOpen {
		main = m.renderHelpModal(width, bodyHeight)
	}

	status := m.renderStatus(width)
	help := tuiMutedStyle.Render("? help  tab focus  up/down choose  left/right pane  / filter  enter details  p preview install  i install  x sync preview  s sync  r/R refresh  d doctor  l/L lint  u reload  q quit")
	if len(help) > width {
		help = help[:width]
	}
	return strings.Join([]string{header, main, status, help}, "\n")
}

func (m tuiModel) renderHeader(width int) string {
	user := "not logged in"
	if m.username != "" {
		user = m.username + " @ " + m.auth.ServerURL
	}
	filter := ""
	if m.filtering {
		filter = "  filter: " + m.filter + "_"
	} else if m.filter != "" {
		filter = "  filter: " + m.filter
	}
	focus := "nav"
	if m.focus == tuiFocusList {
		focus = "list"
	}
	title := tuiTitleStyle.Render("SkillsDex TUI")
	meta := tuiMutedStyle.Render("  " + user + "  target=" + m.target + "  focus=" + focus + filter)
	line := title + meta
	if len(line) > width {
		return line[:width]
	}
	return line
}

func (m tuiModel) renderSidebar() string {
	rows := []string{
		m.navRow(tuiViewSkills, "Skills", len(m.visibleSkills())),
		m.navRow(tuiViewSets, "Sets", len(m.sets)),
		m.navRow(tuiViewInstalled, "Installed", m.installed.InstallCount),
		m.navRow(tuiViewSync, "Sync", len(m.sync.Results)),
		m.navRow(tuiViewDoctor, "Doctor", m.doctor.IssueCount),
		m.navRow(tuiViewLint, "Lint", len(m.visibleSkills())),
	}
	rows = append(rows, "")
	rows = append(rows, tuiMutedStyle.Render("Stage 1 is not an editor."))
	rows = append(rows, tuiMutedStyle.Render("Skill creation and set editing"))
	rows = append(rows, tuiMutedStyle.Render("remain future actions."))
	return strings.Join(rows, "\n")
}

func (m tuiModel) navRow(view tuiView, label string, count int) string {
	row := fmt.Sprintf("%-12s %4d", label, count)
	if m.view == view {
		prefix := "  "
		if m.focus == tuiFocusNav {
			prefix = "> "
		}
		return tuiActiveStyle.Render(prefix + row)
	}
	return "  " + row
}

func (m tuiModel) renderList(height int) string {
	var rows []string
	rows = append(rows, tuiTitleStyle.Render(m.viewTitle()))
	switch m.view {
	case tuiViewSkills, tuiViewLint:
		for i, skill := range m.visibleSkills() {
			flags := tuiLine(tuiBoolLabel(skill.PinnedInstall, "pinned"), tuiBoolLabel(skill.RecommendedDefault, "default"))
			row := fmt.Sprintf("%s  %s  %s", tuiIdentifier(skill.OwnerSlug, skill.Slug), tuiRevisionLabel(skill.RevisionNumber), flags)
			rows = append(rows, m.selectRow(i, row))
		}
	case tuiViewSets:
		for i, set := range m.sets {
			rows = append(rows, m.selectRow(i, fmt.Sprintf("%s  %d skills", set.Slug, set.SkillCount)))
		}
	case tuiViewInstalled:
		for i, row := range m.installed.Installs {
			rows = append(rows, m.selectRow(i, fmt.Sprintf("%s  %s", row.Identifier, row.Status)))
		}
	case tuiViewSync:
		for i, result := range m.sync.Results {
			rows = append(rows, m.selectRow(i, fmt.Sprintf("%s  %s", result.Identifier, result.Status)))
		}
	case tuiViewDoctor:
		rows = append(rows, m.selectRow(0, "Auth: "+m.doctor.Auth.Status))
		rows = append(rows, m.selectRow(1, "Server: "+m.doctor.Server.Status))
		rows = append(rows, m.selectRow(2, "Library: "+m.doctor.Library.Status))
		rows = append(rows, m.selectRow(3, "Managed installs: "+m.doctor.ManagedInstalls.Status))
		rows = append(rows, m.selectRow(4, "Codex CLI: "+m.doctor.CodexCLI.Status))
		rows = append(rows, m.selectRow(5, "Codex skills dir: "+m.doctor.CodexSkillsDirectory.Status))
		rows = append(rows, m.selectRow(6, "Default target: "+m.doctor.DefaultTarget.Status))
		rows = append(rows, m.selectRow(7, fmt.Sprintf("Issues: %d", m.doctor.IssueCount)))
	}
	if len(rows) == 1 {
		rows = append(rows, tuiMutedStyle.Render("No rows."))
	}
	if len(rows) > height {
		rows = rows[:height]
	}
	return strings.Join(rows, "\n")
}

func (m tuiModel) selectRow(i int, row string) string {
	row = truncate(row, 80)
	if i == m.cursor[m.view] {
		prefix := "  "
		if m.focus == tuiFocusList {
			prefix = "> "
		}
		return tuiSelectedStyle.Render(prefix + row)
	}
	return "  " + row
}

func (m tuiModel) viewTitle() string {
	switch m.view {
	case tuiViewSkills:
		return "Skills"
	case tuiViewSets:
		return "Install Sets"
	case tuiViewInstalled:
		return "Managed Installs"
	case tuiViewSync:
		return "Sync Preview"
	case tuiViewDoctor:
		return "Doctor"
	case tuiViewLint:
		return "Quality"
	default:
		return "SkillsDex"
	}
}

func (m tuiModel) renderDetail() string {
	var b strings.Builder
	b.WriteString(tuiTitleStyle.Render(m.viewTitle()))
	b.WriteString("\n\n")
	switch m.view {
	case tuiViewSkills, tuiViewLint:
		m.renderSkillDetail(&b)
	case tuiViewSets:
		m.renderSetDetail(&b)
	case tuiViewInstalled:
		m.renderInstalledDetail(&b)
	case tuiViewSync:
		m.renderSyncDetail(&b)
	case tuiViewDoctor:
		m.renderDoctorDetail(&b)
	}
	if strings.TrimSpace(m.actionLog) != "" {
		b.WriteString("\n\n")
		b.WriteString(tuiTitleStyle.Render("Action Log"))
		b.WriteString("\n")
		b.WriteString(m.actionLog)
	}
	return b.String()
}

func (m tuiModel) renderSkillDetail(b *strings.Builder) {
	skill, ok := m.selectedSkill()
	if !ok {
		b.WriteString("No skill selected.")
		return
	}
	id := tuiIdentifier(skill.OwnerSlug, skill.Slug)
	fmt.Fprintf(b, "%s\n%s\nRevision: %s\nTags: %s\nPinned: %t  Recommended: %t\n", id, skill.Name, tuiRevisionLabel(skill.RevisionNumber), emptyDash(tagsText(skill.Tags)), skill.PinnedInstall, skill.RecommendedDefault)
	if skill.SourceURL != "" {
		fmt.Fprintf(b, "Source: %s\n", sourceDisplayLabel(&skill))
	}
	if skill.ArchivedAt != "" {
		fmt.Fprintf(b, "Archived: %s\n", skill.ArchivedAt)
	}
	b.WriteString("\n")
	b.WriteString(skill.Description)
	if detail, ok := m.skillDetails[id]; ok {
		b.WriteString("\n\nFiles:\n")
		for _, file := range detail.Files {
			mode := ""
			if file.Executable {
				mode = " executable"
			}
			fmt.Fprintf(b, "  %s (%d bytes%s)\n", file.Path, file.SizeBytes, mode)
		}
		b.WriteString("\nSKILL.md preview:\n")
		b.WriteString(truncate(detail.Raw, 2000))
	} else {
		b.WriteString("\n\nPress enter to load package files.")
	}
}

func (m tuiModel) renderSetDetail(b *strings.Builder) {
	set, ok := m.selectedSet()
	if !ok {
		b.WriteString("No install set selected.")
		return
	}
	fmt.Fprintf(b, "%s\n%s\n%d skill(s)\nUpdated: %s\n\n%s\n", set.Slug, set.Name, set.SkillCount, set.UpdatedAt.Format("2006-01-02 15:04"), set.Description)
	if detail, ok := m.setDetails[set.Slug]; ok {
		b.WriteString("\nSkills:\n")
		for _, skill := range detail.Skills {
			fmt.Fprintf(b, "  %s/%s  %s\n", skill.OwnerSlug, skill.Slug, skill.Name)
		}
	} else {
		b.WriteString("\nPress enter to load set contents.")
	}
}

func (m tuiModel) renderInstalledDetail(b *strings.Builder) {
	row, ok := m.selectedInstallRow()
	if !ok {
		b.WriteString("No managed installs.")
		return
	}
	fmt.Fprintf(b, "%s\nTarget: %s\nPath: %s\nRevision: %s\nInstall status: %s\nSource status: %s\nNeeds attention: %t\n", row.Identifier, row.Target, row.Path, row.RemoteRevision, row.Status, row.SourceStatus, row.NeedsAttention)
	if len(m.installed.Issues) > 0 {
		b.WriteString("\nIssues:\n")
		for _, issue := range m.installed.Issues {
			b.WriteString("  " + issue + "\n")
		}
	}
}

func (m tuiModel) renderSyncDetail(b *strings.Builder) {
	result, ok := m.selectedSyncResult()
	if !ok {
		b.WriteString("No managed installs to sync.")
		return
	}
	fmt.Fprintf(b, "%s\nStatus: %s\nPath: %s\nRemote revision: %s\nNext revision: %s\nNeeds attention: %t\n", result.Identifier, result.Status, result.Path, result.RemoteRevision, tuiRevisionLabel(result.NextRevision), result.NeedsAttention)
	if result.Error != "" {
		b.WriteString("\nError: " + result.Error + "\n")
	}
	if len(result.Changes) > 0 {
		b.WriteString("\nChanges:\n")
		for _, change := range result.Changes {
			fmt.Fprintf(b, "  %s  %s\n", change.Status, change.Path)
		}
	}
}

func (m tuiModel) renderDoctorDetail(b *strings.Builder) {
	fmt.Fprintf(b, "Healthy: %t\nIssues: %d\n\n", m.doctor.Healthy, m.doctor.IssueCount)
	fmt.Fprintf(b, "Auth: %s %s\n", m.doctor.Auth.Status, doctorCheckDetail(m.doctor.Auth))
	fmt.Fprintf(b, "Server: %s %s\n", m.doctor.Server.Status, doctorCheckDetail(m.doctor.Server))
	fmt.Fprintf(b, "Library: %s %s\n", m.doctor.Library.Status, doctorLibraryDetail(m.doctor.Library))
	fmt.Fprintf(b, "Managed installs: %s %s\n", m.doctor.ManagedInstalls.Status, doctorManagedInstallsDetail(m.doctor.ManagedInstalls))
	fmt.Fprintf(b, "Codex CLI: %s %s\n", m.doctor.CodexCLI.Status, doctorCheckDetail(m.doctor.CodexCLI))
	fmt.Fprintf(b, "Codex skills dir: %s %s\n", m.doctor.CodexSkillsDirectory.Status, doctorCheckDetail(m.doctor.CodexSkillsDirectory))
	fmt.Fprintf(b, "Default target: %s %s\n", m.doctor.DefaultTarget.Status, doctorCheckDetail(m.doctor.DefaultTarget))
	if len(m.doctor.Issues) > 0 {
		b.WriteString("\nIssues:\n")
		for _, issue := range m.doctor.Issues {
			b.WriteString("  " + issue + "\n")
		}
	}
	if len(m.doctor.NextSteps) > 0 {
		b.WriteString("\nNext steps:\n")
		for _, step := range m.doctor.NextSteps {
			b.WriteString("  " + step + "\n")
		}
	}
}

func (m tuiModel) renderStatus(width int) string {
	status := m.status
	if m.loading {
		status = "Working... " + status
	}
	if m.pending != nil {
		status = m.pending.Prompt + "  y/n"
	}
	if strings.Contains(strings.ToLower(status), "error") || strings.Contains(strings.ToLower(status), "failed") {
		status = tuiErrorStyle.Render(status)
	} else if strings.Contains(strings.ToLower(status), "ready") || strings.Contains(strings.ToLower(status), "completed") {
		status = tuiOKStyle.Render(status)
	} else {
		status = tuiWarnStyle.Render(status)
	}
	return truncate(status, width)
}

func truncate(value string, max int) string {
	if max <= 0 || len(value) <= max {
		return value
	}
	if max <= 3 {
		return value[:max]
	}
	return value[:max-3] + "..."
}

func emptyDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}
