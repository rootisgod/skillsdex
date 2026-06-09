package skillsdex

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"golang.org/x/term"
)

type apiClient struct {
	base  string
	token string
	http  *http.Client
}

const cliRequestTimeout = 30 * time.Second

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

func runLogin(args []string) error {
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	server := fs.String("server", "http://127.0.0.1:8765", "SkillsDex server URL")
	tokenFlag := fs.String("token", "", "API token")
	tokenStdin := fs.Bool("token-stdin", false, "read API token from stdin")
	_ = fs.Parse(args)

	token := strings.TrimSpace(*tokenFlag)
	if token != "" && *tokenStdin {
		return errors.New("--token and --token-stdin cannot be used together")
	}
	if *tokenStdin {
		read, err := readTokenFromStdin()
		if err != nil {
			return err
		}
		token = read
	}
	if token == "" {
		fmt.Print("API token: ")
		read, err := readTokenFromStdin()
		if err != nil {
			return err
		}
		token = read
	}
	if token == "" {
		return errors.New("API token is required")
	}
	cfg := AuthConfig{ServerURL: strings.TrimRight(*server, "/"), Token: token}
	client := newAPIClient(cfg)
	var me map[string]any
	if err := client.getJSON("/v1/me", &me); err != nil {
		return fmt.Errorf("token check failed: %w", err)
	}
	if err := saveAuth(cfg); err != nil {
		return err
	}
	fmt.Printf("Logged in to %s as %s\n", cfg.ServerURL, me["username"])
	return nil
}

func tokenStdinLoginCommand() string {
	return `printf '%s\n' "$SKILLSDEX_TOKEN" | skillsdex login --server http://127.0.0.1:8765 --token-stdin`
}

func readTokenFromStdin() (string, error) {
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func runLogout() error {
	path, err := authPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	fmt.Println("Logged out")
	return nil
}

func runWhoami() error {
	client, err := authenticatedClient()
	if err != nil {
		return err
	}
	var me map[string]any
	if err := client.getJSON("/v1/me", &me); err != nil {
		return err
	}
	fmt.Printf("%s on %s\n", me["username"], client.base)
	return nil
}

func runConfig(args []string) error {
	if len(args) < 1 {
		return errors.New("usage: skillsdex config (get default_target|set default_target codex)")
	}
	switch args[0] {
	case "get":
		if len(args) != 2 || args[1] != "default_target" {
			return errors.New("usage: skillsdex config get default_target")
		}
		target, err := resolveTarget("")
		if err != nil {
			return err
		}
		fmt.Println(target)
		return nil
	case "set":
		if len(args) != 3 || args[1] != "default_target" {
			return errors.New("usage: skillsdex config set default_target codex")
		}
		target := strings.TrimSpace(args[2])
		if err := validateTarget(target); err != nil {
			return err
		}
		cfg, err := loadLocalConfig()
		if err != nil {
			return err
		}
		cfg.DefaultTarget = target
		if err := saveLocalConfig(cfg); err != nil {
			return err
		}
		fmt.Printf("default_target = %s\n", target)
		return nil
	default:
		return errors.New("usage: skillsdex config (get default_target|set default_target codex)")
	}
}

func runSkills(args []string) error {
	opts, err := parseSkillsArgs(args)
	if err != nil {
		return errors.New("usage: skillsdex skills [--tag TAG] [--json]")
	}
	client, err := authenticatedClient()
	if err != nil {
		return err
	}
	var resp struct {
		Skills []Skill `json:"skills"`
	}
	path := "/v1/skills"
	if opts.Tag != "" {
		path += "?tag=" + url.QueryEscape(opts.Tag)
	}
	if err := client.getJSON(path, &resp); err != nil {
		return err
	}
	if opts.JSON {
		return printJSON(resp)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "IDENTIFIER\tNAME\tTAGS\tREVISION\tUPDATED")
	for _, s := range resp.Skills {
		fmt.Fprintf(tw, "%s/%s\t%s\t%s\t%d\t%s\n", s.OwnerSlug, s.Slug, s.Name, tagsText(s.Tags), s.RevisionNumber, s.UpdatedAt.Format("2006-01-02 15:04"))
	}
	return tw.Flush()
}

func runPublicSearch(args []string) error {
	query, limit, err := parsePublicSearchArgs(args)
	if err != nil {
		return errors.New("usage: skillsdex search <query> [--limit N]")
	}
	client, err := authenticatedClient()
	if err != nil {
		return err
	}
	var resp agentSkillSearchResponse
	path := "/v1/public/search?q=" + url.QueryEscape(query) + "&limit=" + url.QueryEscape(strconv.Itoa(limit))
	if err := client.getJSON(path, &resp); err != nil {
		return err
	}
	return printAgentSkillResults(resp.Results)
}

func runPublicTop(args []string) error {
	limit, err := parseLimitOnlyArgs(args, 20)
	if err != nil {
		return errors.New("usage: skillsdex top [--limit N]")
	}
	client, err := authenticatedClient()
	if err != nil {
		return err
	}
	var resp agentSkillSearchResponse
	if err := client.getJSON("/v1/public/top?limit="+url.QueryEscape(strconv.Itoa(limit)), &resp); err != nil {
		return err
	}
	return printAgentSkillResults(resp.Results)
}

func printAgentSkillResults(results []agentSkillSearchResult) error {
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SOURCE\tNAME\tQUALITY\tSECURITY\tSTARS")
	for _, result := range results {
		fmt.Fprintf(tw, "@%s/%s\t%s\t%s\t%s\t%d\n", result.Owner, result.Slug, result.Name, cliScore(result.ContentQualityScore), cliScore(result.SecurityScore), result.GitHubStars)
	}
	return tw.Flush()
}

func cliScore(score *int) string {
	if score == nil {
		return "-"
	}
	return strconv.Itoa(*score)
}

func runImportSource(args []string) error {
	opts, err := parseImportSourceArgs(args)
	if err != nil {
		return errors.New("usage: skillsdex import-source <github-url|agentskill-url|@owner/slug> [--install] [--target codex] [--dry-run] [--yes|--no]")
	}
	client, err := authenticatedClient()
	if err != nil {
		return err
	}
	var resp importSourceResponse
	if err := client.postJSON("/v1/import", map[string]string{"source": opts.Source}, &resp); err != nil {
		return err
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "IDENTIFIER\tNAME\tSTATUS")
	for _, result := range resp.Results {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", result.Identifier, result.Name, result.Status)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if !opts.Install {
		return nil
	}
	target, err := resolveTarget(opts.Target)
	if err != nil {
		return err
	}
	return installImportedResults(client, resp.Results, target, opts.Confirm)
}

func installImportedResults(client *apiClient, results []skillImportResult, target string, confirm confirmOptions) error {
	var identifiers []string
	for _, result := range results {
		if importResultInstallable(result) {
			identifiers = append(identifiers, result.Identifier)
		}
	}
	if len(identifiers) == 0 {
		fmt.Println("No successfully imported skills to install.")
		return nil
	}
	manifest, err := loadManifest()
	if err != nil {
		return err
	}
	installed := 0
	for _, identifier := range identifiers {
		owner, slug, err := parseIdentifier(identifier)
		if err != nil {
			return err
		}
		resp, err := client.fetchSkill(owner, slug)
		if err != nil {
			return err
		}
		if _, err := installSkillResponse(&manifest, resp, target, confirm); err != nil {
			return err
		}
		installed++
	}
	if confirm.DryRun {
		fmt.Printf("Would install %d imported skill(s).\n", installed)
		return nil
	}
	if err := saveManifest(manifest); err != nil {
		return err
	}
	fmt.Printf("Installed %d imported skill(s).\n", installed)
	return nil
}

func importResultInstallable(result skillImportResult) bool {
	switch strings.ToLower(strings.TrimSpace(result.Status)) {
	case "created", "updated", "unchanged":
		return strings.TrimSpace(result.Identifier) != ""
	default:
		return false
	}
}

func runPublish(args []string) error {
	opts, err := parsePublishArgs(args)
	if err != nil {
		return errors.New("usage: skillsdex publish PATH [--slug SLUG] [--apply|--dry-run] [--strict] [--yes|--no]")
	}
	client, err := authenticatedClient()
	if err != nil {
		return err
	}
	var me struct {
		Username string `json:"username"`
	}
	if err := client.getJSON("/v1/me", &me); err != nil {
		return err
	}
	pkg, err := skillInputFromLocalPath(opts.Path, me.Username, opts.Slug)
	if err != nil {
		return err
	}
	in := pkg.Input
	if err := validateSlug("slug", in.Slug); err != nil {
		return err
	}
	report := newLintReport(in.Owner+"/"+in.Slug, skillQuality(in))
	printQualityReport(report)
	if opts.Strict && report.WarningCount > 0 {
		return errors.New("quality warnings found")
	}
	exists, err := client.skillExists(in.Owner, in.Slug)
	if err != nil {
		return err
	}
	action := "create"
	if exists {
		action = "update"
	}
	fileCount := len(in.Files) + 1
	if !opts.Apply {
		fmt.Printf("Would %s %s/%s from %s (%d package file(s)).\n", action, in.Owner, in.Slug, opts.Path, fileCount)
		printLocalPackageFileSummary(in)
		printSkippedLocalPackagePaths(pkg.Skipped)
		fmt.Println("Next steps:")
		fmt.Printf("  skillsdex publish %s --apply --yes\n", shellQuote(opts.Path))
		return nil
	}
	promptAction := "Create"
	doneAction := "Created"
	if exists {
		promptAction = "Update"
		doneAction = "Updated"
	}
	if err := requireConfirmation(fmt.Sprintf("%s %s/%s from %s?", promptAction, in.Owner, in.Slug, opts.Path), opts.Confirm); err != nil {
		return err
	}
	var resp struct {
		Skill Skill `json:"skill"`
	}
	if exists {
		if err := client.putJSON("/v1/skills/"+url.PathEscape(in.Owner)+"/"+url.PathEscape(in.Slug), in, &resp); err != nil {
			return err
		}
	} else {
		if err := client.postJSON("/v1/skills", in, &resp); err != nil {
			return err
		}
	}
	fmt.Printf("%s %s/%s revision %d from %s (%d package file(s)).\n", doneAction, resp.Skill.OwnerSlug, resp.Skill.Slug, resp.Skill.RevisionNumber, opts.Path, fileCount)
	printLocalPackageFileSummary(in)
	printSkippedLocalPackagePaths(pkg.Skipped)
	return nil
}

func skillInputFromLocalPath(inputPath, owner, slugOverride string) (localSkillPackage, error) {
	info, err := os.Stat(inputPath)
	if err != nil {
		return localSkillPackage{}, err
	}
	if info.IsDir() {
		return skillInputFromLocalDir(inputPath, owner, slugOverride)
	}
	raw, err := os.ReadFile(inputPath)
	if err != nil {
		return localSkillPackage{}, err
	}
	if len(raw) > maxImportedSkillBytes {
		return localSkillPackage{}, fmt.Errorf("%s is too large", inputPath)
	}
	fallback := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	if slugOverride != "" {
		fallback = slugOverride
	}
	in := skillInputFromRawMarkdown(string(raw), owner, fallback, "")
	if slugOverride != "" {
		in.Slug = slugOverride
	}
	in.Owner = owner
	return localSkillPackage{Input: in}, nil
}

func skillInputFromLocalDir(dir, owner, slugOverride string) (localSkillPackage, error) {
	mainPath := filepath.Join(dir, "SKILL.md")
	raw, err := os.ReadFile(mainPath)
	if err != nil {
		return localSkillPackage{}, fmt.Errorf("read SKILL.md: %w", err)
	}
	if len(raw) > maxImportedSkillBytes {
		return localSkillPackage{}, fmt.Errorf("%s is too large", mainPath)
	}
	fallback := filepath.Base(dir)
	if slugOverride != "" {
		fallback = slugOverride
	}
	in := skillInputFromRawMarkdown(string(raw), owner, fallback, "")
	if slugOverride != "" {
		in.Slug = slugOverride
	}
	in.Owner = owner
	var total int64 = int64(len(raw))
	var skipped []string
	err = filepath.WalkDir(dir, func(pathName string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if pathName == dir || pathName == mainPath {
			return nil
		}
		rel, err := filepath.Rel(dir, pathName)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if shouldSkipLocalSkillPackageDir(entry.Name()) {
				skipped = append(skipped, rel+"/")
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s: symlink package files are not supported", pathName)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		if shouldSkipLocalSkillPackageFile(rel) {
			skipped = append(skipped, rel)
			return nil
		}
		if err := validateSkillFilePath(rel); err != nil {
			return err
		}
		data, err := os.ReadFile(pathName)
		if err != nil {
			return err
		}
		if len(data) > maxImportedSkillBytes {
			return fmt.Errorf("%s is too large", rel)
		}
		total += int64(len(data))
		if total > maxSkillsDexExportBytes {
			return errors.New("skill package is too large")
		}
		stat, err := entry.Info()
		if err != nil {
			return err
		}
		in.Files = append(in.Files, SkillFile{Path: rel, Content: string(data), ContentType: contentTypeForPath(rel), SizeBytes: int64(len(data)), Executable: stat.Mode()&0111 != 0})
		return nil
	})
	if err != nil {
		return localSkillPackage{}, err
	}
	sort.Slice(in.Files, func(i, j int) bool {
		return in.Files[i].Path < in.Files[j].Path
	})
	sort.Strings(skipped)
	return localSkillPackage{Input: in, Skipped: skipped}, nil
}

func printLocalPackageFileSummary(in SkillInput) {
	fmt.Println("Package files:")
	mainSize := len(renderSkillMarkdown(in))
	fmt.Printf("  SKILL.md (%d bytes)\n", mainSize)
	for _, file := range in.Files {
		size := file.SizeBytes
		if size == 0 && file.Content != "" {
			size = int64(len(file.Content))
		}
		mode := ""
		if file.Executable {
			mode = " executable"
		}
		fmt.Printf("  %s (%d bytes%s)\n", file.Path, size, mode)
	}
}

func printSkillPackageFileSummary(files []SkillFile) {
	if len(files) == 0 {
		return
	}
	fmt.Println("Package files:")
	for _, file := range files {
		size := file.SizeBytes
		if size == 0 && file.Content != "" {
			size = int64(len(file.Content))
		}
		mode := ""
		if file.Executable {
			mode = " executable"
		}
		fmt.Printf("  %s (%d bytes%s)\n", file.Path, size, mode)
	}
}

func printSkippedLocalPackagePaths(paths []string) {
	if len(paths) == 0 {
		return
	}
	fmt.Printf("Skipped %d local package path(s):\n", len(paths))
	for _, skippedPath := range paths {
		fmt.Printf("  %s\n", skippedPath)
	}
}

func shouldSkipLocalSkillPackageDir(name string) bool {
	switch name {
	case ".git", ".hg", ".svn", ".idea", ".vscode", "__pycache__", "node_modules":
		return true
	default:
		return false
	}
}

func shouldSkipLocalSkillPackageFile(rel string) bool {
	base := path.Base(rel)
	if base == ".DS_Store" || base == "Thumbs.db" || base == ".env" || base == ".envrc" {
		return true
	}
	if strings.HasPrefix(base, ".env.") && base != ".env.example" && base != ".env.sample" {
		return true
	}
	if strings.HasSuffix(base, "~") || strings.HasSuffix(base, ".swp") || strings.HasSuffix(base, ".swo") || strings.HasSuffix(base, ".tmp") {
		return true
	}
	return false
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if strings.ContainsAny(value, " \t\n'\"\\$`") {
		return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
	}
	return value
}

func runRefresh(args []string) error {
	target, all, apply, strict, showDiff, jsonOutput, confirm, err := parseRefreshArgs(args)
	if err != nil {
		return errors.New("usage: skillsdex refresh (--all | <owner>/<slug>) [--apply|--dry-run] [--strict] [--diff] [--json] [--yes|--no]")
	}
	client, err := authenticatedClient()
	if err != nil {
		return err
	}
	dryRun := !apply
	if jsonOutput && !dryRun {
		return errors.New("--json is preview-only for refresh; omit --apply")
	}
	if all {
		skills, err := refreshableSkills(client)
		if err != nil {
			return err
		}
		if jsonOutput {
			return runRefreshAllJSON(client, skills, strict)
		}
		if len(skills) == 0 {
			fmt.Println("No active imported skills with source URLs found.")
			return nil
		}
		if dryRun {
			fmt.Printf("Previewing source refresh for %d imported skill(s). Use --apply --yes to update changed skills.\n", len(skills))
		} else if err := requireConfirmation(fmt.Sprintf("Refresh %d imported skill(s) from upstream?", len(skills)), confirm); err != nil {
			return err
		}
		statusCounts := map[string]int{}
		failures := 0
		for _, skill := range skills {
			resp, err := client.refreshSkill(skill.OwnerSlug, skill.Slug, dryRun)
			if err != nil {
				failures++
				fmt.Fprintf(os.Stderr, "%s/%s: refresh failed: %v\n", skill.OwnerSlug, skill.Slug, err)
				continue
			}
			statusCounts[resp.Status]++
			fmt.Printf("%s/%s: %s\n", resp.Skill.OwnerSlug, resp.Skill.Slug, resp.Status)
			printSourceRefreshChanges(resp.Changes, showDiff)
		}
		printRefreshNextSteps(dryRun, statusCounts, "skillsdex refresh --all --apply --yes")
		if failures > 0 {
			return fmt.Errorf("%d source refresh(es) failed", failures)
		}
		if strict && statusCounts["update available"] > 0 {
			return fmt.Errorf("%d source update(s) available", statusCounts["update available"])
		}
		return nil
	}
	owner, slug, err := parseIdentifier(target)
	if err != nil {
		return err
	}
	if err := validateSlug("owner", owner); err != nil {
		return err
	}
	if err := validateSlug("slug", slug); err != nil {
		return err
	}
	if jsonOutput {
		return runRefreshOneJSON(client, owner, slug, strict)
	}
	if dryRun {
		fmt.Println("Previewing source refresh. Use --apply --yes to update if upstream changed.")
	} else if err := requireConfirmation(fmt.Sprintf("Refresh %s/%s from upstream?", owner, slug), confirm); err != nil {
		return err
	}
	resp, err := client.refreshSkill(owner, slug, dryRun)
	if err != nil {
		return err
	}
	fmt.Printf("%s/%s: %s\n", resp.Skill.OwnerSlug, resp.Skill.Slug, resp.Status)
	printSourceRefreshChanges(resp.Changes, showDiff)
	printRefreshNextSteps(dryRun, map[string]int{resp.Status: 1}, fmt.Sprintf("skillsdex refresh %s/%s --apply --yes", owner, slug))
	if strict && resp.Status == "update available" {
		return errors.New("source update available")
	}
	return nil
}

func runRefreshOneJSON(client *apiClient, owner, slug string, strict bool) error {
	resp, err := client.refreshSkill(owner, slug, true)
	if err != nil {
		return err
	}
	report := newRefreshReport(true, strict, false, owner+"/"+slug)
	addRefreshResponseToReport(&report, resp)
	report.NextSteps = refreshNextSteps(report.StatusCounts, fmt.Sprintf("skillsdex refresh %s/%s --apply --yes", owner, slug))
	report.Healthy = report.UpdateCount == 0 && report.FailureCount == 0
	if err := printRefreshReportJSON(report); err != nil {
		return err
	}
	if strict && report.UpdateCount > 0 {
		return errors.New("source update available")
	}
	return nil
}

func runRefreshAllJSON(client *apiClient, skills []Skill, strict bool) error {
	report := newRefreshReport(true, strict, true, "")
	if len(skills) == 0 {
		report.Healthy = true
		return printRefreshReportJSON(report)
	}
	for _, skill := range skills {
		resp, err := client.refreshSkill(skill.OwnerSlug, skill.Slug, true)
		if err != nil {
			report.FailureCount++
			report.Results = append(report.Results, refreshResult{
				Identifier: skill.OwnerSlug + "/" + skill.Slug,
				Status:     "refresh failed",
				Error:      err.Error(),
			})
			report.StatusCounts["refresh failed"]++
			continue
		}
		addRefreshResponseToReport(&report, resp)
	}
	report.NextSteps = refreshNextSteps(report.StatusCounts, "skillsdex refresh --all --apply --yes")
	report.Healthy = report.UpdateCount == 0 && report.FailureCount == 0
	if err := printRefreshReportJSON(report); err != nil {
		return err
	}
	if report.FailureCount > 0 {
		return fmt.Errorf("%d source refresh(es) failed", report.FailureCount)
	}
	if strict && report.UpdateCount > 0 {
		return fmt.Errorf("%d source update(s) available", report.UpdateCount)
	}
	return nil
}

func newRefreshReport(dryRun, strict, all bool, target string) refreshReport {
	return refreshReport{
		DryRun:       dryRun,
		Strict:       strict,
		All:          all,
		Target:       target,
		StatusCounts: map[string]int{},
	}
}

func addRefreshResponseToReport(report *refreshReport, resp refreshResponse) {
	identifier := resp.Skill.OwnerSlug + "/" + resp.Skill.Slug
	report.CheckedCount++
	report.StatusCounts[resp.Status]++
	if resp.Status == "update available" {
		report.UpdateCount++
	}
	skill := resp.Skill
	report.Results = append(report.Results, refreshResult{
		Identifier: identifier,
		Status:     resp.Status,
		Skill:      &skill,
		Changes:    resp.Changes,
	})
}

func printRefreshReportJSON(report refreshReport) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func printSourceRefreshChanges(changes []SourceRefreshChange, showDiff bool) {
	if len(changes) == 0 {
		return
	}
	for _, change := range changes {
		fmt.Printf("  %s\t%s\n", change.Status, change.Path)
		if showDiff && strings.TrimSpace(change.Diff) != "" {
			fmt.Print(indentBlock(change.Diff, "    "))
			if !strings.HasSuffix(change.Diff, "\n") {
				fmt.Println()
			}
		}
	}
}

func indentBlock(value, prefix string) string {
	if value == "" {
		return ""
	}
	lines := strings.SplitAfter(value, "\n")
	var b strings.Builder
	for _, line := range lines {
		if line == "" {
			continue
		}
		b.WriteString(prefix)
		b.WriteString(line)
	}
	return b.String()
}

func printRefreshNextSteps(dryRun bool, statusCounts map[string]int, applyCommand string) {
	steps := refreshNextStepsForMode(dryRun, statusCounts, applyCommand)
	if len(steps) == 0 {
		return
	}
	fmt.Println("Next steps:")
	for _, step := range steps {
		fmt.Println("  " + step)
	}
}

func refreshNextSteps(statusCounts map[string]int, applyCommand string) []string {
	return refreshNextStepsForMode(true, statusCounts, applyCommand)
}

func refreshNextStepsForMode(dryRun bool, statusCounts map[string]int, applyCommand string) []string {
	if dryRun {
		if statusCounts["update available"] > 0 {
			return []string{applyCommand, "skillsdex sync --dry-run --strict"}
		}
		return nil
	}
	if statusCounts["updated from source"] > 0 {
		return []string{"skillsdex sync --dry-run --strict"}
	}
	return nil
}

func refreshableSkills(client *apiClient) ([]Skill, error) {
	var list struct {
		Skills []Skill `json:"skills"`
	}
	if err := client.getJSON("/v1/skills", &list); err != nil {
		return nil, err
	}
	var skills []Skill
	for _, skill := range list.Skills {
		if skill.ArchivedAt != "" || strings.TrimSpace(skill.SourceURL) == "" {
			continue
		}
		skills = append(skills, skill)
	}
	sort.Slice(skills, func(i, j int) bool {
		return skills[i].OwnerSlug+"/"+skills[i].Slug < skills[j].OwnerSlug+"/"+skills[j].Slug
	})
	return skills, nil
}

func runSets(args []string) error {
	opts, err := parseSetsArgs(args)
	if err != nil {
		return errors.New("usage: skillsdex sets [set-slug] [--json]")
	}
	client, err := authenticatedClient()
	if err != nil {
		return err
	}
	if opts.Slug == "" {
		var resp installSetsResponse
		if err := client.getJSON("/v1/install-sets", &resp); err != nil {
			return err
		}
		if opts.JSON {
			return printJSON(resp)
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "SLUG\tNAME\tSKILLS\tUPDATED\tDESCRIPTION")
		for _, set := range resp.Sets {
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\n", set.Slug, set.Name, set.SkillCount, set.UpdatedAt.Format("2006-01-02 15:04"), set.Description)
		}
		return tw.Flush()
	}
	if err := validateSlug("set slug", opts.Slug); err != nil {
		return err
	}
	resp, err := client.fetchInstallSet(opts.Slug)
	if err != nil {
		return err
	}
	if opts.JSON {
		return printJSON(resp)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SET\tNAME\tSKILLS\tUPDATED")
	fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n\n", resp.Set.Slug, resp.Set.Name, resp.Set.SkillCount, resp.Set.UpdatedAt.Format("2006-01-02 15:04"))
	fmt.Fprintln(tw, "IDENTIFIER\tNAME\tREVISION\tUPDATED")
	for _, skill := range resp.Skills {
		fmt.Fprintf(tw, "%s/%s\t%s\t%d\t%s\n", skill.OwnerSlug, skill.Slug, skill.Name, skill.RevisionNumber, skill.UpdatedAt.Format("2006-01-02 15:04"))
	}
	return tw.Flush()
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func runGitHub(args []string) error {
	if len(args) == 0 || args[0] != "export" {
		return errors.New("usage: skillsdex github export [--dir PATH] [--yes|--no]")
	}
	dir, confirm, err := parseGitHubExportArgs(args[1:])
	if err != nil {
		return errors.New("usage: skillsdex github export [--dir PATH] [--yes|--no]")
	}
	client, err := authenticatedClient()
	if err != nil {
		return err
	}
	data, err := client.getBytes("/v1/export/github")
	if err != nil {
		return err
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return errors.New("server returned an invalid GitHub export zip")
	}
	written, err := extractZipToDir(zr, dir, confirm)
	if err != nil {
		return err
	}
	fmt.Printf("Exported %d file(s) to %s\n", written, dir)
	return nil
}

func runBackup(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: skillsdex backup export --out PATH | skillsdex backup inspect --file PATH [--json] | skillsdex backup restore --file PATH [--dry-run] [--json]")
	}
	switch args[0] {
	case "export":
		out, err := parseBackupExportArgs(args[1:])
		if err != nil {
			return errors.New("usage: skillsdex backup export --out PATH")
		}
		client, err := authenticatedClient()
		if err != nil {
			return err
		}
		data, err := client.getBytes("/v1/backup")
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(out), 0700); err != nil {
			return err
		}
		if err := os.WriteFile(out, data, 0600); err != nil {
			return err
		}
		fmt.Printf("Exported backup to %s\n", out)
		return nil
	case "inspect":
		opts, err := parseBackupFileArgs(args[1:], false)
		if err != nil {
			return errors.New("usage: skillsdex backup inspect --file PATH [--json]")
		}
		data, err := readLocalBackupFile(opts.File)
		if err != nil {
			return err
		}
		summary, err := inspectSkillsDexBackup(data)
		if err != nil {
			return err
		}
		if opts.JSON {
			return printBackupInspectSummaryJSON(opts.File, false, summary)
		}
		return printBackupInspectSummary(opts.File, summary)
	case "restore":
		opts, err := parseBackupFileArgs(args[1:], true)
		if err != nil {
			return errors.New("usage: skillsdex backup restore --file PATH [--dry-run] [--json]")
		}
		data, err := readLocalBackupFile(opts.File)
		if err != nil {
			return err
		}
		if opts.DryRun {
			summary, err := inspectSkillsDexBackup(data)
			if err != nil {
				return err
			}
			if opts.JSON {
				return printBackupInspectSummaryJSON(opts.File, true, summary)
			}
			fmt.Println("Restore dry-run: backup was not uploaded.")
			return printBackupInspectSummary(opts.File, summary)
		}
		if opts.JSON {
			return errors.New("--json requires --dry-run for backup restore")
		}
		client, err := authenticatedClient()
		if err != nil {
			return err
		}
		var resp importSourceResponse
		if err := client.postBytes("/v1/backup", data, "application/zip", &resp); err != nil {
			return err
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "IDENTIFIER\tNAME\tSTATUS")
		for _, result := range resp.Results {
			fmt.Fprintf(tw, "%s\t%s\t%s\n", result.Identifier, result.Name, result.Status)
		}
		return tw.Flush()
	default:
		return errors.New("usage: skillsdex backup export --out PATH | skillsdex backup inspect --file PATH [--json] | skillsdex backup restore --file PATH [--dry-run] [--json]")
	}
}

type backupInspectSummary struct {
	ExportedAt      string             `json:"exported_at"`
	User            string             `json:"user"`
	SkillCount      int                `json:"skill_count"`
	ActiveCount     int                `json:"active_count"`
	ArchivedCount   int                `json:"archived_count"`
	PinnedCount     int                `json:"pinned_count"`
	Recommended     int                `json:"recommended_count"`
	SourceBacked    int                `json:"source_backed_count"`
	SupportingFiles int                `json:"supporting_file_count"`
	Rows            []backupInspectRow `json:"skills"`
}

type backupInspectRow struct {
	Identifier string `json:"identifier"`
	Name       string `json:"name"`
	Revision   int    `json:"revision"`
	Status     string `json:"status"`
	FileCount  int    `json:"file_count"`
	Flags      string `json:"flags"`
	Source     string `json:"source"`
}

type backupInspectOutput struct {
	File          string               `json:"file"`
	Valid         bool                 `json:"valid"`
	RestoreDryRun bool                 `json:"restore_dry_run"`
	Summary       backupInspectSummary `json:"summary"`
}

func readLocalBackupFile(file string) ([]byte, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxSkillsDexExportBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSkillsDexExportBytes {
		return nil, fmt.Errorf("backup file is too large; limit is %d MB", maxSkillsDexExportBytes/(1<<20))
	}
	return data, nil
}

func inspectSkillsDexBackup(data []byte) (backupInspectSummary, error) {
	manifest, err := backupManifestFromData(data)
	if err != nil {
		return backupInspectSummary{}, err
	}
	restored, err := importSkillInputsFromSkillsDexExport(data, "inspect")
	if err != nil {
		return backupInspectSummary{}, err
	}
	summary := backupInspectSummary{
		ExportedAt: manifest.ExportedAt,
		User:       manifest.User,
		SkillCount: len(restored),
	}
	for i, item := range restored {
		in := item.Input
		status := "active"
		if item.Archived {
			status = "archived"
			summary.ArchivedCount++
		} else {
			summary.ActiveCount++
		}
		if in.PinnedInstall {
			summary.PinnedCount++
		}
		if in.RecommendedDefault {
			summary.Recommended++
		}
		if strings.TrimSpace(in.SourceProvider) != "" || strings.TrimSpace(in.SourceURL) != "" || strings.TrimSpace(in.SourceAuthor) != "" || strings.TrimSpace(in.SourceName) != "" {
			summary.SourceBacked++
		}
		summary.SupportingFiles += len(in.Files)
		meta := exportSkillMeta{}
		if i < len(manifest.Skills) {
			meta = manifest.Skills[i]
		}
		summary.Rows = append(summary.Rows, backupInspectRow{
			Identifier: meta.Identifier,
			Name:       in.Name,
			Revision:   meta.RevisionNumber,
			Status:     status,
			FileCount:  1 + len(in.Files),
			Flags:      backupFlags(in),
			Source:     backupSourceLabel(in),
		})
	}
	return summary, nil
}

func backupManifestFromData(data []byte) (exportManifest, error) {
	if len(data) == 0 {
		return exportManifest{}, errors.New("backup upload is empty")
	}
	if len(data) > maxSkillsDexExportBytes {
		return exportManifest{}, fmt.Errorf("backup upload is too large; limit is %d MB", maxSkillsDexExportBytes/(1<<20))
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return exportManifest{}, errors.New("invalid SkillsDex backup zip")
	}
	var manifestFile *zip.File
	for _, f := range zr.File {
		if f.Name == "skillsdex-export.json" {
			manifestFile = f
			break
		}
	}
	if manifestFile == nil {
		return exportManifest{}, errors.New("backup is missing skillsdex-export.json")
	}
	manifestBytes, err := readZipFileLimited(manifestFile, maxImportedSkillBytes)
	if err != nil {
		return exportManifest{}, err
	}
	var manifest exportManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return exportManifest{}, errors.New("backup manifest is invalid")
	}
	return manifest, nil
}

func backupFlags(in SkillInput) string {
	var flags []string
	if in.PinnedInstall {
		flags = append(flags, "pinned")
	}
	if in.RecommendedDefault {
		flags = append(flags, "recommended")
	}
	if len(flags) == 0 {
		return "-"
	}
	return strings.Join(flags, ",")
}

func backupSourceLabel(in SkillInput) string {
	label := sourceDisplayLabel(&Skill{
		SourceProvider: in.SourceProvider,
		SourceURL:      in.SourceURL,
		SourceAuthor:   in.SourceAuthor,
		SourceName:     in.SourceName,
		SourcePath:     in.SourcePath,
		SourceRef:      in.SourceRef,
	})
	if label != "Manual" {
		return label
	}
	if strings.TrimSpace(in.SourceURL) != "" {
		return "source"
	}
	return "-"
}

func printBackupInspectSummary(file string, summary backupInspectSummary) error {
	fmt.Printf("Backup is valid: %s\n", file)
	if summary.User != "" || summary.ExportedAt != "" {
		fmt.Printf("Exported by %s at %s\n", valueOrDash(summary.User), valueOrDash(summary.ExportedAt))
	}
	fmt.Printf("%d skill(s): %d active, %d archived, %d pinned, %d recommended, %d source-backed, %d supporting file(s)\n\n",
		summary.SkillCount,
		summary.ActiveCount,
		summary.ArchivedCount,
		summary.PinnedCount,
		summary.Recommended,
		summary.SourceBacked,
		summary.SupportingFiles,
	)
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "IDENTIFIER\tNAME\tREVISION\tSTATUS\tFILES\tFLAGS\tSOURCE")
	for _, row := range summary.Rows {
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%d\t%s\t%s\n", row.Identifier, row.Name, row.Revision, row.Status, row.FileCount, row.Flags, row.Source)
	}
	return tw.Flush()
}

func printBackupInspectSummaryJSON(file string, restoreDryRun bool, summary backupInspectSummary) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(backupInspectOutput{
		File:          file,
		Valid:         true,
		RestoreDryRun: restoreDryRun,
		Summary:       summary,
	})
}

func valueOrDash(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return value
}

func runAdmin(args []string) error {
	if len(args) == 0 || args[0] != "reset-password" {
		return errors.New("usage: skillsdex admin reset-password [--db PATH] [--username admin] [--password PASSWORD]")
	}
	fs := flag.NewFlagSet("admin reset-password", flag.ExitOnError)
	dbPath := fs.String("db", "", "SQLite database path")
	username := fs.String("username", "admin", "admin username")
	passwordFlag := fs.String("password", "", "new password")
	_ = fs.Parse(args[1:])
	if fs.NArg() != 0 {
		return errors.New("usage: skillsdex admin reset-password [--db PATH] [--username admin] [--password PASSWORD]")
	}
	password, err := passwordFromFlagOrPrompt(*passwordFlag)
	if err != nil {
		return err
	}
	if strings.TrimSpace(*dbPath) == "" {
		*dbPath, err = defaultDBPath()
		if err != nil {
			return err
		}
	}
	db, err := openDB(*dbPath)
	if err != nil {
		return dbPathError(*dbPath, err)
	}
	defer db.Close()
	ctx := context.Background()
	u, err := getUserByUsername(ctx, db, strings.TrimSpace(*username))
	if err != nil {
		return err
	}
	if !u.IsAdmin {
		return fmt.Errorf("%s is not an admin user", u.Username)
	}
	if err := setUserPassword(ctx, db, u.ID, password); err != nil {
		return err
	}
	if err := revokeUserAccess(ctx, db, u.ID); err != nil {
		return err
	}
	_ = createAuditEvent(ctx, db, AuditEvent{
		Action:        "password_reset_local",
		TargetType:    "user",
		TargetID:      u.ID,
		TargetLabel:   u.Username,
		ActorUsername: "local-cli",
		DetailJSON:    `{"sessions_revoked":true,"api_tokens_revoked":true}`,
	})
	fmt.Printf("Reset password for %s and revoked existing sessions/API tokens.\n", u.Username)
	return nil
}

func defaultDBPath() (string, error) {
	dir, err := skillsdexDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "skillsdex.db"), nil
}

func passwordFromFlagOrPrompt(password string) (string, error) {
	password = strings.TrimSpace(password)
	if password != "" {
		return password, nil
	}
	if !isTerminal(os.Stdin) {
		return "", errors.New("password is required in non-interactive mode")
	}
	fmt.Print("New password: ")
	first, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", err
	}
	fmt.Print("Confirm password: ")
	second, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", err
	}
	if string(first) != string(second) {
		return "", errors.New("passwords do not match")
	}
	return strings.TrimSpace(string(first)), nil
}

func runShow(args []string) error {
	opts, err := parseShowArgs(args)
	if err != nil {
		return errors.New("usage: skillsdex show <owner>/<slug> [--raw|--file PATH]")
	}
	owner, slug, err := parseIdentifier(opts.Identifier)
	if err != nil {
		return err
	}
	client, err := authenticatedClient()
	if err != nil {
		return err
	}
	resp, err := client.fetchSkill(owner, slug)
	if err != nil {
		return err
	}
	if opts.Raw {
		fmt.Print(resp.Raw)
		return nil
	}
	if opts.FilePath != "" {
		content, err := skillResponseFileContent(resp, opts.FilePath)
		if err != nil {
			return err
		}
		fmt.Print(content)
		return nil
	}
	s := resp.Skill
	fmt.Printf("Identifier: %s/%s\nName: %s\nDescription: %s\nShort description: %s\nRevision: %d\nUpdated: %s\n",
		s.OwnerSlug, s.Slug, s.Name, s.Description, s.ShortDescription, s.RevisionNumber, s.UpdatedAt.Format("2006-01-02 15:04"))
	if s.PinnedInstall {
		fmt.Println("Pinned install: yes")
	}
	if s.SourceProvider != "" || s.SourceURL != "" || s.SourceAuthor != "" || s.SourceName != "" || s.SourcePath != "" {
		source := sourceDisplayLabel(&s)
		if s.SourceURL != "" {
			source += " (" + s.SourceURL + ")"
		}
		fmt.Printf("Source: %s\n", source)
	}
	if s.ArchivedAt != "" {
		fmt.Printf("Archived: %s\n", s.ArchivedAt)
	}
	var supporting []SkillFile
	for _, file := range resp.Files {
		if file.Path != "SKILL.md" {
			supporting = append(supporting, file)
		}
	}
	if len(supporting) > 0 {
		fmt.Println("Supporting files:")
		for _, file := range supporting {
			mode := ""
			if file.Executable {
				mode = " executable"
			}
			fmt.Printf("  %s (%d bytes%s)\n", file.Path, file.SizeBytes, mode)
		}
	}
	return nil
}

func skillResponseFileContent(resp skillResponse, filePath string) (string, error) {
	if err := validateSkillFilePath(filePath); err != nil {
		return "", err
	}
	if filePath == "SKILL.md" {
		return resp.Raw, nil
	}
	for _, file := range resp.Files {
		if file.Path == filePath {
			return file.Content, nil
		}
	}
	return "", fmt.Errorf("package file %q not found", filePath)
}

func runMark(args []string) error {
	opts, err := parseMarkArgs(args)
	if err != nil {
		return errors.New("usage: skillsdex mark <owner>/<slug> [--pinned|--unpinned] [--recommended|--not-recommended] [--apply|--dry-run] [--yes|--no]")
	}
	owner, slug, err := parseIdentifier(opts.Identifier)
	if err != nil {
		return err
	}
	client, err := authenticatedClient()
	if err != nil {
		return err
	}
	resp, err := client.fetchSkill(owner, slug)
	if err != nil {
		return err
	}
	current := resp.Skill
	nextPinned := current.PinnedInstall
	nextRecommended := current.RecommendedDefault
	if opts.Pinned != nil {
		nextPinned = *opts.Pinned
	}
	if opts.Recommended != nil {
		nextRecommended = *opts.Recommended
	}
	changes := skillFlagChanges(current, nextPinned, nextRecommended)
	identifier := current.OwnerSlug + "/" + current.Slug
	if len(changes) == 0 {
		fmt.Printf("%s already has pinned=%s, recommended=%s.\n", identifier, yesNo(current.PinnedInstall), yesNo(current.RecommendedDefault))
		return nil
	}
	if !opts.Apply {
		fmt.Printf("Would update %s install flags:\n", identifier)
		for _, change := range changes {
			fmt.Println("  " + change)
		}
		fmt.Println("Next steps:")
		fmt.Printf("  %s\n", markApplyCommand(identifier, opts))
		return nil
	}
	if err := requireConfirmation("Update install flags for "+identifier+"?", opts.Confirm); err != nil {
		return err
	}
	updated, err := client.updateSkillFlags(owner, slug, SkillFlagUpdate{PinnedInstall: &nextPinned, RecommendedDefault: &nextRecommended})
	if err != nil {
		return err
	}
	fmt.Printf("Updated %s: pinned=%s, recommended=%s.\n", updated.OwnerSlug+"/"+updated.Slug, yesNo(updated.PinnedInstall), yesNo(updated.RecommendedDefault))
	return nil
}

func skillFlagChanges(current Skill, nextPinned, nextRecommended bool) []string {
	var changes []string
	if current.PinnedInstall != nextPinned {
		changes = append(changes, fmt.Sprintf("Pinned install: %s -> %s", yesNo(current.PinnedInstall), yesNo(nextPinned)))
	}
	if current.RecommendedDefault != nextRecommended {
		changes = append(changes, fmt.Sprintf("Recommended default: %s -> %s", yesNo(current.RecommendedDefault), yesNo(nextRecommended)))
	}
	return changes
}

func markApplyCommand(identifier string, opts markOptions) string {
	parts := []string{"skillsdex", "mark", shellQuote(identifier)}
	if opts.Pinned != nil {
		if *opts.Pinned {
			parts = append(parts, "--pinned")
		} else {
			parts = append(parts, "--unpinned")
		}
	}
	if opts.Recommended != nil {
		if *opts.Recommended {
			parts = append(parts, "--recommended")
		} else {
			parts = append(parts, "--not-recommended")
		}
	}
	parts = append(parts, "--apply", "--yes")
	return strings.Join(parts, " ")
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func runNewSkill(args []string) error {
	opts, err := parseNewSkillArgs(args)
	if err != nil {
		return errors.New("usage: skillsdex new PATH [--template workflow|review|research] [--slug SLUG] [--name NAME] [--with-folders] [--yes|--no]")
	}
	in, err := newSkillInput(opts)
	if err != nil {
		return err
	}
	if err := writeNewSkill(opts, in); err != nil {
		return err
	}
	fmt.Printf("Created %s/SKILL.md\n", opts.Path)
	fmt.Println("Next steps:")
	fmt.Printf("  skillsdex lint --file %s\n", shellQuote(opts.Path))
	fmt.Printf("  skillsdex publish %s\n", shellQuote(opts.Path))
	return nil
}

func runTemplates(args []string) error {
	if len(args) != 0 {
		return errors.New("usage: skillsdex templates")
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TEMPLATE\tNAME\tDEFAULT SLUG\tDESCRIPTION")
	for _, option := range skillTemplateOptions("local") {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", option.Key, option.Label, option.Slug, option.Description)
	}
	return tw.Flush()
}

func newSkillInput(opts newSkillOptions) (SkillInput, error) {
	tmpl := skillTemplate(opts.Template, "local")
	if tmpl.Body == "" {
		return SkillInput{}, fmt.Errorf("unknown template %q; choose one of: %s", opts.Template, skillTemplateKeysText())
	}
	slug := opts.Slug
	if slug == "" {
		slug = slugify(filepath.Base(filepath.Clean(opts.Path)))
	}
	if slug == "" || slug == "." {
		slug = tmpl.Slug
	}
	if err := validateSlug("slug", slug); err != nil {
		return SkillInput{}, err
	}
	tmpl.Owner = "local"
	tmpl.Slug = slug
	if opts.Name != "" {
		tmpl.Name = opts.Name
	} else {
		tmpl.Name = humanizeSlug(slug)
	}
	return tmpl, nil
}

func writeNewSkill(opts newSkillOptions, in SkillInput) error {
	if info, err := os.Stat(opts.Path); err == nil && !info.IsDir() {
		return fmt.Errorf("%s exists and is not a directory", opts.Path)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(opts.Path, 0700); err != nil {
		return err
	}
	skillPath := filepath.Join(opts.Path, "SKILL.md")
	content := []byte(renderSkillMarkdown(in))
	if err := confirmFileOverwrite(skillPath, content, opts.Confirm); err != nil {
		return err
	}
	if err := os.WriteFile(skillPath, content, 0600); err != nil {
		return err
	}
	if opts.WithFolders {
		for _, dir := range []string{"references", "scripts", "assets", "templates"} {
			if err := os.MkdirAll(filepath.Join(opts.Path, dir), 0700); err != nil {
				return err
			}
		}
	}
	return nil
}

func humanizeSlug(slug string) string {
	parts := strings.Split(slug, "-")
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + part[1:]
	}
	return strings.Join(parts, " ")
}

func runLint(args []string) error {
	target, filePath, all, strict, jsonOutput, err := parseLintArgs(args)
	if err != nil {
		return errors.New("usage: skillsdex lint [--strict] [--json] (--all | --file PATH | <owner>/<slug>)")
	}
	if all {
		reports, err := lintAllRemoteSkills()
		if err != nil {
			return err
		}
		if jsonOutput {
			if err := printQualityReportsJSON(reports); err != nil {
				return err
			}
		} else {
			printQualityReports(reports)
		}
		if strict {
			return strictLintError(reports)
		}
		return nil
	}

	report, err := lintOneSkill(target, filePath)
	if err != nil {
		return err
	}
	if jsonOutput {
		if err := printQualityReportJSON(report); err != nil {
			return err
		}
	} else {
		printQualityReport(report)
	}
	if strict {
		return strictLintError([]lintReport{report})
	}
	return nil
}

func lintOneSkill(target, filePath string) (lintReport, error) {
	var in SkillInput
	var ctx SkillQualityContext
	label := target
	if filePath != "" {
		pkg, err := skillInputFromLocalPath(filePath, "local", "")
		if err != nil {
			return lintReport{}, err
		}
		in = pkg.Input
		if label == "" {
			label = filePath
		}
	} else {
		owner, slug, err := parseIdentifier(target)
		if err != nil {
			return lintReport{}, err
		}
		client, err := authenticatedClient()
		if err != nil {
			return lintReport{}, err
		}
		resp, err := client.fetchSkill(owner, slug)
		if err != nil {
			return lintReport{}, err
		}
		in = skillInputFromResponse(resp)
		var list struct {
			Skills []Skill `json:"skills"`
		}
		_ = client.getJSON("/v1/skills", &list)
		ctx = qualityContextForSkill(resp.Skill, list.Skills)
	}
	return newLintReport(label, skillQualityWithContext(in, ctx)), nil
}

func lintAllRemoteSkills() ([]lintReport, error) {
	client, err := authenticatedClient()
	if err != nil {
		return nil, err
	}
	var list struct {
		Skills []Skill `json:"skills"`
	}
	if err := client.getJSON("/v1/skills", &list); err != nil {
		return nil, err
	}
	skills := append([]Skill(nil), list.Skills...)
	sort.Slice(skills, func(i, j int) bool {
		return skills[i].OwnerSlug+"/"+skills[i].Slug < skills[j].OwnerSlug+"/"+skills[j].Slug
	})
	reports := make([]lintReport, 0, len(skills))
	for _, skill := range skills {
		if skill.ArchivedAt != "" {
			continue
		}
		resp, err := client.fetchSkill(skill.OwnerSlug, skill.Slug)
		if err != nil {
			return nil, fmt.Errorf("%s/%s: %w", skill.OwnerSlug, skill.Slug, err)
		}
		report := newLintReport(skill.OwnerSlug+"/"+skill.Slug, skillQualityWithContext(skillInputFromResponse(resp), qualityContextForSkill(resp.Skill, list.Skills)))
		reports = append(reports, report)
	}
	return reports, nil
}

func skillInputFromResponse(resp skillResponse) SkillInput {
	return SkillInput{
		Owner:              resp.Skill.OwnerSlug,
		Slug:               resp.Skill.Slug,
		Name:               resp.Skill.Name,
		Description:        resp.Skill.Description,
		ShortDescription:   resp.Skill.ShortDescription,
		RecommendedDefault: resp.Skill.RecommendedDefault,
		PinnedInstall:      resp.Skill.PinnedInstall,
		SourceProvider:     resp.Skill.SourceProvider,
		SourceURL:          resp.Skill.SourceURL,
		SourceAuthor:       resp.Skill.SourceAuthor,
		SourceName:         resp.Skill.SourceName,
		SourcePath:         resp.Skill.SourcePath,
		SourceRef:          resp.Skill.SourceRef,
		Tags:               resp.Skill.Tags,
		TagsExplicit:       true,
		Body:               bodyFromSkillMarkdown(resp.Raw),
		Files:              supportingFilesOnly(resp.Files),
	}
}

func qualityContextForSkill(skill Skill, skills []Skill) SkillQualityContext {
	return SkillQualityContext{
		CurrentIdentifier: skill.OwnerSlug + "/" + skill.Slug,
		SourceProvider:    skill.SourceProvider,
		SourceURL:         skill.SourceURL,
		UpdatedAt:         skill.UpdatedAt,
		Skills:            skills,
	}
}

func newLintReport(label string, issues []SkillQualityIssue) lintReport {
	report := lintReport{Target: label, Summary: qualitySummary(issues), IssueCount: len(issues), Issues: issues}
	for _, issue := range issues {
		if issue.Severity == "warn" {
			report.WarningCount++
		}
	}
	return report
}

func strictLintError(reports []lintReport) error {
	for _, report := range reports {
		if report.WarningCount > 0 {
			return errors.New("quality warnings found")
		}
	}
	return nil
}

func printQualityReportJSON(report lintReport) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func printQualityReportsJSON(reports []lintReport) error {
	aggregate := aggregateLintReports(reports)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(aggregate)
}

func aggregateLintReports(reports []lintReport) lintReports {
	aggregate := lintReports{Summary: "ready", SkillCount: len(reports), Reports: reports}
	for _, report := range reports {
		aggregate.IssueCount += report.IssueCount
		aggregate.WarningCount += report.WarningCount
		if report.Summary == "needs attention" {
			aggregate.Summary = "needs attention"
		} else if report.Summary == "review" && aggregate.Summary == "ready" {
			aggregate.Summary = "review"
		}
	}
	return aggregate
}

func printQualityReports(reports []lintReport) {
	fmt.Printf("Linted %d active skill(s): %s\n", len(reports), aggregateLintReports(reports).Summary)
	if len(reports) == 0 {
		fmt.Println("No active skills found.")
		return
	}
	for _, report := range reports {
		printQualityReport(report)
	}
}

func printQualityReport(report lintReport) {
	fmt.Printf("%s: %s\n", report.Target, report.Summary)
	if len(report.Issues) == 0 {
		fmt.Println("No obvious quality issues found.")
		return
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SEVERITY\tISSUE\tDETAIL")
	for _, issue := range report.Issues {
		fmt.Fprintf(tw, "%s\t%s\t%s\n", issue.Severity, issue.Title, issue.Detail)
	}
	_ = tw.Flush()
}

func runSetup(args []string) error {
	opts, err := parseSetupArgs(args)
	if err != nil {
		return errors.New("usage: skillsdex setup [--target codex] [--set SLUG|--pinned|--defaults] [--apply|--dry-run] [--json] [--yes|--no]")
	}
	if opts.JSON && opts.Apply {
		return errors.New("--json is preview-only for setup; omit --apply")
	}
	opts.Target, err = resolveTarget(opts.Target)
	if err != nil {
		return err
	}
	client, err := authenticatedClient()
	if err != nil {
		return err
	}
	var me struct {
		Username string `json:"username"`
	}
	if err := client.getJSON("/v1/me", &me); err != nil {
		return fmt.Errorf("server check failed: %w", err)
	}

	selection, err := chooseSetupInstall(client, opts)
	if err != nil {
		return err
	}
	confirm := opts.Confirm
	confirm.DryRun = !opts.Apply
	if opts.JSON {
		return printSetupPlanJSON(client.base, me.Username, opts.Target, selection)
	}
	fmt.Printf("Server: %s reachable as %s\n", client.base, me.Username)
	if confirm.DryRun {
		fmt.Println("Mode: preview only; no files will be written.")
	} else {
		fmt.Println("Mode: apply; local managed skill files may be written after confirmation.")
	}
	switch selection.Mode {
	case setupInstallSet:
		fmt.Printf("Selected install set: %s\n", selection.SetSlug)
		if err := runInstallSet(append(setupInstallArgs(opts.Target, confirm), selection.SetSlug)); err != nil {
			return err
		}
	case setupInstallPinned:
		fmt.Println("Selected pinned skills.")
		if err := runInstallPinned(setupInstallArgs(opts.Target, confirm)); err != nil {
			return err
		}
	case setupInstallDefaults:
		fmt.Println("Selected recommended defaults.")
		if err := runInstallDefaults(setupInstallArgs(opts.Target, confirm)); err != nil {
			return err
		}
	default:
		return errors.New("no setup install selection found")
	}
	if confirm.DryRun {
		fmt.Println("Next steps:")
		fmt.Printf("  %s\n", setupApplyCommand(opts.Target, selection))
		fmt.Println("  skillsdex sync --dry-run --strict")
		printSetupQualityCommands()
		printSetupBackupCommands()
		return nil
	}
	fmt.Println("Sync preview:")
	if err := runSync([]string{"--dry-run", "--strict"}); err != nil {
		return err
	}
	printSetupQualityCommands()
	printSetupBackupCommands()
	return nil
}

func printSetupQualityCommands() {
	fmt.Println("Quality gate:")
	for _, command := range setupQualityCommands() {
		fmt.Printf("  %s\n", command)
	}
}

func printSetupBackupCommands() {
	fmt.Println("Backup confidence:")
	for _, command := range setupBackupCommands() {
		fmt.Printf("  %s\n", command)
	}
}

func setupQualityCommands() []string {
	return []string{"skillsdex lint --all --strict"}
}

func setupBackupCommands() []string {
	return []string{
		"skillsdex backup export --out ./skillsdex-backup.zip",
		"skillsdex backup restore --file ./skillsdex-backup.zip --dry-run",
	}
}

func printSetupPlanJSON(serverURL, username, target string, selection setupOptions) error {
	nextSteps := []string{
		setupApplyCommand(target, selection),
		"skillsdex sync --dry-run --strict",
	}
	nextSteps = append(nextSteps, setupQualityCommands()...)
	nextSteps = append(nextSteps, setupBackupCommands()...)
	report := setupPlanReport{
		ServerURL:    serverURL,
		Username:     username,
		Target:       target,
		Mode:         string(selection.Mode),
		SetSlug:      selection.SetSlug,
		Apply:        false,
		PreviewOnly:  true,
		ApplyCommand: setupApplyCommand(target, selection),
		NextSteps:    nextSteps,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}

func setupInstallArgs(target string, confirm confirmOptions) []string {
	args := []string{"--target", target}
	if confirm.DryRun {
		args = append(args, "--dry-run")
	}
	if confirm.Yes {
		args = append(args, "--yes")
	}
	if confirm.No {
		args = append(args, "--no")
	}
	return args
}

func setupApplyCommand(target string, selection setupOptions) string {
	args := []string{"skillsdex setup", "--target", target}
	switch selection.Mode {
	case setupInstallSet:
		args = append(args, "--set", selection.SetSlug)
	case setupInstallPinned:
		args = append(args, "--pinned")
	case setupInstallDefaults:
		args = append(args, "--defaults")
	}
	args = append(args, "--apply", "--yes")
	return strings.Join(args, " ")
}

func resolveTarget(target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		cfg, err := loadLocalConfig()
		if err != nil {
			return "", err
		}
		target = cfg.DefaultTarget
	}
	if err := validateTarget(target); err != nil {
		return "", err
	}
	return target, nil
}

func chooseSetupInstall(client *apiClient, opts setupOptions) (setupOptions, error) {
	if opts.Mode != setupInstallAuto {
		return opts, nil
	}
	var sets installSetsResponse
	if err := client.getJSON("/v1/install-sets", &sets); err != nil {
		return setupOptions{}, err
	}
	nonEmptySets := make([]InstallSet, 0, len(sets.Sets))
	for _, set := range sets.Sets {
		if set.SkillCount > 0 {
			nonEmptySets = append(nonEmptySets, set)
		}
	}
	if len(nonEmptySets) == 1 {
		opts.Mode = setupInstallSet
		opts.SetSlug = nonEmptySets[0].Slug
		return opts, nil
	}
	if len(nonEmptySets) > 1 {
		slugs := make([]string, 0, len(nonEmptySets))
		for _, set := range nonEmptySets {
			slugs = append(slugs, set.Slug)
		}
		sort.Strings(slugs)
		return setupOptions{}, fmt.Errorf("multiple install sets found (%s); choose one with --set SLUG", strings.Join(slugs, ", "))
	}

	var list struct {
		Skills []Skill `json:"skills"`
	}
	if err := client.getJSON("/v1/skills", &list); err != nil {
		return setupOptions{}, err
	}
	pinned, defaults := 0, 0
	for _, skill := range list.Skills {
		if skill.ArchivedAt != "" {
			continue
		}
		if skill.PinnedInstall {
			pinned++
		}
		if skill.RecommendedDefault {
			defaults++
		}
	}
	if pinned > 0 {
		opts.Mode = setupInstallPinned
		return opts, nil
	}
	if defaults > 0 {
		opts.Mode = setupInstallDefaults
		return opts, nil
	}
	return setupOptions{}, errors.New("no install set, pinned skill, or recommended default found; create or import a skill first")
}

func runInstall(args []string) error {
	identifier, target, confirm, err := parseTargetArgs(args, true)
	if err != nil {
		return errors.New("usage: skillsdex install <owner>/<slug> [--target codex] [--dry-run] [--yes|--no]")
	}
	target, err = resolveTarget(target)
	if err != nil {
		return err
	}
	owner, slug, err := parseIdentifier(identifier)
	if err != nil {
		return err
	}
	if err := validateSlug("owner", owner); err != nil {
		return err
	}
	if err := validateSlug("slug", slug); err != nil {
		return err
	}
	client, err := authenticatedClient()
	if err != nil {
		return err
	}
	resp, err := client.fetchSkill(owner, slug)
	if err != nil {
		return err
	}
	if resp.Skill.ArchivedAt != "" {
		return errors.New("remote skill is archived")
	}
	manifest, err := loadManifest()
	if err != nil {
		return err
	}
	if _, err := installSkillResponse(&manifest, resp, target, confirm); err != nil {
		return err
	}
	if confirm.DryRun {
		return nil
	}
	if err := saveManifest(manifest); err != nil {
		return err
	}
	return nil
}

func runInstallDefaults(args []string) error {
	target, confirm, err := parseInstallDefaultsArgs(args)
	if err != nil {
		return errors.New("usage: skillsdex install-defaults [--target codex] [--dry-run] [--yes|--no]")
	}
	target, err = resolveTarget(target)
	if err != nil {
		return err
	}
	return installMatchingSkills(target, confirm, "recommended default", func(s Skill) bool {
		return s.RecommendedDefault && s.ArchivedAt == ""
	})
}

func runInstallPinned(args []string) error {
	target, confirm, err := parseInstallDefaultsArgs(args)
	if err != nil {
		return errors.New("usage: skillsdex install-pinned [--target codex] [--dry-run] [--yes|--no]")
	}
	target, err = resolveTarget(target)
	if err != nil {
		return err
	}
	return installMatchingSkills(target, confirm, "pinned", func(s Skill) bool {
		return s.PinnedInstall && s.ArchivedAt == ""
	})
}

func runInstallSet(args []string) error {
	setSlug, target, confirm, err := parseTargetArgs(args, true)
	if err != nil {
		return errors.New("usage: skillsdex install-set <set-slug> [--target codex] [--dry-run] [--yes|--no]")
	}
	target, err = resolveTarget(target)
	if err != nil {
		return err
	}
	if err := validateSlug("set slug", setSlug); err != nil {
		return err
	}
	client, err := authenticatedClient()
	if err != nil {
		return err
	}
	set, err := client.fetchInstallSet(setSlug)
	if err != nil {
		return err
	}
	if len(set.Skills) == 0 {
		fmt.Printf("Install set %s has no active skills.\n", set.Set.Slug)
		return nil
	}
	manifest, err := loadManifest()
	if err != nil {
		return err
	}
	installed := 0
	for _, skill := range set.Skills {
		resp, err := client.fetchSkill(skill.OwnerSlug, skill.Slug)
		if err != nil {
			return err
		}
		if _, err := installSkillResponse(&manifest, resp, target, confirm); err != nil {
			return err
		}
		installed++
	}
	if confirm.DryRun {
		fmt.Printf("Would install %d skill(s) from set %s.\n", installed, set.Set.Slug)
		return nil
	}
	if err := saveManifest(manifest); err != nil {
		return err
	}
	fmt.Printf("Installed %d skill(s) from set %s.\n", installed, set.Set.Slug)
	return nil
}

func installMatchingSkills(target string, confirm confirmOptions, label string, include func(Skill) bool) error {
	client, err := authenticatedClient()
	if err != nil {
		return err
	}
	var list struct {
		Skills []Skill `json:"skills"`
	}
	if err := client.getJSON("/v1/skills", &list); err != nil {
		return err
	}
	var selected []Skill
	for _, s := range list.Skills {
		if include(s) {
			selected = append(selected, s)
		}
	}
	sort.Slice(selected, func(i, j int) bool {
		return selected[i].OwnerSlug+"/"+selected[i].Slug < selected[j].OwnerSlug+"/"+selected[j].Slug
	})
	if len(selected) == 0 {
		fmt.Printf("No %s skills found.\n", label)
		return nil
	}
	manifest, err := loadManifest()
	if err != nil {
		return err
	}
	installed := 0
	for _, s := range selected {
		resp, err := client.fetchSkill(s.OwnerSlug, s.Slug)
		if err != nil {
			return err
		}
		if _, err := installSkillResponse(&manifest, resp, target, confirm); err != nil {
			return err
		}
		installed++
	}
	if confirm.DryRun {
		fmt.Printf("Would install %d %s skill(s).\n", installed, label)
		return nil
	}
	if err := saveManifest(manifest); err != nil {
		return err
	}
	fmt.Printf("Installed %d %s skill(s).\n", installed, label)
	return nil
}

func installSkillResponse(manifest *Manifest, resp skillResponse, target string, confirm confirmOptions) (string, error) {
	if resp.Skill.ArchivedAt != "" {
		return "", errors.New("remote skill is archived")
	}
	owner, slug := resp.Skill.OwnerSlug, resp.Skill.Slug
	skillPath, err := targetPath(target, owner, slug)
	if err != nil {
		return "", err
	}
	_, existing := manifest.find(owner, slug, target)
	files, err := skillPackageFiles(resp)
	if err != nil {
		return "", err
	}
	baseDir := filepath.Dir(skillPath)
	for _, file := range files {
		dst := packageFileDestination(skillPath, file.Path)
		installedHash := installedHashForFile(existing, file.Path)
		if err := confirmExecutableFile(file, installedHash, confirm); err != nil {
			return "", err
		}
		if err := confirmPackageFileWrite(dst, installedHash, []byte(file.Content), confirm); err != nil {
			return "", err
		}
	}
	if existing != nil {
		nextPaths := map[string]bool{}
		for _, file := range files {
			nextPaths[file.Path] = true
		}
		for _, file := range installedFiles(existing) {
			if nextPaths[file.Path] {
				continue
			}
			dst := packageFileDestination(skillPath, file.Path)
			if err := confirmManagedFileRemoval(dst, file.Hash, confirm); err != nil {
				return "", err
			}
		}
	}
	if confirm.DryRun {
		fmt.Printf("Would install %s/%s to %s\n", owner, slug, skillPath)
		printSkillPackageFileSummary(files)
		return skillPath, nil
	}
	if err := os.MkdirAll(baseDir, 0700); err != nil {
		return "", err
	}
	if existing != nil {
		nextPaths := map[string]bool{}
		for _, file := range files {
			nextPaths[file.Path] = true
		}
		for _, file := range installedFiles(existing) {
			if nextPaths[file.Path] {
				continue
			}
			dst := packageFileDestination(skillPath, file.Path)
			if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
				return "", err
			}
			removeEmptyParents(filepath.Dir(dst), baseDir)
		}
	}
	for _, file := range files {
		dst := packageFileDestination(skillPath, file.Path)
		if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
			return "", err
		}
		if err := os.WriteFile(dst, []byte(file.Content), fileMode(file)); err != nil {
			return "", err
		}
	}
	manifest.upsert(newInstallPackage(owner, slug, target, skillPath, resp.Skill.CurrentRevisionID, files))
	fmt.Printf("Installed %s/%s to %s\n", owner, slug, skillPath)
	return skillPath, nil
}

func skillPackageFiles(resp skillResponse) ([]SkillFile, error) {
	files := resp.Files
	if len(files) == 0 {
		files = []SkillFile{{Path: "SKILL.md", Content: resp.Raw, ContentType: "text/markdown; charset=utf-8"}}
	}
	out := make([]SkillFile, 0, len(files)+1)
	seen := map[string]bool{}
	hasMain := false
	for _, file := range files {
		file.Path = strings.TrimSpace(file.Path)
		if file.Path == "" {
			continue
		}
		if err := validateSkillFilePath(file.Path); err != nil {
			return nil, err
		}
		if seen[file.Path] {
			return nil, fmt.Errorf("duplicate package file %q", file.Path)
		}
		seen[file.Path] = true
		if file.Path == "SKILL.md" {
			hasMain = true
			if file.Content == "" && resp.Raw != "" {
				file.Content = resp.Raw
			}
		}
		out = append(out, file)
	}
	if !hasMain {
		out = append(out, SkillFile{Path: "SKILL.md", Content: resp.Raw, ContentType: "text/markdown; charset=utf-8"})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path == "SKILL.md" {
			return true
		}
		if out[j].Path == "SKILL.md" {
			return false
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

func fileMode(file SkillFile) os.FileMode {
	if file.Executable {
		return 0700
	}
	return 0600
}

func runInstalled() error {
	return runInstalledArgs(nil)
}

func runInstalledArgs(args []string) error {
	opts, err := parseInstalledArgs(args)
	if err != nil {
		return errors.New("usage: skillsdex installed [--strict] [--refresh] [--json]")
	}
	manifest, err := loadManifest()
	if err != nil {
		return err
	}
	var client *apiClient
	if c, err := authenticatedClient(); err == nil {
		client = c
	} else if opts.Refresh {
		return err
	}
	report := installedStatusReport{
		InstallCount:       len(manifest.Installs),
		Strict:             opts.Strict,
		Refresh:            opts.Refresh,
		StatusCounts:       map[string]int{},
		SourceStatusCounts: map[string]int{},
	}
	if len(manifest.Installs) == 0 {
		report.Issues = append(report.Issues, "no managed installs")
	}
	for _, in := range manifest.Installs {
		var remote *Skill
		var remoteErr error
		sourceStatus := "-"
		id := in.Owner + "/" + in.Slug
		if client != nil {
			resp, err := client.fetchSkill(in.Owner, in.Slug)
			if err != nil {
				remoteErr = err
			} else {
				remote = &resp.Skill
			}
		}
		status := installStatus(in, remote, remoteErr)
		installIssue := status != "clean"
		if opts.Refresh {
			sourceStatus = installedSourceRefreshStatus(client, in, remote, remoteErr)
			if sourceStatus == "update-available" || sourceStatus == "refresh-failed" {
				installIssue = true
			}
		} else if remote != nil && remote.SourceURL != "" {
			sourceStatus = "imported"
		}
		if installIssue {
			if status != "clean" {
				report.Issues = append(report.Issues, fmt.Sprintf("%s install status %s", id, status))
			} else {
				report.Issues = append(report.Issues, fmt.Sprintf("%s source status %s", id, sourceStatus))
			}
		}
		report.StatusCounts[status]++
		report.SourceStatusCounts[sourceStatus]++
		report.Installs = append(report.Installs, installedStatusRow{
			Identifier:     id,
			Owner:          in.Owner,
			Slug:           in.Slug,
			Target:         in.Target,
			Path:           in.Path,
			RemoteRevision: in.RemoteRevision,
			Status:         status,
			SourceStatus:   sourceStatus,
			NeedsAttention: installIssue,
		})
	}
	report.IssueCount = len(report.Issues)
	report.Healthy = report.InstallCount > 0 && report.IssueCount == 0
	if opts.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return err
		}
	} else {
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "IDENTIFIER\tTARGET\tPATH\tREVISION\tSTATUS\tSOURCE")
		for _, row := range report.Installs {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n", row.Identifier, row.Target, row.Path, row.RemoteRevision, row.Status, row.SourceStatus)
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}
	if opts.Strict {
		if len(manifest.Installs) == 0 {
			return errors.New("no managed installs")
		}
		if report.IssueCount > 0 {
			return fmt.Errorf("%d managed install(s) need attention", report.IssueCount)
		}
	}
	return nil
}

func installedSourceRefreshStatus(client *apiClient, in Install, remote *Skill, remoteErr error) string {
	if client == nil || remoteErr != nil || remote == nil || remote.ArchivedAt != "" {
		return "not-checked"
	}
	if remote.SourceURL == "" {
		return "-"
	}
	resp, err := client.refreshSkill(in.Owner, in.Slug, true)
	if err != nil {
		return "refresh-failed"
	}
	switch resp.Status {
	case "already current":
		return "current"
	case "update available":
		return "update-available"
	default:
		return strings.ReplaceAll(resp.Status, " ", "-")
	}
}

func runSync(args []string) error {
	opts, err := parseSyncArgs(args)
	if err != nil {
		return errors.New("usage: skillsdex sync [--dry-run] [--strict] [--json] [--yes|--no]")
	}
	confirm := opts.Confirm
	if opts.JSON && !confirm.DryRun {
		return errors.New("--json requires --dry-run for sync")
	}
	manifest, err := loadManifest()
	if err != nil {
		return err
	}
	client, err := authenticatedClient()
	if err != nil {
		return err
	}
	sort.Slice(manifest.Installs, func(i, j int) bool {
		return manifest.Installs[i].Owner+"/"+manifest.Installs[i].Slug < manifest.Installs[j].Owner+"/"+manifest.Installs[j].Slug
	})
	if opts.JSON {
		return runSyncDryRunJSON(opts, manifest, client)
	}
	changed := false
	pendingChanges := false
	failures := 0
	for _, in := range manifest.Installs {
		id := in.Owner + "/" + in.Slug
		resp, err := client.fetchSkill(in.Owner, in.Slug)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: remote-deleted or unavailable; leaving local file in place\n", id)
			failures++
			continue
		}
		if resp.Skill.ArchivedAt != "" {
			fmt.Fprintf(os.Stderr, "%s: remote archived; leaving local file in place\n", id)
			failures++
			continue
		}
		localModified, localMissing := installHasLocalChanges(in)
		if !localModified && !localMissing && resp.Skill.CurrentRevisionID == in.RemoteRevision {
			fmt.Printf("%s: clean\n", id)
			continue
		}
		files, err := skillPackageFiles(resp)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: invalid package: %v\n", id, err)
			failures++
			continue
		}
		if localModified {
			showPackageDiffs(in, files)
			if confirm.DryRun {
				fmt.Printf("%s: local file modified; would require confirmation before updating to revision %d\n", id, resp.Skill.RevisionNumber)
				printSourceRefreshChanges(syncPackageChanges(in, files), false)
				pendingChanges = true
				continue
			}
			if err := requireConfirmation(fmt.Sprintf("%s was edited locally. Overwrite with remote revision?", in.Path), confirm); err != nil {
				fmt.Fprintf(os.Stderr, "%s: skipped: %v\n", id, err)
				continue
			}
		}
		if confirm.DryRun {
			if localMissing {
				fmt.Printf("%s: would reinstall missing package file(s) at revision %d\n", id, resp.Skill.RevisionNumber)
			} else {
				fmt.Printf("%s: would update package to revision %d\n", id, resp.Skill.RevisionNumber)
			}
			printSourceRefreshChanges(syncPackageChanges(in, files), false)
			pendingChanges = true
			continue
		}
		writeConfirm := confirm
		if localModified {
			writeConfirm.Yes = true
		}
		if _, err := installSkillResponse(&manifest, resp, in.Target, writeConfirm); err != nil {
			return err
		}
		changed = true
		fmt.Printf("%s: updated to revision %d\n", id, resp.Skill.RevisionNumber)
	}
	if changed {
		if err := saveManifest(manifest); err != nil {
			return err
		}
	}
	if confirm.DryRun && pendingChanges {
		fmt.Println("Next steps:")
		fmt.Println("  skillsdex sync --yes")
	}
	if failures > 0 {
		return fmt.Errorf("%d sync issue(s) need attention", failures)
	}
	if opts.Strict && pendingChanges {
		return errors.New("sync updates available")
	}
	return nil
}

func runSyncDryRunJSON(opts syncOptions, manifest Manifest, client *apiClient) error {
	report := syncDryRunReport{
		DryRun:       true,
		Strict:       opts.Strict,
		InstallCount: len(manifest.Installs),
		StatusCounts: map[string]int{},
	}
	for _, in := range manifest.Installs {
		id := in.Owner + "/" + in.Slug
		result := syncDryRunResult{
			Identifier:     id,
			Owner:          in.Owner,
			Slug:           in.Slug,
			Target:         in.Target,
			Path:           in.Path,
			RemoteRevision: in.RemoteRevision,
		}
		resp, err := client.fetchSkill(in.Owner, in.Slug)
		if err != nil {
			result.Status = "remote-unavailable"
			result.NeedsAttention = true
			result.Error = err.Error()
			report.FailureCount++
			report.StatusCounts[result.Status]++
			report.Results = append(report.Results, result)
			continue
		}
		if resp.Skill.ArchivedAt != "" {
			result.Status = "remote-archived"
			result.NeedsAttention = true
			report.FailureCount++
			report.StatusCounts[result.Status]++
			report.Results = append(report.Results, result)
			continue
		}
		localModified, localMissing := installHasLocalChanges(in)
		result.NextRevision = resp.Skill.RevisionNumber
		if !localModified && !localMissing && resp.Skill.CurrentRevisionID == in.RemoteRevision {
			result.Status = "clean"
			report.CleanCount++
			report.StatusCounts[result.Status]++
			report.Results = append(report.Results, result)
			continue
		}
		files, err := skillPackageFiles(resp)
		if err != nil {
			result.Status = "invalid-package"
			result.NeedsAttention = true
			result.Error = err.Error()
			report.FailureCount++
			report.StatusCounts[result.Status]++
			report.Results = append(report.Results, result)
			continue
		}
		result.Changes = syncPackageChanges(in, files)
		result.NeedsAttention = true
		switch {
		case localModified:
			result.Status = "local-modified"
		case localMissing:
			result.Status = "missing"
		default:
			result.Status = "update-available"
		}
		report.PendingCount++
		report.StatusCounts[result.Status]++
		report.Results = append(report.Results, result)
	}
	if report.PendingCount > 0 {
		report.NextSteps = append(report.NextSteps, "skillsdex sync --yes")
	}
	report.Healthy = report.PendingCount == 0 && report.FailureCount == 0
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(report); err != nil {
		return err
	}
	if report.FailureCount > 0 {
		return fmt.Errorf("%d sync issue(s) need attention", report.FailureCount)
	}
	if opts.Strict && report.PendingCount > 0 {
		return errors.New("sync updates available")
	}
	return nil
}

func syncPackageChanges(in Install, files []SkillFile) []SourceRefreshChange {
	current := map[string]InstalledFile{}
	for _, file := range installedFiles(&in) {
		current[file.Path] = file
	}
	next := map[string]SkillFile{}
	paths := map[string]bool{}
	for _, file := range files {
		next[file.Path] = file
		paths[file.Path] = true
	}
	for path := range current {
		paths[path] = true
	}
	var sorted []string
	for path := range paths {
		sorted = append(sorted, path)
	}
	sort.Strings(sorted)
	var changes []SourceRefreshChange
	for _, path := range sorted {
		oldFile, hadOld := current[path]
		newFile, hasNew := next[path]
		switch {
		case !hadOld && hasNew:
			changes = append(changes, SourceRefreshChange{Path: path, Status: "added"})
		case hadOld && !hasNew:
			changes = append(changes, SourceRefreshChange{Path: path, Status: "removed"})
		case hadOld && hasNew:
			nextHash := sha256String([]byte(newFile.Content))
			if oldFile.Hash != nextHash || oldFile.Executable != newFile.Executable {
				changes = append(changes, SourceRefreshChange{Path: path, Status: "modified"})
			}
		}
	}
	return changes
}

func runUninstall(args []string) error {
	identifier, target, confirm, err := parseTargetArgs(args, false)
	if err != nil {
		return errors.New("usage: skillsdex uninstall <owner>/<slug> [--target codex] [--yes|--no]")
	}
	target, err = resolveTarget(target)
	if err != nil {
		return err
	}
	owner, slug, err := parseIdentifier(identifier)
	if err != nil {
		return err
	}
	manifest, err := loadManifest()
	if err != nil {
		return err
	}
	_, in := manifest.find(owner, slug, target)
	if in == nil {
		return errors.New("install is not managed by SkillsDex")
	}
	modified, err := installHasModifiedManagedFiles(*in)
	if err != nil {
		return err
	}
	if modified {
		showPackageDiffs(*in, nil)
		if err := requireConfirmation(fmt.Sprintf("%s was edited locally. Remove it?", in.Path), confirm); err != nil {
			return err
		}
	}
	for _, file := range installedFiles(in) {
		dst, err := installedFileDestination(*in, file.Path)
		if err != nil {
			return err
		}
		if err := os.Remove(dst); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		removeEmptyParents(filepath.Dir(dst), filepath.Dir(in.Path))
	}
	_ = os.Remove(filepath.Dir(in.Path))
	manifest.remove(owner, slug, target)
	if err := saveManifest(manifest); err != nil {
		return err
	}
	fmt.Printf("Uninstalled %s/%s\n", owner, slug)
	return nil
}

func runDoctor(args ...string) error {
	opts, err := parseDoctorArgs(args)
	if err != nil {
		return errors.New("usage: skillsdex doctor [--strict] [--refresh] [--fix-permissions] [--json]")
	}
	dir, _ := skillsdexDir()
	authPath, _ := authPath()
	auth, authErr := loadAuth()
	manifestPath, _ := manifestPath()
	manifest, manifestErr := loadManifest()
	configPath, _ := configPath()
	cfg, configErr := loadLocalConfig()
	codexDir, codexErr := codexSkillsDir()
	var issues []string
	report := doctorReport{
		SkillsDexDirectory: dir,
		Strict:             opts.Strict,
		Refresh:            opts.Refresh,
	}
	if !opts.JSON {
		fmt.Printf("SkillsDex directory: %s\n", dir)
	}
	if opts.FixPermissions {
		if fixed, issue := fixDoctorPrivateFilePermissions(authPath, "auth file"); issue != "" {
			issues = append(issues, issue)
		} else if fixed != "" {
			report.FixedPermissions = append(report.FixedPermissions, fixed)
			if !opts.JSON {
				fmt.Println(fixed)
			}
		}
		if fixed, issue := fixDoctorPrivateFilePermissions(manifestPath, "manifest file"); issue != "" {
			issues = append(issues, issue)
		} else if fixed != "" {
			report.FixedPermissions = append(report.FixedPermissions, fixed)
			if !opts.JSON {
				fmt.Println(fixed)
			}
		}
		if fixed, issue := fixDoctorPrivateFilePermissions(configPath, "config file"); issue != "" {
			issues = append(issues, issue)
		} else if fixed != "" {
			report.FixedPermissions = append(report.FixedPermissions, fixed)
			if !opts.JSON {
				fmt.Println(fixed)
			}
		}
	}
	report.AuthPath = doctorPrivateFileReport(authPath, "auth file")
	report.ManifestPath = doctorPrivateFileReport(manifestPath, "manifest file")
	report.ConfigPath = doctorPrivateFileReport(configPath, "config file")
	if !opts.JSON {
		fmt.Printf("Auth path: %s%s\n", authPath, doctorPathHumanSuffix(report.AuthPath))
		fmt.Printf("Manifest path: %s%s\n", manifestPath, doctorPathHumanSuffix(report.ManifestPath))
		fmt.Printf("Config path: %s%s\n", configPath, doctorPathHumanSuffix(report.ConfigPath))
	}
	if report.AuthPath.Issue != "" {
		issues = append(issues, report.AuthPath.Issue)
	}
	if report.ManifestPath.Issue != "" {
		issues = append(issues, report.ManifestPath.Issue)
	}
	if report.ConfigPath.Issue != "" {
		issues = append(issues, report.ConfigPath.Issue)
	}
	if configErr != nil {
		report.DefaultTarget = doctorCheckReport{Status: "unavailable", Message: configErr.Error()}
		if !opts.JSON {
			fmt.Printf("Default target: unavailable (%v)\n", configErr)
		}
		issues = append(issues, "local config unavailable")
	} else if err := validateTarget(cfg.DefaultTarget); err != nil {
		report.DefaultTarget = doctorCheckReport{Status: "invalid", Value: cfg.DefaultTarget, Message: err.Error()}
		if !opts.JSON {
			fmt.Printf("Default target: invalid (%v)\n", err)
		}
		issues = append(issues, "default target invalid")
	} else {
		report.DefaultTarget = doctorCheckReport{Status: "ok", Value: cfg.DefaultTarget}
		if !opts.JSON {
			fmt.Printf("Default target: %s\n", cfg.DefaultTarget)
		}
	}
	var client *apiClient
	authReady := false
	if authErr == nil {
		report.Auth = doctorCheckReport{Status: "configured", Value: auth.ServerURL}
		if !opts.JSON {
			fmt.Printf("Auth: configured for %s\n", auth.ServerURL)
		}
		client = newAPIClient(auth)
		var me map[string]any
		if err := client.getJSON("/v1/me", &me); err != nil {
			report.Server = doctorCheckReport{Status: "unreachable", Message: err.Error()}
			if !opts.JSON {
				fmt.Printf("Server: unreachable or token rejected (%v)\n", err)
			}
			issues = append(issues, "server unreachable or API token rejected")
			client = nil
		} else {
			username := fmt.Sprint(me["username"])
			report.Server = doctorCheckReport{Status: "reachable", Value: username}
			if !opts.JSON {
				fmt.Printf("Server: reachable as %v\n", me["username"])
			}
			authReady = true
		}
	} else {
		report.Auth = doctorCheckReport{Status: "not-configured", Message: authErr.Error()}
		if !opts.JSON {
			fmt.Printf("Auth: not configured (%v)\n", authErr)
		}
		issues = append(issues, "auth not configured")
	}
	if client != nil {
		var list struct {
			Skills []Skill `json:"skills"`
		}
		if err := client.getJSON("/v1/skills", &list); err != nil {
			report.Library = doctorLibraryReport{Status: "unavailable", Message: err.Error()}
			if !opts.JSON {
				fmt.Printf("Library: unavailable (%v)\n", err)
			}
			issues = append(issues, "library unavailable")
		} else {
			active := 0
			recommended := 0
			pinned := 0
			for _, s := range list.Skills {
				if s.ArchivedAt != "" {
					continue
				}
				active++
				if s.RecommendedDefault {
					recommended++
				}
				if s.PinnedInstall {
					pinned++
				}
			}
			report.Library = doctorLibraryReport{Status: "ok", Active: active, Recommended: recommended, Pinned: pinned}
			if !opts.JSON {
				fmt.Printf("Library: %d active skill(s), %d recommended default, %d pinned\n", active, recommended, pinned)
			}
		}
	} else {
		report.Library = doctorLibraryReport{Status: "not-checked"}
	}
	if manifestErr != nil {
		report.ManagedInstalls = doctorManagedInstallsReport{Status: "unavailable", Message: manifestErr.Error()}
		if !opts.JSON {
			fmt.Printf("Managed installs: unavailable (%v)\n", manifestErr)
		}
		issues = append(issues, "managed install manifest unavailable")
	} else {
		report.ManagedInstalls = doctorManagedInstallsReport{Status: "ok", Count: len(manifest.Installs)}
		if !opts.JSON {
			fmt.Printf("Managed installs: %d\n", len(manifest.Installs))
		}
		if len(manifest.Installs) == 0 {
			report.ManagedInstalls.Status = "empty"
			issues = append(issues, "no managed installs")
		} else {
			counts := map[string]int{}
			sourceCounts := map[string]int{}
			for _, in := range manifest.Installs {
				var remote *Skill
				var remoteErr error
				if client != nil {
					resp, err := client.fetchSkill(in.Owner, in.Slug)
					if err != nil {
						remoteErr = err
					} else {
						remote = &resp.Skill
					}
				}
				counts[installStatus(in, remote, remoteErr)]++
				if opts.Refresh {
					sourceStatus := installedSourceRefreshStatus(client, in, remote, remoteErr)
					if sourceStatus != "-" && sourceStatus != "not-checked" {
						sourceCounts[sourceStatus]++
					}
				}
			}
			report.ManagedInstalls.StatusCounts = counts
			statuses := []string{"clean", "update-available", "modified", "missing", "remote-archived", "remote-deleted"}
			var parts []string
			for _, status := range statuses {
				if counts[status] > 0 {
					parts = append(parts, fmt.Sprintf("%s=%d", status, counts[status]))
				}
			}
			if len(parts) > 0 {
				if !opts.JSON {
					fmt.Printf("Install status: %s\n", strings.Join(parts, ", "))
				}
			}
			for status, count := range counts {
				if status != "clean" && count > 0 {
					issues = append(issues, fmt.Sprintf("managed install status %s=%d", status, count))
				}
			}
			if opts.Refresh {
				report.ManagedInstalls.SourceRefreshCounts = sourceCounts
				if !opts.JSON {
					printDoctorSourceRefresh(sourceCounts)
				}
				if sourceCounts["update-available"] > 0 {
					issues = append(issues, fmt.Sprintf("source updates available=%d", sourceCounts["update-available"]))
				}
				if sourceCounts["refresh-failed"] > 0 {
					issues = append(issues, fmt.Sprintf("source refresh failed=%d", sourceCounts["refresh-failed"]))
				}
			}
		}
	}
	if codexErr != nil {
		report.CodexSkillsDirectory = doctorCheckReport{Status: "unavailable", Message: codexErr.Error()}
		if !opts.JSON {
			fmt.Printf("Codex skills directory: unavailable (%v)\n", codexErr)
		}
		issues = append(issues, "Codex skills directory unavailable")
		report.NextSteps = buildDoctorNextSteps(authReady, manifest)
		return finishDoctorReport(opts, report, issues)
	}
	report.CodexSkillsDirectory = doctorCheckReport{Status: "found", Value: codexDir}
	if !opts.JSON {
		fmt.Printf("Codex skills directory: %s\n", codexDir)
	}
	if err := os.MkdirAll(codexDir, 0700); err != nil {
		report.CodexSkillsDirectory.Status = "not-writable"
		report.CodexSkillsDirectory.Message = err.Error()
		if !opts.JSON {
			fmt.Printf("Codex skills directory writable: no (%v)\n", err)
		}
		issues = append(issues, "Codex skills directory is not writable")
		report.NextSteps = buildDoctorNextSteps(authReady, manifest)
		return finishDoctorReport(opts, report, issues)
	}
	test := filepath.Join(codexDir, ".skillsdex-write-test")
	if err := os.WriteFile(test, []byte("ok"), 0600); err != nil {
		report.CodexSkillsDirectory.Status = "not-writable"
		report.CodexSkillsDirectory.Message = err.Error()
		if !opts.JSON {
			fmt.Printf("Codex skills directory writable: no (%v)\n", err)
		}
		issues = append(issues, "Codex skills directory is not writable")
	} else {
		_ = os.Remove(test)
		report.CodexSkillsDirectory.Status = "writable"
		if !opts.JSON {
			fmt.Println("Codex skills directory writable: yes")
		}
	}
	report.NextSteps = buildDoctorNextSteps(authReady, manifest)
	return finishDoctorReport(opts, report, issues)
}

func printDoctorSourceRefresh(counts map[string]int) {
	statuses := []string{"current", "update-available", "refresh-failed"}
	var parts []string
	for _, status := range statuses {
		if counts[status] > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", status, counts[status]))
		}
	}
	if len(parts) == 0 {
		fmt.Println("Source refresh: no imported managed installs checked")
		return
	}
	fmt.Printf("Source refresh: %s\n", strings.Join(parts, ", "))
}

func fixDoctorPrivateFilePermissions(path, label string) (string, string) {
	if strings.TrimSpace(path) == "" {
		return "", ""
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", ""
	}
	if err != nil {
		return "", fmt.Sprintf("%s permissions could not be checked", label)
	}
	if info.IsDir() || info.Mode().Perm()&0077 == 0 {
		return "", ""
	}
	if err := os.Chmod(path, 0600); err != nil {
		return "", fmt.Sprintf("%s permissions could not be fixed", label)
	}
	return fmt.Sprintf("Fixed %s permissions: %s", label, path), ""
}

func finishDoctorReport(opts doctorOptions, report doctorReport, issues []string) error {
	report.Issues = issues
	report.IssueCount = len(issues)
	report.Healthy = len(issues) == 0
	if opts.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return err
		}
		if opts.Strict && len(issues) > 0 {
			return fmt.Errorf("doctor found %d issue(s)", len(issues))
		}
		return nil
	}
	printDoctorNextSteps(report.NextSteps)
	return finishDoctor(opts.Strict, issues)
}

func finishDoctor(strict bool, issues []string) error {
	if len(issues) == 0 {
		if strict {
			fmt.Println("Doctor strict check: passed")
		}
		return nil
	}
	if strict {
		fmt.Println("Doctor strict check: failed")
		fmt.Println("Issues:")
		for _, issue := range issues {
			fmt.Printf("  %s\n", issue)
		}
		return fmt.Errorf("doctor found %d issue(s)", len(issues))
	}
	return nil
}

func doctorPrivateFileReport(path, label string) doctorPathReport {
	report := doctorPathReport{Path: path}
	if strings.TrimSpace(path) == "" {
		report.Status = "unknown"
		return report
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		report.Status = "not-found"
		return report
	}
	if err != nil {
		report.Status = "unavailable"
		report.Detail = err.Error()
		report.Issue = label + " unavailable"
		return report
	}
	if info.IsDir() {
		report.Status = "directory"
		report.Issue = label + " path is a directory"
		return report
	}
	perm := info.Mode().Perm()
	report.Permissions = fmt.Sprintf("%04o", perm)
	if perm&0077 != 0 {
		report.Status = "too-open"
		report.Detail = fmt.Sprintf("run chmod 600 %s", shellQuote(path))
		report.Issue = label + " permissions are too open"
		return report
	}
	report.Status = "private"
	return report
}

func doctorPathHumanSuffix(report doctorPathReport) string {
	switch report.Status {
	case "not-found":
		return " (not found)"
	case "unavailable":
		return fmt.Sprintf(" (unavailable: %s)", report.Detail)
	case "directory":
		return " (directory)"
	case "too-open":
		return fmt.Sprintf(" (too open: %s; %s)", report.Permissions, report.Detail)
	case "private":
		return " (private)"
	default:
		return ""
	}
}

func buildDoctorNextSteps(authReady bool, manifest Manifest) []string {
	var steps []string
	if !authReady {
		steps = append(steps, tokenStdinLoginCommand())
	}
	if len(manifest.Installs) == 0 {
		steps = append(steps, "skillsdex install-defaults")
	} else {
		steps = append(steps, "skillsdex installed --refresh --strict")
	}
	steps = append(steps, "skillsdex sync --dry-run --strict")
	if authReady {
		steps = append(steps, "skillsdex lint --all --strict")
		steps = append(steps, "skillsdex backup export --out ./skillsdex-backup.zip")
		steps = append(steps, "skillsdex backup restore --file ./skillsdex-backup.zip --dry-run")
	}
	return steps
}

func printDoctorNextSteps(steps []string) {
	if len(steps) == 0 {
		return
	}
	fmt.Println("Next steps:")
	for _, step := range steps {
		fmt.Printf("  %s\n", step)
	}
}

func authenticatedClient() (*apiClient, error) {
	cfg, err := loadAuth()
	if err != nil {
		return nil, err
	}
	return newAPIClient(cfg), nil
}

func newAPIClient(cfg AuthConfig) *apiClient {
	return &apiClient{
		base:  strings.TrimRight(cfg.ServerURL, "/"),
		token: cfg.Token,
		http:  &http.Client{Timeout: cliRequestTimeout},
	}
}

func (c *apiClient) newRequest(method, path string, body io.Reader) (*http.Request, context.CancelFunc, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cliRequestTimeout)
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	return req, cancel, nil
}

func (c *apiClient) fetchSkill(owner, slug string) (skillResponse, error) {
	var resp skillResponse
	err := c.getJSON("/v1/skills/"+owner+"/"+slug, &resp)
	return resp, err
}

func (c *apiClient) fetchInstallSet(slug string) (installSetResponse, error) {
	var resp installSetResponse
	err := c.getJSON("/v1/install-sets/"+slug, &resp)
	return resp, err
}

func (c *apiClient) refreshSkill(owner, slug string, dryRun bool) (refreshResponse, error) {
	var resp refreshResponse
	path := "/v1/skills/" + url.PathEscape(owner) + "/" + url.PathEscape(slug) + "/refresh"
	if dryRun {
		path += "?dry_run=1"
	}
	err := c.postJSON(path, map[string]bool{}, &resp)
	return resp, err
}

func (c *apiClient) updateSkillFlags(owner, slug string, flags SkillFlagUpdate) (Skill, error) {
	var resp struct {
		Skill Skill `json:"skill"`
	}
	err := c.putJSON("/v1/skills/"+url.PathEscape(owner)+"/"+url.PathEscape(slug)+"/flags", flags, &resp)
	return resp.Skill, err
}

func (c *apiClient) skillExists(owner, slug string) (bool, error) {
	path := "/v1/skills/" + url.PathEscape(owner) + "/" + url.PathEscape(slug)
	req, cancel, err := c.newRequest(http.MethodGet, path, nil)
	if err != nil {
		return false, err
	}
	defer cancel()
	res, err := c.http.Do(req)
	if err != nil {
		return false, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return false, apiResponseError(http.MethodGet, path, res)
	}
	return true, nil
}

func (c *apiClient) getJSON(path string, out any) error {
	req, cancel, err := c.newRequest(http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	defer cancel()
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return apiResponseError(http.MethodGet, path, res)
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func (c *apiClient) getBytes(path string) ([]byte, error) {
	req, cancel, err := c.newRequest(http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer cancel()
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, apiResponseError(http.MethodGet, path, res)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxSkillsDexExportBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSkillsDexExportBytes {
		return nil, errors.New("export download is too large")
	}
	return data, nil
}

func (c *apiClient) putJSON(path string, in, out any) error {
	body, _ := json.Marshal(in)
	req, cancel, err := c.newRequest(http.MethodPut, path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer cancel()
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return apiResponseError(http.MethodPut, path, res)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func (c *apiClient) postJSON(path string, in, out any) error {
	body, _ := json.Marshal(in)
	req, cancel, err := c.newRequest(http.MethodPost, path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer cancel()
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return apiResponseError(http.MethodPost, path, res)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func (c *apiClient) postBytes(path string, data []byte, contentType string, out any) error {
	req, cancel, err := c.newRequest(http.MethodPost, path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer cancel()
	req.Header.Set("Content-Type", contentType)
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return apiResponseError(http.MethodPost, path, res)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func apiResponseError(method, path string, res *http.Response) error {
	message := ""
	body, err := io.ReadAll(io.LimitReader(res.Body, 4096))
	if err == nil {
		if parsed := apiErrorMessage(body); parsed != "" {
			message = parsed
		} else if trimmed := strings.TrimSpace(string(body)); trimmed != "" {
			message = trimmed
		}
	}
	if message == "" {
		return fmt.Errorf("%s %s: %s", method, path, res.Status)
	}
	return fmt.Errorf("%s %s: %s: %s", method, path, res.Status, message)
}

func apiErrorMessage(body []byte) string {
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.Error)
}

func packageFileDestination(skillPath, filePath string) string {
	if filePath == "SKILL.md" {
		return skillPath
	}
	return filepath.Join(filepath.Dir(skillPath), filepath.FromSlash(filePath))
}

func installedFileDestination(in Install, filePath string) (string, error) {
	if filePath == "SKILL.md" {
		return in.Path, nil
	}
	if err := validateSkillFilePath(filePath); err != nil {
		return "", err
	}
	return packageFileDestination(in.Path, filePath), nil
}

func installedFiles(in *Install) []InstalledFile {
	if in == nil {
		return nil
	}
	if len(in.Files) > 0 {
		return in.Files
	}
	return []InstalledFile{{Path: "SKILL.md", Hash: in.InstalledHash}}
}

func installedHashForFile(in *Install, filePath string) string {
	for _, file := range installedFiles(in) {
		if file.Path == filePath {
			return file.Hash
		}
	}
	return ""
}

func installHasModifiedManagedFiles(in Install) (bool, error) {
	for _, file := range installedFiles(&in) {
		dst, err := installedFileDestination(in, file.Path)
		if err != nil {
			return false, err
		}
		current, err := os.ReadFile(dst)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return false, err
		}
		if sha256String(current) != file.Hash {
			return true, nil
		}
	}
	return false, nil
}

func confirmPackageFileWrite(path, installedHash string, next []byte, confirm confirmOptions) error {
	current, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if bytes.Equal(current, next) {
		return nil
	}
	currentHash := sha256String(current)
	if installedHash != "" && currentHash == installedHash {
		return nil
	}
	showOverwriteDiff(path, current, next)
	if confirm.DryRun {
		return nil
	}
	if installedHash == "" {
		return requireConfirmation(fmt.Sprintf("%s exists but is not managed by SkillsDex. Overwrite?", path), confirm)
	}
	return requireConfirmation(fmt.Sprintf("%s was edited locally. Overwrite?", path), confirm)
}

func confirmExecutableFile(file SkillFile, installedHash string, confirm confirmOptions) error {
	if confirm.DryRun || !file.Executable || file.Path == "SKILL.md" || installedHash != "" {
		return nil
	}
	return requireConfirmation(fmt.Sprintf("%s is an executable supporting file. Install it?", file.Path), confirm)
}

func confirmManagedFileRemoval(path, installedHash string, confirm confirmOptions) error {
	current, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if installedHash != "" && sha256String(current) == installedHash {
		return nil
	}
	showOverwriteDiff(path, current, nil)
	if confirm.DryRun {
		return nil
	}
	return requireConfirmation(fmt.Sprintf("%s was edited locally. Remove stale managed file?", path), confirm)
}

func showPackageDiffs(in Install, files []SkillFile) {
	baseDir := filepath.Dir(in.Path)
	next := map[string]SkillFile{}
	for _, file := range files {
		next[file.Path] = file
	}
	for _, file := range installedFiles(&in) {
		dst := in.Path
		if file.Path != "SKILL.md" {
			if err := validateSkillFilePath(file.Path); err != nil {
				continue
			}
			dst = filepath.Join(baseDir, filepath.FromSlash(file.Path))
		}
		current, err := os.ReadFile(dst)
		if err != nil || sha256String(current) == file.Hash {
			continue
		}
		if replacement, ok := next[file.Path]; ok {
			showOverwriteDiff(dst, current, []byte(replacement.Content))
		} else {
			showOverwriteDiff(dst, current, nil)
		}
	}
}

func removeEmptyParents(dir, stop string) {
	stopAbs, err := filepath.Abs(stop)
	if err != nil {
		return
	}
	for {
		dirAbs, err := filepath.Abs(dir)
		if err != nil || dirAbs == stopAbs {
			return
		}
		rel, err := filepath.Rel(stopAbs, dirAbs)
		if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			return
		}
		if err := os.Remove(dirAbs); err != nil {
			return
		}
		dir = filepath.Dir(dirAbs)
	}
}

func showOverwriteDiff(path string, current, next []byte) {
	diff := strings.TrimSpace(unifiedDiff(string(current), string(next)))
	if diff == "" {
		return
	}
	fmt.Fprintf(os.Stderr, "\nDiff for %s:\n%s\n\n", path, diff)
}

func extractZipToDir(zr *zip.Reader, dir string, confirm confirmOptions) (int, error) {
	rootAbs, err := filepath.Abs(dir)
	if err != nil {
		return 0, err
	}
	written := 0
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		dst, err := safeZipDestination(rootAbs, f.Name)
		if err != nil {
			return written, err
		}
		content, err := readZipEntryLimited(f, maxImportedSkillBytes)
		if err != nil {
			return written, err
		}
		if err := confirmFileOverwrite(dst, content, confirm); err != nil {
			return written, err
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
			return written, err
		}
		if err := os.WriteFile(dst, content, 0600); err != nil {
			return written, err
		}
		written++
	}
	return written, nil
}

func safeZipDestination(rootAbs, name string) (string, error) {
	if strings.TrimSpace(name) == "" || strings.Contains(name, "\\") || path.IsAbs(name) {
		return "", fmt.Errorf("unsafe export path %q", name)
	}
	clean := path.Clean(name)
	if clean == "." || clean != name || strings.HasPrefix(clean, "../") || clean == ".." || strings.Contains(clean, "/../") {
		return "", fmt.Errorf("unsafe export path %q", name)
	}
	dst := filepath.Join(rootAbs, filepath.FromSlash(clean))
	dstAbs, err := filepath.Abs(dst)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, dstAbs)
	if err != nil {
		return "", err
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." || filepath.IsAbs(rel) {
		return "", fmt.Errorf("unsafe export path %q", name)
	}
	return dstAbs, nil
}

func readZipEntryLimited(f *zip.File, limit int64) ([]byte, error) {
	if f.UncompressedSize64 > uint64(limit) {
		return nil, fmt.Errorf("%s is too large", f.Name)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	content, err := io.ReadAll(io.LimitReader(rc, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(content)) > limit {
		return nil, fmt.Errorf("%s is too large", f.Name)
	}
	return content, nil
}

func confirmFileOverwrite(path string, next []byte, confirm confirmOptions) error {
	current, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if bytes.Equal(current, next) {
		return nil
	}
	showOverwriteDiff(path, current, next)
	return requireConfirmation(fmt.Sprintf("%s already exists. Overwrite?", path), confirm)
}

func requireConfirmation(prompt string, confirm confirmOptions) error {
	if confirm.Yes && confirm.No {
		return errors.New("--yes and --no cannot be used together")
	}
	if confirm.Yes {
		return nil
	}
	if confirm.No {
		return errors.New("declined by --no")
	}
	if !isTerminal(os.Stdin) {
		return errors.New("refusing to overwrite in non-interactive mode; rerun with --yes to allow or --no to skip")
	}
	fmt.Printf("%s [y/N] ", prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	if ans != "y" && ans != "yes" {
		return errors.New("declined")
	}
	return nil
}

func isTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

func parseShowArgs(args []string) (showOptions, error) {
	var opts showOptions
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--raw":
			opts.Raw = true
		case arg == "--file":
			if i+1 >= len(args) {
				return showOptions{}, errors.New("missing file")
			}
			opts.FilePath = strings.TrimSpace(args[i+1])
			i++
		case strings.HasPrefix(arg, "--file="):
			opts.FilePath = strings.TrimSpace(strings.TrimPrefix(arg, "--file="))
		default:
			if strings.HasPrefix(arg, "-") {
				return showOptions{}, fmt.Errorf("unknown flag %s", arg)
			}
			if opts.Identifier != "" {
				return showOptions{}, errors.New("too many arguments")
			}
			opts.Identifier = arg
		}
	}
	if opts.Identifier == "" {
		return showOptions{}, errors.New("missing identifier")
	}
	if opts.Raw && opts.FilePath != "" {
		return showOptions{}, errors.New("--raw and --file cannot be used together")
	}
	if opts.FilePath != "" {
		if err := validateSkillFilePath(opts.FilePath); err != nil {
			return showOptions{}, err
		}
	}
	return opts, nil
}

func parseMarkArgs(args []string) (markOptions, error) {
	var opts markOptions
	applySeen := false
	dryRunSeen := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--pinned":
			if err := setMarkBool(&opts.Pinned, true, "pinned"); err != nil {
				return markOptions{}, err
			}
		case "--unpinned":
			if err := setMarkBool(&opts.Pinned, false, "pinned"); err != nil {
				return markOptions{}, err
			}
		case "--recommended", "--default":
			if err := setMarkBool(&opts.Recommended, true, "recommended"); err != nil {
				return markOptions{}, err
			}
		case "--not-recommended", "--not-default":
			if err := setMarkBool(&opts.Recommended, false, "recommended"); err != nil {
				return markOptions{}, err
			}
		case "--apply":
			opts.Apply = true
			applySeen = true
		case "--dry-run":
			opts.Apply = false
			dryRunSeen = true
		case "--yes", "-y":
			opts.Confirm.Yes = true
		case "--no", "-n":
			opts.Confirm.No = true
		default:
			if strings.HasPrefix(arg, "-") {
				return markOptions{}, fmt.Errorf("unknown flag %s", arg)
			}
			if opts.Identifier != "" {
				return markOptions{}, errors.New("too many arguments")
			}
			opts.Identifier = strings.TrimSpace(arg)
		}
	}
	if opts.Identifier == "" {
		return markOptions{}, errors.New("missing identifier")
	}
	if opts.Pinned == nil && opts.Recommended == nil {
		return markOptions{}, errors.New("at least one flag is required")
	}
	if opts.Confirm.Yes && opts.Confirm.No {
		return markOptions{}, errors.New("--yes and --no cannot be used together")
	}
	if applySeen && dryRunSeen {
		return markOptions{}, errors.New("--apply and --dry-run cannot be used together")
	}
	return opts, nil
}

func setMarkBool(dst **bool, value bool, name string) error {
	if *dst != nil && **dst != value {
		return fmt.Errorf("conflicting %s flags", name)
	}
	v := value
	*dst = &v
	return nil
}

func parseNewSkillArgs(args []string) (newSkillOptions, error) {
	opts := newSkillOptions{Template: "workflow"}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--template":
			if i+1 >= len(args) {
				return newSkillOptions{}, errors.New("missing template")
			}
			opts.Template = strings.TrimSpace(args[i+1])
			i++
		case strings.HasPrefix(arg, "--template="):
			opts.Template = strings.TrimSpace(strings.TrimPrefix(arg, "--template="))
		case arg == "--slug":
			if i+1 >= len(args) {
				return newSkillOptions{}, errors.New("missing slug")
			}
			opts.Slug = strings.TrimSpace(args[i+1])
			i++
		case strings.HasPrefix(arg, "--slug="):
			opts.Slug = strings.TrimSpace(strings.TrimPrefix(arg, "--slug="))
		case arg == "--name":
			if i+1 >= len(args) {
				return newSkillOptions{}, errors.New("missing name")
			}
			opts.Name = strings.TrimSpace(args[i+1])
			i++
		case strings.HasPrefix(arg, "--name="):
			opts.Name = strings.TrimSpace(strings.TrimPrefix(arg, "--name="))
		case arg == "--with-folders":
			opts.WithFolders = true
		case arg == "--yes" || arg == "-y":
			opts.Confirm.Yes = true
		case arg == "--no" || arg == "-n":
			opts.Confirm.No = true
		default:
			if strings.HasPrefix(arg, "-") {
				return newSkillOptions{}, fmt.Errorf("unknown flag %s", arg)
			}
			if opts.Path != "" {
				return newSkillOptions{}, errors.New("too many arguments")
			}
			opts.Path = strings.TrimSpace(arg)
		}
	}
	if opts.Path == "" {
		return newSkillOptions{}, errors.New("missing path")
	}
	if opts.Confirm.Yes && opts.Confirm.No {
		return newSkillOptions{}, errors.New("--yes and --no cannot be used together")
	}
	if opts.Slug != "" {
		if err := validateSlug("slug", opts.Slug); err != nil {
			return newSkillOptions{}, err
		}
	}
	return opts, nil
}

func parseLintArgs(args []string) (target, filePath string, all, strict, jsonOutput bool, err error) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--all":
			all = true
		case arg == "--strict":
			strict = true
		case arg == "--json":
			jsonOutput = true
		case arg == "--file":
			if i+1 >= len(args) {
				return "", "", false, false, false, errors.New("missing file")
			}
			filePath = strings.TrimSpace(args[i+1])
			i++
		case strings.HasPrefix(arg, "--file="):
			filePath = strings.TrimSpace(strings.TrimPrefix(arg, "--file="))
		case strings.HasPrefix(arg, "-"):
			return "", "", false, false, false, fmt.Errorf("unknown flag %s", arg)
		default:
			if target != "" {
				return "", "", false, false, false, errors.New("too many arguments")
			}
			target = strings.TrimSpace(arg)
		}
	}
	if all && (filePath != "" || target != "") {
		return "", "", false, false, false, errors.New("--all cannot be combined with --file or an identifier")
	}
	if filePath != "" && target != "" {
		return "", "", false, false, false, errors.New("--file cannot be combined with an identifier")
	}
	if !all && filePath == "" && target == "" {
		return "", "", false, false, false, errors.New("missing target")
	}
	return target, filePath, all, strict, jsonOutput, nil
}

func parseDoctorArgs(args []string) (doctorOptions, error) {
	var opts doctorOptions
	for _, arg := range args {
		switch arg {
		case "--strict":
			opts.Strict = true
		case "--refresh":
			opts.Refresh = true
		case "--fix-permissions":
			opts.FixPermissions = true
		case "--json":
			opts.JSON = true
		default:
			return doctorOptions{}, fmt.Errorf("unknown flag %s", arg)
		}
	}
	return opts, nil
}

func parseInstalledArgs(args []string) (installedOptions, error) {
	var opts installedOptions
	for _, arg := range args {
		switch arg {
		case "--strict":
			opts.Strict = true
		case "--refresh":
			opts.Refresh = true
		case "--json":
			opts.JSON = true
		default:
			return installedOptions{}, fmt.Errorf("unknown flag %s", arg)
		}
	}
	return opts, nil
}

func parseSkillsArgs(args []string) (skillsOptions, error) {
	var opts skillsOptions
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--json":
			opts.JSON = true
		case arg == "--tag":
			if i+1 >= len(args) {
				return skillsOptions{}, errors.New("missing tag")
			}
			opts.Tag = strings.ToLower(strings.TrimSpace(args[i+1]))
			i++
		case strings.HasPrefix(arg, "--tag="):
			opts.Tag = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(arg, "--tag=")))
		default:
			if strings.HasPrefix(arg, "-") {
				return skillsOptions{}, fmt.Errorf("unknown flag %s", arg)
			}
			return skillsOptions{}, errors.New("skills does not accept positional arguments")
		}
	}
	if opts.Tag != "" {
		if err := validateTagSlug(opts.Tag); err != nil {
			return skillsOptions{}, err
		}
	}
	return opts, nil
}

func parseSetsArgs(args []string) (setsOptions, error) {
	var opts setsOptions
	for _, arg := range args {
		switch {
		case arg == "--json":
			opts.JSON = true
		case strings.HasPrefix(arg, "-"):
			return setsOptions{}, fmt.Errorf("unknown flag %s", arg)
		default:
			arg = strings.TrimSpace(arg)
			if arg == "" {
				return setsOptions{}, errors.New("empty set slug")
			}
			if opts.Slug != "" {
				return setsOptions{}, errors.New("too many arguments")
			}
			opts.Slug = arg
		}
	}
	if opts.Slug != "" {
		if err := validateSlug("set slug", opts.Slug); err != nil {
			return setsOptions{}, err
		}
	}
	return opts, nil
}

func parseRefreshArgs(args []string) (target string, all bool, apply bool, strict bool, showDiff bool, jsonOutput bool, confirm confirmOptions, err error) {
	applySeen := false
	dryRunSeen := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--all":
			all = true
		case "--apply":
			apply = true
			applySeen = true
		case "--dry-run":
			apply = false
			dryRunSeen = true
		case "--strict":
			strict = true
		case "--diff":
			showDiff = true
		case "--json":
			jsonOutput = true
		case "--yes", "-y":
			confirm.Yes = true
		case "--no", "-n":
			confirm.No = true
		default:
			if strings.HasPrefix(arg, "-") {
				return "", false, false, false, false, false, confirmOptions{}, fmt.Errorf("unknown flag %s", arg)
			}
			if target != "" {
				return "", false, false, false, false, false, confirmOptions{}, errors.New("too many arguments")
			}
			target = strings.TrimSpace(arg)
		}
	}
	if confirm.Yes && confirm.No {
		return "", false, false, false, false, false, confirmOptions{}, errors.New("--yes and --no cannot be used together")
	}
	if applySeen && dryRunSeen {
		return "", false, false, false, false, false, confirmOptions{}, errors.New("--apply and --dry-run cannot be used together")
	}
	if all && target != "" {
		return "", false, false, false, false, false, confirmOptions{}, errors.New("--all cannot be combined with an identifier")
	}
	if !all && target == "" {
		return "", false, false, false, false, false, confirmOptions{}, errors.New("missing identifier")
	}
	return target, all, apply, strict, showDiff, jsonOutput, confirm, nil
}

func parsePublishArgs(args []string) (publishOptions, error) {
	var opts publishOptions
	applySeen := false
	dryRunSeen := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--slug":
			if i+1 >= len(args) {
				return publishOptions{}, errors.New("missing slug")
			}
			opts.Slug = strings.TrimSpace(args[i+1])
			i++
		case strings.HasPrefix(arg, "--slug="):
			opts.Slug = strings.TrimSpace(strings.TrimPrefix(arg, "--slug="))
		case arg == "--apply":
			opts.Apply = true
			applySeen = true
		case arg == "--dry-run":
			opts.Apply = false
			dryRunSeen = true
		case arg == "--strict":
			opts.Strict = true
		case arg == "--yes" || arg == "-y":
			opts.Confirm.Yes = true
		case arg == "--no" || arg == "-n":
			opts.Confirm.No = true
		default:
			if strings.HasPrefix(arg, "-") {
				return publishOptions{}, fmt.Errorf("unknown flag %s", arg)
			}
			if opts.Path != "" {
				return publishOptions{}, errors.New("too many arguments")
			}
			opts.Path = strings.TrimSpace(arg)
		}
	}
	if opts.Path == "" {
		return publishOptions{}, errors.New("missing path")
	}
	if opts.Confirm.Yes && opts.Confirm.No {
		return publishOptions{}, errors.New("--yes and --no cannot be used together")
	}
	if applySeen && dryRunSeen {
		return publishOptions{}, errors.New("--apply and --dry-run cannot be used together")
	}
	if opts.Slug != "" {
		if err := validateSlug("slug", opts.Slug); err != nil {
			return publishOptions{}, err
		}
	}
	return opts, nil
}

func parseSetupArgs(args []string) (setupOptions, error) {
	var opts setupOptions
	applySeen := false
	dryRunSeen := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--target":
			if i+1 >= len(args) {
				return setupOptions{}, errors.New("missing target")
			}
			opts.Target = args[i+1]
			i++
		case strings.HasPrefix(arg, "--target="):
			opts.Target = strings.TrimPrefix(arg, "--target=")
		case arg == "--set":
			if i+1 >= len(args) {
				return setupOptions{}, errors.New("missing set slug")
			}
			if opts.Mode != setupInstallAuto {
				return setupOptions{}, errors.New("choose only one install source")
			}
			opts.Mode = setupInstallSet
			opts.SetSlug = args[i+1]
			i++
		case strings.HasPrefix(arg, "--set="):
			if opts.Mode != setupInstallAuto {
				return setupOptions{}, errors.New("choose only one install source")
			}
			opts.Mode = setupInstallSet
			opts.SetSlug = strings.TrimPrefix(arg, "--set=")
		case arg == "--pinned":
			if opts.Mode != setupInstallAuto {
				return setupOptions{}, errors.New("choose only one install source")
			}
			opts.Mode = setupInstallPinned
		case arg == "--defaults":
			if opts.Mode != setupInstallAuto {
				return setupOptions{}, errors.New("choose only one install source")
			}
			opts.Mode = setupInstallDefaults
		case arg == "--apply":
			opts.Apply = true
			applySeen = true
		case arg == "--dry-run":
			opts.Apply = false
			dryRunSeen = true
		case arg == "--json":
			opts.JSON = true
		case arg == "--yes" || arg == "-y":
			opts.Confirm.Yes = true
		case arg == "--no" || arg == "-n":
			opts.Confirm.No = true
		default:
			return setupOptions{}, fmt.Errorf("unknown flag %s", arg)
		}
	}
	if opts.Confirm.Yes && opts.Confirm.No {
		return setupOptions{}, errors.New("--yes and --no cannot be used together")
	}
	if applySeen && dryRunSeen {
		return setupOptions{}, errors.New("--apply and --dry-run cannot be used together")
	}
	if opts.Mode == setupInstallSet {
		if err := validateSlug("set slug", opts.SetSlug); err != nil {
			return setupOptions{}, err
		}
	}
	return opts, nil
}

func parsePublicSearchArgs(args []string) (string, int, error) {
	query, limit, err := parseTextWithLimitArgs(args, 20)
	if err != nil {
		return "", 0, err
	}
	if query == "" {
		return "", 0, errors.New("missing query")
	}
	return query, limit, nil
}

func parseImportSourceArgs(args []string) (importSourceOptions, error) {
	var opts importSourceOptions
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--install":
			opts.Install = true
		case arg == "--target":
			if i+1 >= len(args) {
				return importSourceOptions{}, errors.New("missing target")
			}
			opts.Target = strings.TrimSpace(args[i+1])
			i++
		case strings.HasPrefix(arg, "--target="):
			opts.Target = strings.TrimSpace(strings.TrimPrefix(arg, "--target="))
		case arg == "--dry-run":
			opts.Confirm.DryRun = true
		case arg == "--yes" || arg == "-y":
			opts.Confirm.Yes = true
		case arg == "--no" || arg == "-n":
			opts.Confirm.No = true
		default:
			if strings.HasPrefix(arg, "-") {
				return importSourceOptions{}, fmt.Errorf("unknown flag %s", arg)
			}
			if opts.Source != "" {
				return importSourceOptions{}, errors.New("too many arguments")
			}
			opts.Source = strings.TrimSpace(arg)
		}
	}
	if opts.Source == "" {
		return importSourceOptions{}, errors.New("missing source")
	}
	if opts.Confirm.Yes && opts.Confirm.No {
		return importSourceOptions{}, errors.New("--yes and --no cannot be used together")
	}
	if !opts.Install && (opts.Target != "" || opts.Confirm.DryRun || opts.Confirm.Yes || opts.Confirm.No) {
		return importSourceOptions{}, errors.New("--target, --dry-run, --yes, and --no require --install")
	}
	return opts, nil
}

func parseLimitOnlyArgs(args []string, fallback int) (int, error) {
	query, limit, err := parseTextWithLimitArgs(args, fallback)
	if err != nil {
		return 0, err
	}
	if query != "" {
		return 0, errors.New("too many arguments")
	}
	return limit, nil
}

func parseTextWithLimitArgs(args []string, fallback int) (string, int, error) {
	limit := fallback
	var parts []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--limit" {
			if i+1 >= len(args) {
				return "", 0, errors.New("missing limit")
			}
			parsed, err := strconv.Atoi(args[i+1])
			if err != nil {
				return "", 0, errors.New("invalid limit")
			}
			limit = parsed
			i++
			continue
		}
		if strings.HasPrefix(arg, "--limit=") {
			parsed, err := strconv.Atoi(strings.TrimPrefix(arg, "--limit="))
			if err != nil {
				return "", 0, errors.New("invalid limit")
			}
			limit = parsed
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return "", 0, fmt.Errorf("unknown flag %s", arg)
		}
		parts = append(parts, arg)
	}
	if limit <= 0 {
		return "", 0, errors.New("limit must be positive")
	}
	if limit > 100 {
		limit = 100
	}
	return strings.TrimSpace(strings.Join(parts, " ")), limit, nil
}

func parseGitHubExportArgs(args []string) (string, confirmOptions, error) {
	dir := "skillsdex-github-export"
	var confirm confirmOptions
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--dir" {
			if i+1 >= len(args) {
				return "", confirmOptions{}, errors.New("missing dir")
			}
			dir = strings.TrimSpace(args[i+1])
			i++
			continue
		}
		if strings.HasPrefix(arg, "--dir=") {
			dir = strings.TrimSpace(strings.TrimPrefix(arg, "--dir="))
			continue
		}
		if arg == "--yes" || arg == "-y" {
			confirm.Yes = true
			continue
		}
		if arg == "--no" || arg == "-n" {
			confirm.No = true
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return "", confirmOptions{}, fmt.Errorf("unknown flag %s", arg)
		}
		return "", confirmOptions{}, errors.New("too many arguments")
	}
	if dir == "" {
		return "", confirmOptions{}, errors.New("dir is required")
	}
	if confirm.Yes && confirm.No {
		return "", confirmOptions{}, errors.New("--yes and --no cannot be used together")
	}
	return dir, confirm, nil
}

func parseBackupExportArgs(args []string) (string, error) {
	out := ""
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--out" {
			if i+1 >= len(args) {
				return "", errors.New("missing out")
			}
			out = strings.TrimSpace(args[i+1])
			i++
			continue
		}
		if strings.HasPrefix(arg, "--out=") {
			out = strings.TrimSpace(strings.TrimPrefix(arg, "--out="))
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return "", fmt.Errorf("unknown flag %s", arg)
		}
		return "", errors.New("too many arguments")
	}
	if out == "" {
		return "", errors.New("out is required")
	}
	return out, nil
}

func parseBackupFileArgs(args []string, allowDryRun bool) (backupFileOptions, error) {
	var opts backupFileOptions
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--file" {
			if i+1 >= len(args) {
				return backupFileOptions{}, errors.New("missing file")
			}
			opts.File = strings.TrimSpace(args[i+1])
			i++
			continue
		}
		if strings.HasPrefix(arg, "--file=") {
			opts.File = strings.TrimSpace(strings.TrimPrefix(arg, "--file="))
			continue
		}
		if arg == "--dry-run" {
			if !allowDryRun {
				return backupFileOptions{}, fmt.Errorf("unknown flag %s", arg)
			}
			opts.DryRun = true
			continue
		}
		if arg == "--json" {
			opts.JSON = true
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return backupFileOptions{}, fmt.Errorf("unknown flag %s", arg)
		}
		return backupFileOptions{}, errors.New("too many arguments")
	}
	if opts.File == "" {
		return backupFileOptions{}, errors.New("file is required")
	}
	return opts, nil
}

func parseTargetArgs(args []string, allowDryRun bool) (string, string, confirmOptions, error) {
	var identifier string
	var target string
	var confirm confirmOptions
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--target" {
			if i+1 >= len(args) {
				return "", "", confirmOptions{}, errors.New("missing target")
			}
			target = args[i+1]
			i++
			continue
		}
		if strings.HasPrefix(arg, "--target=") {
			target = strings.TrimPrefix(arg, "--target=")
			continue
		}
		if arg == "--yes" || arg == "-y" {
			confirm.Yes = true
			continue
		}
		if arg == "--no" || arg == "-n" {
			confirm.No = true
			continue
		}
		if arg == "--dry-run" && allowDryRun {
			confirm.DryRun = true
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return "", "", confirmOptions{}, fmt.Errorf("unknown flag %s", arg)
		}
		if identifier != "" {
			return "", "", confirmOptions{}, errors.New("too many arguments")
		}
		identifier = arg
	}
	if confirm.Yes && confirm.No {
		return "", "", confirmOptions{}, errors.New("--yes and --no cannot be used together")
	}
	if identifier == "" {
		return "", "", confirmOptions{}, errors.New("missing identifier")
	}
	return identifier, target, confirm, nil
}

func parseInstallDefaultsArgs(args []string) (string, confirmOptions, error) {
	var target string
	var confirm confirmOptions
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--target" {
			if i+1 >= len(args) {
				return "", confirmOptions{}, errors.New("missing target")
			}
			target = args[i+1]
			i++
			continue
		}
		if strings.HasPrefix(arg, "--target=") {
			target = strings.TrimPrefix(arg, "--target=")
			continue
		}
		if arg == "--yes" || arg == "-y" {
			confirm.Yes = true
			continue
		}
		if arg == "--no" || arg == "-n" {
			confirm.No = true
			continue
		}
		if arg == "--dry-run" {
			confirm.DryRun = true
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return "", confirmOptions{}, fmt.Errorf("unknown flag %s", arg)
		}
		return "", confirmOptions{}, errors.New("too many arguments")
	}
	if confirm.Yes && confirm.No {
		return "", confirmOptions{}, errors.New("--yes and --no cannot be used together")
	}
	return target, confirm, nil
}

func parseSyncArgs(args []string) (syncOptions, error) {
	var opts syncOptions
	for _, arg := range args {
		switch arg {
		case "--dry-run":
			opts.Confirm.DryRun = true
		case "--strict":
			opts.Strict = true
		case "--json":
			opts.JSON = true
		case "--yes", "-y":
			opts.Confirm.Yes = true
		case "--no", "-n":
			opts.Confirm.No = true
		default:
			return syncOptions{}, fmt.Errorf("unknown flag %s", arg)
		}
	}
	if opts.Confirm.Yes && opts.Confirm.No {
		return syncOptions{}, errors.New("--yes and --no cannot be used together")
	}
	return opts, nil
}
