package skillsdex

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

type tuiView int

const (
	tuiViewSkills tuiView = iota
	tuiViewSets
	tuiViewInstalled
	tuiViewSync
	tuiViewDoctor
	tuiViewLint
)

var tuiViews = []tuiView{tuiViewSkills, tuiViewSets, tuiViewInstalled, tuiViewSync, tuiViewDoctor, tuiViewLint}

type tuiFocus int

const (
	tuiFocusNav tuiFocus = iota
	tuiFocusList
)

type tuiPendingAction struct {
	Prompt string
	Cmd    tea.Cmd
}

type tuiModel struct {
	opts         tuiOptions
	width        int
	height       int
	view         tuiView
	focus        tuiFocus
	cursor       map[tuiView]int
	filtering    bool
	filter       string
	loading      bool
	status       string
	actionLog    string
	pending      *tuiPendingAction
	auth         AuthConfig
	client       *apiClient
	username     string
	target       string
	skills       []Skill
	sets         []InstallSet
	installed    installedStatusReport
	sync         syncDryRunReport
	doctor       doctorReport
	skillDetails map[string]skillResponse
	setDetails   map[string]installSetResponse
	viewport     viewport.Model
}

func newTUIModel(opts tuiOptions) tuiModel {
	return tuiModel{
		opts:         opts,
		view:         tuiViewSkills,
		focus:        tuiFocusNav,
		cursor:       map[tuiView]int{},
		loading:      true,
		status:       "Loading SkillsDex data...",
		target:       "codex",
		skillDetails: map[string]skillResponse{},
		setDetails:   map[string]installSetResponse{},
		viewport:     viewport.New(80, 20),
	}
}

func (m tuiModel) Init() tea.Cmd {
	return tuiLoadDataCmd()
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = maxInt(20, msg.Width-34)
		m.viewport.Height = maxInt(6, msg.Height-8)
		return m, nil
	case tuiDataMsg:
		m.loading = false
		m.auth = msg.Auth
		if msg.Auth.Token != "" {
			m.client = newAPIClient(msg.Auth)
		} else {
			m.client = nil
		}
		m.username = msg.Username
		m.target = msg.Target
		m.skills = msg.Skills
		m.sets = msg.Sets
		m.installed = msg.Installed
		m.sync = msg.Sync
		m.doctor = msg.Doctor
		if msg.Err != nil {
			m.status = msg.Err.Error()
		} else {
			m.status = "Ready"
		}
		m.clampCursor()
		return m, nil
	case tuiSkillDetailMsg:
		m.loading = false
		if msg.Err != nil {
			m.status = msg.Err.Error()
			return m, nil
		}
		m.skillDetails[msg.Identifier] = msg.Response
		m.status = "Loaded " + msg.Identifier
		return m, nil
	case tuiSetDetailMsg:
		m.loading = false
		if msg.Err != nil {
			m.status = msg.Err.Error()
			return m, nil
		}
		m.setDetails[msg.Slug] = msg.Response
		m.status = "Loaded set " + msg.Slug
		return m, nil
	case tuiActionMsg:
		m.loading = false
		m.pending = nil
		m.actionLog = strings.TrimSpace(msg.Output)
		if msg.Err != nil {
			if m.actionLog != "" {
				m.actionLog += "\n"
			}
			m.actionLog += "Error: " + msg.Err.Error()
			m.status = msg.Label + " failed"
			return m, tuiLoadDataCmd()
		}
		if m.actionLog == "" {
			m.actionLog = msg.Label + " completed."
		}
		m.status = msg.Label + " completed"
		return m, tuiLoadDataCmd()
	case tea.KeyMsg:
		return m.handleKey(msg)
	default:
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
}

func (m tuiModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.pending != nil {
		switch key {
		case "y", "Y":
			cmd := m.pending.Cmd
			m.pending = nil
			m.loading = true
			m.status = "Running action..."
			return m, cmd
		case "n", "N", "esc":
			m.pending = nil
			m.status = "Action cancelled"
			return m, nil
		}
		return m, nil
	}
	if m.filtering {
		switch key {
		case "enter", "esc":
			m.filtering = false
			m.clampCursor()
			return m, nil
		case "backspace", "ctrl+h":
			if len(m.filter) > 0 {
				m.filter = m.filter[:len(m.filter)-1]
			}
			m.clampCursor()
			return m, nil
		case "ctrl+c":
			return m, tea.Quit
		default:
			if len(msg.String()) == 1 {
				m.filter += msg.String()
				m.clampCursor()
			}
			return m, nil
		}
	}
	switch key {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "tab", "shift+tab":
		m.toggleFocus()
		return m, nil
	case "left":
		m.focus = tuiFocusNav
		return m, nil
	case "right":
		m.focus = tuiFocusList
		return m, nil
	case "up", "k":
		if m.focus == tuiFocusNav {
			m.previousView()
		} else if m.cursor[m.view] > 0 {
			m.cursor[m.view]--
		}
		return m, nil
	case "down", "j":
		if m.focus == tuiFocusNav {
			m.nextView()
		} else {
			m.cursor[m.view]++
			m.clampCursor()
		}
		return m, nil
	case "pgup":
		m.viewport.LineUp(8)
		return m, nil
	case "pgdown":
		m.viewport.LineDown(8)
		return m, nil
	case "/":
		m.filtering = true
		return m, nil
	case "enter":
		if m.focus == tuiFocusNav {
			m.focus = tuiFocusList
			return m, nil
		}
		return m, m.loadSelectedDetail()
	case "p":
		return m.previewSelectedInstall()
	case "i":
		return m.confirmSelectedInstall()
	case "x":
		return m.runSyncPreview()
	case "s":
		return m.confirmSyncApply()
	case "r":
		return m.previewSelectedRefresh()
	case "R":
		return m.confirmSelectedRefresh()
	case "d":
		return m.runDoctor()
	case "l":
		return m.runSelectedLint()
	case "L":
		return m.runAllLint()
	case "u":
		m.loading = true
		m.status = "Refreshing dashboard..."
		return m, tuiLoadDataCmd()
	}
	return m, nil
}

func (m *tuiModel) toggleFocus() {
	if m.focus == tuiFocusNav {
		m.focus = tuiFocusList
		return
	}
	m.focus = tuiFocusNav
}

func (m *tuiModel) nextView() {
	m.view = tuiViews[(m.viewIndex()+1)%len(tuiViews)]
	m.clampCursor()
}

func (m *tuiModel) previousView() {
	idx := m.viewIndex() - 1
	if idx < 0 {
		idx = len(tuiViews) - 1
	}
	m.view = tuiViews[idx]
	m.clampCursor()
}

func (m tuiModel) viewIndex() int {
	for i, view := range tuiViews {
		if view == m.view {
			return i
		}
	}
	return 0
}

func (m *tuiModel) clampCursor() {
	max := m.currentCount() - 1
	if max < 0 {
		m.cursor[m.view] = 0
		return
	}
	if m.cursor[m.view] > max {
		m.cursor[m.view] = max
	}
	if m.cursor[m.view] < 0 {
		m.cursor[m.view] = 0
	}
}

func (m tuiModel) currentCount() int {
	switch m.view {
	case tuiViewSkills:
		return len(m.visibleSkills())
	case tuiViewSets:
		return len(m.sets)
	case tuiViewInstalled:
		return len(m.installed.Installs)
	case tuiViewSync:
		return len(m.sync.Results)
	case tuiViewDoctor:
		return 8
	case tuiViewLint:
		return len(m.visibleSkills())
	default:
		return 0
	}
}

func (m tuiModel) visibleSkills() []Skill {
	if strings.TrimSpace(m.filter) == "" {
		return m.skills
	}
	q := strings.ToLower(strings.TrimSpace(m.filter))
	var out []Skill
	for _, skill := range m.skills {
		haystack := strings.ToLower(strings.Join([]string{
			skill.OwnerSlug + "/" + skill.Slug,
			skill.Name,
			skill.Description,
			skill.ShortDescription,
			tagsText(skill.Tags),
		}, " "))
		if strings.Contains(haystack, q) {
			out = append(out, skill)
		}
	}
	return out
}

func (m tuiModel) selectedSkill() (Skill, bool) {
	skills := m.visibleSkills()
	if len(skills) == 0 {
		return Skill{}, false
	}
	idx := m.cursor[tuiViewSkills]
	if m.view == tuiViewLint {
		idx = m.cursor[tuiViewLint]
	}
	if idx < 0 || idx >= len(skills) {
		return Skill{}, false
	}
	return skills[idx], true
}

func (m tuiModel) selectedSet() (InstallSet, bool) {
	if len(m.sets) == 0 {
		return InstallSet{}, false
	}
	idx := m.cursor[tuiViewSets]
	if idx < 0 || idx >= len(m.sets) {
		return InstallSet{}, false
	}
	return m.sets[idx], true
}

func (m tuiModel) selectedSyncResult() (syncDryRunResult, bool) {
	if len(m.sync.Results) == 0 {
		return syncDryRunResult{}, false
	}
	idx := m.cursor[tuiViewSync]
	if idx < 0 || idx >= len(m.sync.Results) {
		return syncDryRunResult{}, false
	}
	return m.sync.Results[idx], true
}

func (m tuiModel) selectedInstallRow() (installedStatusRow, bool) {
	if len(m.installed.Installs) == 0 {
		return installedStatusRow{}, false
	}
	idx := m.cursor[tuiViewInstalled]
	if idx < 0 || idx >= len(m.installed.Installs) {
		return installedStatusRow{}, false
	}
	return m.installed.Installs[idx], true
}

func (m tuiModel) clientReady() bool {
	return m.client != nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func tuiIdentifier(owner, slug string) string {
	return owner + "/" + slug
}

func tuiLine(parts ...string) string {
	return strings.TrimSpace(strings.Join(parts, " "))
}

func tuiBoolLabel(value bool, label string) string {
	if value {
		return label
	}
	return ""
}

func tuiRevisionLabel(n int) string {
	if n == 0 {
		return "-"
	}
	return fmt.Sprintf("r%d", n)
}
