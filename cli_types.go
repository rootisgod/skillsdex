package skillsdex

type skillResponse struct {
	Skill Skill       `json:"skill"`
	Raw   string      `json:"raw"`
	Files []SkillFile `json:"files"`
}

type installSetResponse struct {
	Set    InstallSet `json:"set"`
	Skills []Skill    `json:"skills"`
}

type installSetsResponse struct {
	Sets []InstallSet `json:"sets"`
}

type invitesResponse struct {
	Invites []SignupInvite `json:"invites"`
}

type pluginResponse struct {
	Plugin PluginInstall `json:"plugin"`
}

type pluginsResponse struct {
	Plugins []PluginInstall `json:"plugins"`
}

type setupInstallMode string

const (
	setupInstallAuto     setupInstallMode = ""
	setupInstallSet      setupInstallMode = "set"
	setupInstallPinned   setupInstallMode = "pinned"
	setupInstallDefaults setupInstallMode = "defaults"
)

type setupOptions struct {
	Target  string
	Mode    setupInstallMode
	SetSlug string
	Apply   bool
	Confirm confirmOptions
	JSON    bool
}

type setupPlanReport struct {
	ServerURL    string   `json:"server_url"`
	Username     string   `json:"username"`
	Target       string   `json:"target"`
	Mode         string   `json:"mode"`
	SetSlug      string   `json:"set_slug,omitempty"`
	Apply        bool     `json:"apply"`
	PreviewOnly  bool     `json:"preview_only"`
	ApplyCommand string   `json:"apply_command"`
	NextSteps    []string `json:"next_steps"`
}

type skillsOptions struct {
	JSON bool
	Tag  string
}

type publishOptions struct {
	Path    string
	Slug    string
	Apply   bool
	Strict  bool
	Confirm confirmOptions
}

type newSkillOptions struct {
	Path        string
	Template    string
	Slug        string
	Name        string
	WithFolders bool
	Confirm     confirmOptions
}

type showOptions struct {
	Identifier string
	Raw        bool
	FilePath   string
}

type markOptions struct {
	Identifier  string
	Pinned      *bool
	Recommended *bool
	Apply       bool
	Confirm     confirmOptions
}

type doctorOptions struct {
	Strict         bool
	FixPermissions bool
	Refresh        bool
	JSON           bool
}

type installedOptions struct {
	Strict  bool
	Refresh bool
	JSON    bool
}

type installedStatusRow struct {
	Identifier     string `json:"identifier"`
	Owner          string `json:"owner"`
	Slug           string `json:"slug"`
	Target         string `json:"target"`
	Path           string `json:"path"`
	RemoteRevision string `json:"remote_revision"`
	Status         string `json:"status"`
	SourceStatus   string `json:"source_status"`
	NeedsAttention bool   `json:"needs_attention"`
}

type installedStatusReport struct {
	InstallCount       int                  `json:"install_count"`
	IssueCount         int                  `json:"issue_count"`
	Healthy            bool                 `json:"healthy"`
	Strict             bool                 `json:"strict"`
	Refresh            bool                 `json:"refresh"`
	StatusCounts       map[string]int       `json:"status_counts"`
	SourceStatusCounts map[string]int       `json:"source_status_counts"`
	Issues             []string             `json:"issues"`
	Installs           []installedStatusRow `json:"installs"`
}

type doctorPathReport struct {
	Path        string `json:"path"`
	Status      string `json:"status"`
	Permissions string `json:"permissions,omitempty"`
	Detail      string `json:"detail,omitempty"`
	Issue       string `json:"issue,omitempty"`
}

type doctorCheckReport struct {
	Status  string `json:"status"`
	Value   string `json:"value,omitempty"`
	Message string `json:"message,omitempty"`
}

type doctorLibraryReport struct {
	Status      string `json:"status"`
	Active      int    `json:"active"`
	Recommended int    `json:"recommended"`
	Pinned      int    `json:"pinned"`
	Message     string `json:"message,omitempty"`
}

type doctorManagedInstallsReport struct {
	Status              string         `json:"status"`
	Count               int            `json:"count"`
	StatusCounts        map[string]int `json:"status_counts,omitempty"`
	SourceRefreshCounts map[string]int `json:"source_refresh_counts,omitempty"`
	Message             string         `json:"message,omitempty"`
}

type doctorReport struct {
	SkillsDexDirectory   string                      `json:"skillsdex_directory"`
	AuthPath             doctorPathReport            `json:"auth_path"`
	ManifestPath         doctorPathReport            `json:"manifest_path"`
	ConfigPath           doctorPathReport            `json:"config_path"`
	DefaultTarget        doctorCheckReport           `json:"default_target"`
	Auth                 doctorCheckReport           `json:"auth"`
	Server               doctorCheckReport           `json:"server"`
	Library              doctorLibraryReport         `json:"library"`
	ManagedInstalls      doctorManagedInstallsReport `json:"managed_installs"`
	CodexCLI             doctorCheckReport           `json:"codex_cli"`
	CodexSkillsDirectory doctorCheckReport           `json:"codex_skills_directory"`
	FixedPermissions     []string                    `json:"fixed_permissions,omitempty"`
	NextSteps            []string                    `json:"next_steps"`
	Issues               []string                    `json:"issues"`
	IssueCount           int                         `json:"issue_count"`
	Healthy              bool                        `json:"healthy"`
	Strict               bool                        `json:"strict"`
	Refresh              bool                        `json:"refresh"`
}

type localSkillPackage struct {
	Input   SkillInput
	Skipped []string
}

type lintReport struct {
	Target       string              `json:"target"`
	Summary      string              `json:"summary"`
	IssueCount   int                 `json:"issue_count"`
	WarningCount int                 `json:"warning_count"`
	Issues       []SkillQualityIssue `json:"issues"`
}

type lintReports struct {
	Summary      string       `json:"summary"`
	SkillCount   int          `json:"skill_count"`
	IssueCount   int          `json:"issue_count"`
	WarningCount int          `json:"warning_count"`
	Reports      []lintReport `json:"reports"`
}

type importSourceResponse struct {
	Results []skillImportResult `json:"results"`
}

type importSourceOptions struct {
	Source  string
	Install bool
	Target  string
	Confirm confirmOptions
}

type setsOptions struct {
	Slug string
	JSON bool
}

type pluginsOptions struct {
	Slug   string
	Target string
	JSON   bool
}

type pluginAddOptions struct {
	Input PluginInstallInput
}

type pluginInstallOptions struct {
	Slug    string
	Confirm confirmOptions
}

type invitesOptions struct {
	JSON bool
}

type inviteCreateOptions struct {
	JSON  bool
	Email string
}

type refreshResponse struct {
	Skill   Skill                 `json:"skill"`
	Status  string                `json:"status"`
	DryRun  bool                  `json:"dry_run"`
	Changes []SourceRefreshChange `json:"changes"`
}

type refreshReport struct {
	DryRun       bool            `json:"dry_run"`
	Strict       bool            `json:"strict"`
	All          bool            `json:"all"`
	Target       string          `json:"target,omitempty"`
	CheckedCount int             `json:"checked_count"`
	UpdateCount  int             `json:"update_count"`
	FailureCount int             `json:"failure_count"`
	Healthy      bool            `json:"healthy"`
	StatusCounts map[string]int  `json:"status_counts"`
	NextSteps    []string        `json:"next_steps,omitempty"`
	Results      []refreshResult `json:"results"`
}

type refreshResult struct {
	Identifier string                `json:"identifier"`
	Status     string                `json:"status"`
	Error      string                `json:"error,omitempty"`
	Skill      *Skill                `json:"skill,omitempty"`
	Changes    []SourceRefreshChange `json:"changes,omitempty"`
}

type confirmOptions struct {
	Yes    bool
	No     bool
	DryRun bool
}

type syncOptions struct {
	Confirm confirmOptions
	Strict  bool
	JSON    bool
}

type syncDryRunResult struct {
	Identifier     string                `json:"identifier"`
	Owner          string                `json:"owner"`
	Slug           string                `json:"slug"`
	Target         string                `json:"target"`
	Path           string                `json:"path"`
	Status         string                `json:"status"`
	RemoteRevision string                `json:"remote_revision,omitempty"`
	NextRevision   int                   `json:"next_revision,omitempty"`
	NeedsAttention bool                  `json:"needs_attention"`
	Error          string                `json:"error,omitempty"`
	Changes        []SourceRefreshChange `json:"changes,omitempty"`
}

type syncDryRunReport struct {
	DryRun       bool               `json:"dry_run"`
	Strict       bool               `json:"strict"`
	InstallCount int                `json:"install_count"`
	PendingCount int                `json:"pending_count"`
	FailureCount int                `json:"failure_count"`
	CleanCount   int                `json:"clean_count"`
	Healthy      bool               `json:"healthy"`
	StatusCounts map[string]int     `json:"status_counts"`
	NextSteps    []string           `json:"next_steps,omitempty"`
	Results      []syncDryRunResult `json:"results"`
}

type backupFileOptions struct {
	File   string
	DryRun bool
	JSON   bool
}
