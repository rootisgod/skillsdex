package skillsdex

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"text/tabwriter"
)

func runDoctor(args ...string) error {
	opts, err := parseDoctorArgs(args)
	if err != nil {
		return errors.New("usage: skillsdex doctor [--strict] [--refresh] [--fix-permissions] [--json]")
	}
	report, issues, err := buildDoctorReport(opts)
	if err != nil {
		return err
	}
	return finishDoctorReport(opts, report, issues)
}

func buildDoctorReport(opts doctorOptions) (doctorReport, []string, error) {
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
	if codexPath, err := exec.LookPath("codex"); err == nil {
		report.CodexCLI = doctorCheckReport{Status: "found", Value: codexPath}
	} else {
		report.CodexCLI = doctorCheckReport{Status: "not-found", Message: "install Codex CLI to use installed skills with Codex"}
	}
	if opts.FixPermissions {
		if fixed, issue := fixDoctorPrivateFilePermissions(authPath, "auth file"); issue != "" {
			issues = append(issues, issue)
		} else if fixed != "" {
			report.FixedPermissions = append(report.FixedPermissions, fixed)
		}
		if fixed, issue := fixDoctorPrivateFilePermissions(manifestPath, "manifest file"); issue != "" {
			issues = append(issues, issue)
		} else if fixed != "" {
			report.FixedPermissions = append(report.FixedPermissions, fixed)
		}
		if fixed, issue := fixDoctorPrivateFilePermissions(configPath, "config file"); issue != "" {
			issues = append(issues, issue)
		} else if fixed != "" {
			report.FixedPermissions = append(report.FixedPermissions, fixed)
		}
	}
	report.AuthPath = doctorPrivateFileReport(authPath, "auth file")
	report.ManifestPath = doctorPrivateFileReport(manifestPath, "manifest file")
	report.ConfigPath = doctorPrivateFileReport(configPath, "config file")
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
		issues = append(issues, "local config unavailable")
	} else if err := validateTarget(cfg.DefaultTarget); err != nil {
		report.DefaultTarget = doctorCheckReport{Status: "invalid", Value: cfg.DefaultTarget, Message: err.Error()}
		issues = append(issues, "default target invalid")
	} else {
		report.DefaultTarget = doctorCheckReport{Status: "ok", Value: cfg.DefaultTarget}
	}
	var client *apiClient
	authReady := false
	if authErr == nil {
		report.Auth = doctorCheckReport{Status: "configured", Value: auth.ServerURL}
		client = newAPIClient(auth)
		var me map[string]any
		if err := client.getJSON("/v1/me", &me); err != nil {
			report.Server = doctorCheckReport{Status: "unreachable", Message: err.Error()}
			issues = append(issues, "server unreachable or API token rejected")
			client = nil
		} else {
			username := fmt.Sprint(me["username"])
			report.Server = doctorCheckReport{Status: "reachable", Value: username}
			authReady = true
		}
	} else {
		report.Auth = doctorCheckReport{Status: "not-configured", Message: authErr.Error()}
		report.Server = doctorCheckReport{Status: "not-checked", Message: "auth not configured"}
		issues = append(issues, "auth not configured")
	}
	if client != nil {
		var list struct {
			Skills []Skill `json:"skills"`
		}
		if err := client.getJSON("/v1/skills", &list); err != nil {
			report.Library = doctorLibraryReport{Status: "unavailable", Message: err.Error()}
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
		}
	} else {
		report.Library = doctorLibraryReport{Status: "not-checked"}
	}
	if manifestErr != nil {
		report.ManagedInstalls = doctorManagedInstallsReport{Status: "unavailable", Message: manifestErr.Error()}
		issues = append(issues, "managed install manifest unavailable")
	} else {
		report.ManagedInstalls = doctorManagedInstallsReport{Status: "ok", Count: len(manifest.Installs)}
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
			for status, count := range counts {
				if status != "clean" && count > 0 {
					issues = append(issues, fmt.Sprintf("managed install status %s=%d", status, count))
				}
			}
			if opts.Refresh {
				report.ManagedInstalls.SourceRefreshCounts = sourceCounts
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
		issues = append(issues, "Codex skills directory unavailable")
		report.NextSteps = buildDoctorNextSteps(authReady, manifest)
		return completeDoctorReport(report, issues), issues, nil
	}
	report.CodexSkillsDirectory = doctorCheckReport{Status: "found", Value: codexDir}
	if err := os.MkdirAll(codexDir, 0700); err != nil {
		report.CodexSkillsDirectory.Status = "not-writable"
		report.CodexSkillsDirectory.Message = err.Error()
		issues = append(issues, "Codex skills directory is not writable")
		report.NextSteps = buildDoctorNextSteps(authReady, manifest)
		return completeDoctorReport(report, issues), issues, nil
	}
	test := filepath.Join(codexDir, ".skillsdex-write-test")
	if err := os.WriteFile(test, []byte("ok"), 0600); err != nil {
		report.CodexSkillsDirectory.Status = "not-writable"
		report.CodexSkillsDirectory.Message = err.Error()
		issues = append(issues, "Codex skills directory is not writable")
	} else {
		_ = os.Remove(test)
		report.CodexSkillsDirectory.Status = "writable"
	}
	report.NextSteps = buildDoctorNextSteps(authReady, manifest)
	return completeDoctorReport(report, issues), issues, nil
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

func completeDoctorReport(report doctorReport, issues []string) doctorReport {
	report.Issues = issues
	report.IssueCount = len(issues)
	report.Healthy = len(issues) == 0
	return report
}

func finishDoctorReport(opts doctorOptions, report doctorReport, issues []string) error {
	report = completeDoctorReport(report, issues)
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
	printDoctorHumanReport(report)
	printDoctorNextSteps(report.NextSteps)
	return finishDoctor(opts.Strict, issues)
}

func printDoctorHumanReport(report doctorReport) {
	fmt.Println("SkillsDex doctor")
	fmt.Println()
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "Check\tStatus\tDetail")
	fmt.Fprintln(w, "-----\t------\t------")
	fmt.Fprintf(w, "Auth\t%s\t%s\n", doctorHumanStatus(report.Auth.Status), doctorAuthTableDetail(report))
	fmt.Fprintf(w, "Server\t%s\t%s\n", doctorHumanStatus(report.Server.Status), doctorServerDetail(report.Server))
	fmt.Fprintf(w, "Library\t%s\t%s\n", doctorHumanStatus(report.Library.Status), doctorLibraryDetail(report.Library))
	fmt.Fprintf(w, "Managed installs\t%s\t%s\n", doctorHumanStatus(report.ManagedInstalls.Status), doctorManagedInstallsDetail(report.ManagedInstalls))
	if report.Refresh {
		fmt.Fprintf(w, "Source refresh\t%s\t%s\n", doctorSourceRefreshStatus(report.ManagedInstalls.SourceRefreshCounts), doctorSourceRefreshDetail(report.ManagedInstalls.SourceRefreshCounts))
	}
	fmt.Fprintf(w, "Codex CLI\t%s\t%s\n", doctorHumanStatus(report.CodexCLI.Status), doctorCheckDetail(report.CodexCLI))
	fmt.Fprintf(w, "Codex skills directory\t%s\t%s\n", doctorHumanStatus(report.CodexSkillsDirectory.Status), doctorCheckDetail(report.CodexSkillsDirectory))
	fmt.Fprintf(w, "Default target\t%s\t%s\n", doctorHumanStatus(report.DefaultTarget.Status), doctorCheckDetail(report.DefaultTarget))
	fmt.Fprintf(w, "Local files\t%s\t%s\n", doctorLocalFilesStatus(report), doctorLocalFilesDetail(report))
	_ = w.Flush()
	fmt.Println()

	if len(report.FixedPermissions) > 0 {
		fmt.Println("Fixed:")
		for _, fixed := range report.FixedPermissions {
			fmt.Printf("  %s\n", fixed)
		}
		fmt.Println()
	}

	fmt.Println("Details:")
	fmt.Printf("  SkillsDex directory: %s\n", report.SkillsDexDirectory)
	fmt.Printf("  Auth path: %s%s\n", report.AuthPath.Path, doctorPathHumanSuffix(report.AuthPath))
	fmt.Printf("  Manifest path: %s%s\n", report.ManifestPath.Path, doctorPathHumanSuffix(report.ManifestPath))
	fmt.Printf("  Config path: %s%s\n", report.ConfigPath.Path, doctorPathHumanSuffix(report.ConfigPath))
	fmt.Printf("  Default target: %s\n", doctorDefaultTargetDetail(report.DefaultTarget))
	fmt.Printf("  Auth: %s\n", doctorAuthDetail(report))
	fmt.Printf("  Server: %s\n", doctorServerDetail(report.Server))
	fmt.Printf("  Library: %s\n", doctorLibraryDetail(report.Library))
	fmt.Printf("  Managed installs: %s\n", doctorManagedInstallsCountDetail(report.ManagedInstalls))
	if detail := doctorInstallStatusDetail(report.ManagedInstalls); detail != "" {
		fmt.Printf("  Install status: %s\n", detail)
	}
	if report.Refresh {
		fmt.Printf("  Source refresh: %s\n", doctorSourceRefreshDetail(report.ManagedInstalls.SourceRefreshCounts))
	}
	fmt.Printf("  Codex CLI: %s\n", doctorCodexCLIDetail(report.CodexCLI))
	fmt.Printf("  Codex skills directory: %s\n", doctorCodexSkillsDirectoryDetail(report.CodexSkillsDirectory))
	fmt.Printf("  Codex skills directory writable: %s\n", doctorCodexSkillsDirectoryWritable(report.CodexSkillsDirectory))
	fmt.Println()
}

func doctorHumanStatus(status string) string {
	switch status {
	case "ok", "configured", "reachable", "writable", "private", "found", "current":
		return "ok"
	case "not-found", "not-configured", "not-checked", "empty":
		return status
	case "":
		return "not-checked"
	default:
		return status
	}
}

func doctorCheckDetail(check doctorCheckReport) string {
	if check.Value != "" {
		return check.Value
	}
	if check.Message != "" {
		return check.Message
	}
	return "-"
}

func doctorAuthDetail(report doctorReport) string {
	switch report.Auth.Status {
	case "configured":
		return "configured for " + report.Auth.Value
	case "not-configured":
		return "not configured (" + report.Auth.Message + ")"
	default:
		return doctorCheckDetail(report.Auth)
	}
}

func doctorAuthTableDetail(report doctorReport) string {
	switch report.Auth.Status {
	case "configured":
		return report.Auth.Value
	case "not-configured":
		return "not logged in; run the login command in Next steps"
	default:
		return doctorCheckDetail(report.Auth)
	}
}

func doctorServerDetail(check doctorCheckReport) string {
	switch check.Status {
	case "reachable":
		return "reachable as " + check.Value
	case "unreachable":
		return "unreachable or token rejected (" + check.Message + ")"
	case "":
		return "not checked"
	case "not-checked":
		return "not checked until auth is configured"
	default:
		return doctorCheckDetail(check)
	}
}

func doctorLibraryDetail(report doctorLibraryReport) string {
	switch report.Status {
	case "ok":
		return fmt.Sprintf("%d active skill(s), %d recommended default, %d pinned", report.Active, report.Recommended, report.Pinned)
	case "not-checked":
		return "not checked until auth/server is working"
	case "unavailable":
		return "unavailable (" + report.Message + ")"
	default:
		return doctorDash(report.Message)
	}
}

func doctorManagedInstallsCountDetail(report doctorManagedInstallsReport) string {
	switch report.Status {
	case "unavailable":
		return "unavailable (" + report.Message + ")"
	default:
		return strconv.Itoa(report.Count)
	}
}

func doctorManagedInstallsDetail(report doctorManagedInstallsReport) string {
	if report.Status == "unavailable" {
		return "unavailable (" + report.Message + ")"
	}
	if detail := doctorInstallStatusDetail(report); detail != "" {
		return fmt.Sprintf("%d install(s); %s", report.Count, detail)
	}
	return fmt.Sprintf("%d install(s)", report.Count)
}

func doctorInstallStatusDetail(report doctorManagedInstallsReport) string {
	statuses := []string{"clean", "update-available", "modified", "missing", "remote-archived", "remote-deleted"}
	var parts []string
	for _, status := range statuses {
		if report.StatusCounts[status] > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", status, report.StatusCounts[status]))
		}
	}
	return strings.Join(parts, ", ")
}

func doctorSourceRefreshStatus(counts map[string]int) string {
	if len(counts) == 0 {
		return "not-checked"
	}
	if counts["refresh-failed"] > 0 {
		return "refresh-failed"
	}
	if counts["update-available"] > 0 {
		return "update-available"
	}
	if counts["current"] > 0 {
		return "ok"
	}
	return "not-checked"
}

func doctorSourceRefreshDetail(counts map[string]int) string {
	statuses := []string{"current", "update-available", "refresh-failed"}
	var parts []string
	for _, status := range statuses {
		if counts[status] > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", status, counts[status]))
		}
	}
	if len(parts) == 0 {
		return "no imported managed installs checked"
	}
	return strings.Join(parts, ", ")
}

func doctorLocalFilesStatus(report doctorReport) string {
	for _, path := range []doctorPathReport{report.AuthPath, report.ManifestPath, report.ConfigPath} {
		if path.Issue != "" {
			return "needs-attention"
		}
	}
	return "ok"
}

func doctorLocalFilesDetail(report doctorReport) string {
	return fmt.Sprintf("auth=%s, manifest=%s, config=%s", report.AuthPath.Status, report.ManifestPath.Status, report.ConfigPath.Status)
}

func doctorDefaultTargetDetail(check doctorCheckReport) string {
	switch check.Status {
	case "ok":
		return check.Value
	case "invalid":
		return "invalid (" + check.Message + ")"
	case "unavailable":
		return "unavailable (" + check.Message + ")"
	default:
		return doctorCheckDetail(check)
	}
}

func doctorCodexCLIDetail(check doctorCheckReport) string {
	switch check.Status {
	case "found":
		return "found at " + check.Value
	case "not-found":
		return "not found; " + check.Message
	default:
		return doctorCheckDetail(check)
	}
}

func doctorCodexSkillsDirectoryDetail(check doctorCheckReport) string {
	switch check.Status {
	case "writable":
		return check.Value + " (writable)"
	case "not-writable":
		return check.Value + " (not writable: " + check.Message + ")"
	case "unavailable":
		return "unavailable (" + check.Message + ")"
	default:
		return doctorCheckDetail(check)
	}
}

func doctorCodexSkillsDirectoryWritable(check doctorCheckReport) string {
	switch check.Status {
	case "writable":
		return "yes"
	case "not-writable":
		return "no (" + check.Message + ")"
	default:
		return "unknown"
	}
}

func doctorDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
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
