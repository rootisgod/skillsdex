package skillsdex

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	runErr := fn()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = old
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out), runErr
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func withStdin(t *testing.T, input string, fn func() error) error {
	t.Helper()
	old := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(input)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	defer func() {
		os.Stdin = old
		_ = r.Close()
	}()
	return fn()
}

func TestBinaryEntryPointsReportVersion(t *testing.T) {
	out, err := captureStdout(t, func() error {
		if code := RunCLI([]string{"version"}); code != 0 {
			t.Fatalf("RunCLI version exit code = %d, want 0", code)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != version {
		t.Fatalf("RunCLI version output = %q, want %q", strings.TrimSpace(out), version)
	}

	out, err = captureStdout(t, func() error {
		if code := RunDaemon([]string{"version"}); code != 0 {
			t.Fatalf("RunDaemon version exit code = %d, want 0", code)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != version {
		t.Fatalf("RunDaemon version output = %q, want %q", strings.TrimSpace(out), version)
	}
}

func TestDaemonRejectsUnknownSubcommand(t *testing.T) {
	if code := RunDaemon([]string{"unknown"}); code != 2 {
		t.Fatalf("RunDaemon unknown exit code = %d, want 2", code)
	}
}

func TestRunNewSkillScaffoldsTemplateFolder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	target := filepath.Join(home, "review-skill")
	out, err := captureStdout(t, func() error {
		return runNewSkill([]string{target, "--template", "review", "--slug", "review-skill", "--name", "Review Skill", "--with-folders"})
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(target, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`name: "Review Skill"`, "description: \"Use when reviewing a change", "# Focused Review"} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("scaffolded skill missing %q:\n%s", want, raw)
		}
	}
	for _, dir := range []string{"references", "scripts", "assets", "templates"} {
		if info, err := os.Stat(filepath.Join(target, dir)); err != nil || !info.IsDir() {
			t.Fatalf("expected supporting dir %s, info=%v err=%v", dir, info, err)
		}
	}
	if !strings.Contains(out, "skillsdex lint --file") || !strings.Contains(out, "skillsdex publish") {
		t.Fatalf("new output missing next steps:\n%s", out)
	}
}

func TestRunNewSkillRefusesOverwriteWithoutConfirmation(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, "review-skill")
	if err := os.MkdirAll(target, 0700); err != nil {
		t.Fatal(err)
	}
	skillPath := filepath.Join(target, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("existing\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := captureStdout(t, func() error {
		return runNewSkill([]string{target, "--no"})
	}); err == nil || !strings.Contains(err.Error(), "declined") {
		t.Fatalf("new overwrite err = %v, want declined", err)
	}
	raw, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "existing\n" {
		t.Fatalf("overwrite refusal changed file:\n%s", raw)
	}
}

func TestRunTemplatesListsScaffoldChoices(t *testing.T) {
	out, err := captureStdout(t, func() error {
		return runTemplates(nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"TEMPLATE", "workflow", "New Workflow", "review", "Focused Review", "research", "research-brief"} {
		if !strings.Contains(out, want) {
			t.Fatalf("templates output missing %q:\n%s", want, out)
		}
	}
	if _, err := captureStdout(t, func() error {
		return runTemplates([]string{"extra"})
	}); err == nil || !strings.Contains(err.Error(), "usage: skillsdex templates") {
		t.Fatalf("templates extra arg err = %v, want usage", err)
	}
}

func TestRunNewSkillReportsAvailableTemplates(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, "bad-template")
	if _, err := captureStdout(t, func() error {
		return runNewSkill([]string{target, "--template", "unknown", "--yes"})
	}); err == nil || !strings.Contains(err.Error(), "choose one of: workflow, review, research") {
		t.Fatalf("new unknown template err = %v, want choices", err)
	}
}

func TestRunConfigManagesDefaultTarget(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	out, err := captureStdout(t, func() error {
		return runConfig([]string{"get", "default_target"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != "codex" {
		t.Fatalf("default target output = %q, want codex", out)
	}

	out, err = captureStdout(t, func() error {
		return runConfig([]string{"set", "default_target", "codex"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "default_target = codex") {
		t.Fatalf("config set output missing target:\n%s", out)
	}
	path, err := configPath()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("config file mode = %o, want 0600", got)
	}
	cfg, err := loadLocalConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DefaultTarget != "codex" {
		t.Fatalf("default target = %q, want codex", cfg.DefaultTarget)
	}
	if _, err := captureStdout(t, func() error {
		return runConfig([]string{"set", "default_target", "unknown"})
	}); err == nil || !strings.Contains(err.Error(), "unknown target") {
		t.Fatalf("config set invalid target err = %v, want unknown target", err)
	}
}

func TestInstallCommandsResolveConfiguredDefaultTarget(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	if err := saveLocalConfig(LocalConfig{DefaultTarget: "unknown"}); err != nil {
		t.Fatal(err)
	}
	if err := runInstallDefaults(nil); err == nil || !strings.Contains(err.Error(), "unknown target") {
		t.Fatalf("install-defaults with invalid configured target err = %v, want unknown target", err)
	}
}

func TestRunInstallDefaultsInstallsRecommendedSkills(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	admin, err := getUserByUsername(context.Background(), db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	token, err := createAPIToken(context.Background(), db, admin.ID, "test")
	if err != nil {
		t.Fatal(err)
	}

	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	if err := saveAuth(AuthConfig{ServerURL: server.URL, Token: token}); err != nil {
		t.Fatal(err)
	}
	if err := runInstallDefaults([]string{"--target", "codex"}); err != nil {
		t.Fatal(err)
	}

	manifest, err := loadManifest()
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Installs) != len(defaultSkills()) {
		t.Fatalf("install count = %d, want %d", len(manifest.Installs), len(defaultSkills()))
	}
	wantPath := filepath.Join(home, ".agents", "skills", "admin--five-advisor-review", "SKILL.md")
	if _, err := os.Stat(wantPath); err != nil {
		t.Fatalf("expected installed skill at %s: %v", wantPath, err)
	}
}

func TestInstallSkillResponseHonorsYesNoForUnmanagedOverwrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	resp := skillResponse{
		Skill: Skill{
			OwnerSlug:         "admin",
			Slug:              "overwrite-me",
			CurrentRevisionID: "rev_new",
		},
		Raw: "---\nname: overwrite-me\ndescription: Use when testing overwrites.\n---\n\n# New\n",
	}
	path := filepath.Join(home, ".agents", "skills", "admin--overwrite-me", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# Existing\n"), 0600); err != nil {
		t.Fatal(err)
	}

	manifest := Manifest{Version: 1}
	if _, err := installSkillResponse(&manifest, resp, "codex", confirmOptions{No: true}); err == nil || !strings.Contains(err.Error(), "--no") {
		t.Fatalf("install with --no err = %v, want declined by --no", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "# Existing\n" {
		t.Fatalf("--no changed existing file:\n%s", got)
	}

	if _, err := installSkillResponse(&manifest, resp, "codex", confirmOptions{Yes: true}); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != resp.Raw {
		t.Fatalf("--yes did not overwrite file:\n%s", got)
	}
}

func TestInstallSkillResponseDryRunDoesNotPromptForOverwriteOrRemoval(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	resp := skillResponse{
		Skill: Skill{
			OwnerSlug:         "admin",
			Slug:              "dry-run-overwrite",
			CurrentRevisionID: "rev_new",
		},
		Raw: "# New\n",
	}
	path := filepath.Join(home, ".agents", "skills", "admin--dry-run-overwrite", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# Existing\n"), 0600); err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{Version: 1}
	out, err := captureStdout(t, func() error {
		_, err := installSkillResponse(&manifest, resp, "codex", confirmOptions{DryRun: true})
		return err
	})
	if err != nil {
		t.Fatalf("dry-run should not prompt/fail for unmanaged overwrite: %v", err)
	}
	if !strings.Contains(out, "Would install admin/dry-run-overwrite") {
		t.Fatalf("dry-run output missing install preview:\n%s", out)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "# Existing\n" {
		t.Fatalf("dry-run changed unmanaged file:\n%s", got)
	}
	if len(manifest.Installs) != 0 {
		t.Fatalf("dry-run mutated manifest: %#v", manifest.Installs)
	}

	initial := skillResponse{
		Skill: Skill{
			OwnerSlug:         "admin",
			Slug:              "dry-run-removal",
			CurrentRevisionID: "rev_old",
		},
		Files: []SkillFile{
			{Path: "SKILL.md", Content: "# Old\n"},
			{Path: "references/stale.md", Content: "# Stale\n"},
		},
	}
	manifest = Manifest{Version: 1}
	mainPath, err := installSkillResponse(&manifest, initial, "codex", confirmOptions{Yes: true})
	if err != nil {
		t.Fatal(err)
	}
	stalePath := filepath.Join(filepath.Dir(mainPath), "references", "stale.md")
	if err := os.WriteFile(stalePath, []byte("# Local stale edit\n"), 0600); err != nil {
		t.Fatal(err)
	}
	next := skillResponse{
		Skill: Skill{
			OwnerSlug:         "admin",
			Slug:              "dry-run-removal",
			CurrentRevisionID: "rev_new",
		},
		Files: []SkillFile{{Path: "SKILL.md", Content: "# Old\n"}},
	}
	beforeRevision := manifest.Installs[0].RemoteRevision
	if _, err := captureStdout(t, func() error {
		_, err := installSkillResponse(&manifest, next, "codex", confirmOptions{DryRun: true})
		return err
	}); err != nil {
		t.Fatalf("dry-run should not prompt/fail for stale managed removal: %v", err)
	}
	stale, err := os.ReadFile(stalePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(stale) != "# Local stale edit\n" {
		t.Fatalf("dry-run changed stale managed file:\n%s", stale)
	}
	if manifest.Installs[0].RemoteRevision != beforeRevision {
		t.Fatalf("dry-run changed manifest revision = %q, want %q", manifest.Installs[0].RemoteRevision, beforeRevision)
	}
}

func TestInstallSkillResponseWritesSupportingFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	resp := skillResponse{
		Skill: Skill{
			OwnerSlug:         "admin",
			Slug:              "folder-skill",
			CurrentRevisionID: "rev_folder",
		},
		Raw: "---\nname: Folder Skill\ndescription: Use when testing folders.\n---\n\n# Folder Skill\n",
		Files: []SkillFile{
			{Path: "SKILL.md", Content: "---\nname: Folder Skill\ndescription: Use when testing folders.\n---\n\n# Folder Skill\n"},
			{Path: "references/guide.md", Content: "# Guide\n"},
			{Path: "scripts/run.sh", Content: "#!/bin/sh\n", Executable: true},
		},
	}
	manifest := Manifest{Version: 1}
	mainPath, err := installSkillResponse(&manifest, resp, "codex", confirmOptions{Yes: true})
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Dir(mainPath)
	guide, err := os.ReadFile(filepath.Join(base, "references", "guide.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(guide) != "# Guide\n" {
		t.Fatalf("guide content = %q", guide)
	}
	info, err := os.Stat(filepath.Join(base, "scripts", "run.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0700 {
		t.Fatalf("script mode = %v, want 0700", info.Mode().Perm())
	}
	if len(manifest.Installs) != 1 || len(manifest.Installs[0].Files) != 3 {
		t.Fatalf("manifest did not track package files: %#v", manifest.Installs)
	}
	if manifest.Installs[0].Files[2].Path != "scripts/run.sh" || !manifest.Installs[0].Files[2].Executable {
		t.Fatalf("manifest did not track executable file: %#v", manifest.Installs[0].Files)
	}
}

func TestInstallSkillResponseRequiresTrustForNewExecutableFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	resp := skillResponse{
		Skill: Skill{
			OwnerSlug:         "admin",
			Slug:              "exec-skill",
			CurrentRevisionID: "rev_exec",
		},
		Raw: "---\nname: Exec Skill\ndescription: Use when testing executable trust.\n---\n\n# Exec Skill\n",
		Files: []SkillFile{
			{Path: "SKILL.md", Content: "---\nname: Exec Skill\ndescription: Use when testing executable trust.\n---\n\n# Exec Skill\n"},
			{Path: "scripts/run.sh", Content: "#!/bin/sh\n", Executable: true},
		},
	}
	manifest := Manifest{Version: 1}
	if _, err := installSkillResponse(&manifest, resp, "codex", confirmOptions{No: true}); err == nil || !strings.Contains(err.Error(), "--no") {
		t.Fatalf("install executable with --no err = %v, want declined by --no", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "admin--exec-skill", "scripts", "run.sh")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("executable script should not be installed, stat err=%v", err)
	}
}

func TestInstallSkillResponseDryRunPrintsPackageFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	resp := skillResponse{
		Skill: Skill{
			OwnerSlug:         "admin",
			Slug:              "dry-run-folder",
			CurrentRevisionID: "rev_dry_run_folder",
		},
		Files: []SkillFile{
			{Path: "SKILL.md", Content: "# Dry Run Folder\n"},
			{Path: "references/guide.md", Content: "# Guide\n"},
			{Path: "scripts/run.sh", Content: "#!/bin/sh\n", Executable: true},
		},
	}
	manifest := Manifest{Version: 1}
	out, err := captureStdout(t, func() error {
		_, err := installSkillResponse(&manifest, resp, "codex", confirmOptions{DryRun: true})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Would install admin/dry-run-folder to ",
		"Package files:",
		"  SKILL.md (17 bytes)",
		"  references/guide.md (8 bytes)",
		"  scripts/run.sh (10 bytes executable)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("install dry-run output missing %q:\n%s", want, out)
		}
	}
	if len(manifest.Installs) != 0 {
		t.Fatalf("dry-run mutated manifest: %#v", manifest.Installs)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "admin--dry-run-folder", "SKILL.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run wrote package files, stat err=%v", err)
	}
}

func TestRunUninstallRemovesSupportingFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	resp := skillResponse{
		Skill: Skill{
			OwnerSlug:         "admin",
			Slug:              "uninstall-folder",
			CurrentRevisionID: "rev_uninstall",
		},
		Raw: "---\nname: Uninstall Folder\ndescription: Use when testing uninstall.\n---\n\n# Uninstall Folder\n",
		Files: []SkillFile{
			{Path: "SKILL.md", Content: "---\nname: Uninstall Folder\ndescription: Use when testing uninstall.\n---\n\n# Uninstall Folder\n"},
			{Path: "references/guide.md", Content: "# Guide\n"},
			{Path: "scripts/run.sh", Content: "#!/bin/sh\n", Executable: true},
		},
	}
	manifest := Manifest{Version: 1}
	mainPath, err := installSkillResponse(&manifest, resp, "codex", confirmOptions{Yes: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := saveManifest(manifest); err != nil {
		t.Fatal(err)
	}
	base := filepath.Dir(mainPath)
	if err := runUninstall([]string{"admin/uninstall-folder", "--target", "codex", "--yes"}); err != nil {
		t.Fatal(err)
	}
	for _, rel := range []string{"SKILL.md", "references/guide.md", "scripts/run.sh"} {
		if _, err := os.Stat(filepath.Join(base, rel)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s should be removed, stat err=%v", rel, err)
		}
	}
	after, err := loadManifest()
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Installs) != 0 {
		t.Fatalf("manifest still has installs: %#v", after.Installs)
	}
}

func TestRunUninstallProtectsModifiedSupportingFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	resp := skillResponse{
		Skill: Skill{
			OwnerSlug:         "admin",
			Slug:              "uninstall-modified",
			CurrentRevisionID: "rev_uninstall_modified",
		},
		Raw: "---\nname: Uninstall Modified\ndescription: Use when testing uninstall protection.\n---\n\n# Uninstall Modified\n",
		Files: []SkillFile{
			{Path: "SKILL.md", Content: "---\nname: Uninstall Modified\ndescription: Use when testing uninstall protection.\n---\n\n# Uninstall Modified\n"},
			{Path: "references/guide.md", Content: "# Guide\n"},
		},
	}
	manifest := Manifest{Version: 1}
	mainPath, err := installSkillResponse(&manifest, resp, "codex", confirmOptions{Yes: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := saveManifest(manifest); err != nil {
		t.Fatal(err)
	}
	guidePath := filepath.Join(filepath.Dir(mainPath), "references", "guide.md")
	if err := os.WriteFile(guidePath, []byte("# Local edit\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := runUninstall([]string{"admin/uninstall-modified", "--target", "codex", "--no"}); err == nil || !strings.Contains(err.Error(), "--no") {
		t.Fatalf("uninstall modified file err = %v, want declined by --no", err)
	}
	if got, err := os.ReadFile(guidePath); err != nil || string(got) != "# Local edit\n" {
		t.Fatalf("modified supporting file should remain, got=%q err=%v", got, err)
	}
	after, err := loadManifest()
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Installs) != 1 {
		t.Fatalf("manifest should still track install: %#v", after.Installs)
	}
}

func TestRunDoctorShowsFirstRunNextSteps(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	out, err := captureStdout(t, func() error {
		return runDoctor()
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Check",
		"Status",
		"Detail",
		"Auth path: " + filepath.Join(home, ".skillsdex", "auth.json") + " (not found)",
		"Manifest path: " + filepath.Join(home, ".skillsdex", "manifest.json") + " (not found)",
		"Config path: " + filepath.Join(home, ".skillsdex", "config.json") + " (not found)",
		"Default target: codex",
		"Auth: not configured",
		"Managed installs: 0",
		"Codex CLI",
		"Codex skills directory writable: yes",
		tokenStdinLoginCommand(),
		"skillsdex install-defaults",
		"skillsdex sync --dry-run --strict",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, out)
		}
	}

	strictOut, err := captureStdout(t, func() error {
		return runDoctor("--strict")
	})
	if err == nil || !strings.Contains(err.Error(), "doctor found") {
		t.Fatalf("strict doctor err = %v, want doctor found", err)
	}
	if !strings.Contains(strictOut, "Doctor strict check: failed") || !strings.Contains(strictOut, "auth not configured") || !strings.Contains(strictOut, "no managed installs") {
		t.Fatalf("strict doctor output missing issues:\n%s", strictOut)
	}

	jsonOut, err := captureStdout(t, func() error {
		return runDoctor("--json")
	})
	if err != nil {
		t.Fatal(err)
	}
	var report doctorReport
	if err := json.Unmarshal([]byte(jsonOut), &report); err != nil {
		t.Fatalf("doctor JSON did not decode: %v\n%s", err, jsonOut)
	}
	if report.Healthy || report.IssueCount != 2 || report.Auth.Status != "not-configured" || report.Server.Status != "not-checked" || report.ManagedInstalls.Status != "empty" || report.CodexSkillsDirectory.Status != "writable" {
		t.Fatalf("unexpected first-run doctor JSON: %#v", report)
	}
	if report.CodexCLI.Status == "" {
		t.Fatalf("doctor JSON missing codex CLI status: %#v", report.CodexCLI)
	}
	for _, want := range []string{"auth not configured", "no managed installs"} {
		if !stringSliceContains(report.Issues, want) {
			t.Fatalf("doctor JSON issues missing %q: %#v", want, report.Issues)
		}
	}
	if !stringSliceContains(report.NextSteps, "skillsdex install-defaults") {
		t.Fatalf("doctor JSON next steps missing install-defaults: %#v", report.NextSteps)
	}

	jsonStrictOut, err := captureStdout(t, func() error {
		return runDoctor("--json", "--strict")
	})
	if err == nil || !strings.Contains(err.Error(), "doctor found") {
		t.Fatalf("strict doctor JSON err = %v, want doctor found", err)
	}
	if err := json.Unmarshal([]byte(jsonStrictOut), &report); err != nil {
		t.Fatalf("strict doctor JSON did not decode: %v\n%s", err, jsonStrictOut)
	}
	if !report.Strict || report.Healthy {
		t.Fatalf("unexpected strict doctor JSON: %#v", report)
	}
}

func TestRunDoctorChecksServerAndInstallStatus(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := createSkill(ctx, db, admin, SkillInput{
		Owner:       "admin",
		Slug:        "doctor-skill",
		Name:        "Doctor Skill",
		Description: "Use when checking doctor output.",
		Body:        "# Doctor Skill\n\nInstructions.",
	}); err != nil {
		t.Fatal(err)
	}
	token, err := createAPIToken(ctx, db, admin.ID, "doctor")
	if err != nil {
		t.Fatal(err)
	}
	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	auth := AuthConfig{ServerURL: server.URL, Token: token}
	if err := saveAuth(auth); err != nil {
		t.Fatal(err)
	}
	resp, err := newAPIClient(auth).fetchSkill("admin", "doctor-skill")
	if err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{Version: 1}
	if _, err := installSkillResponse(&manifest, resp, "codex", confirmOptions{Yes: true}); err != nil {
		t.Fatal(err)
	}
	if err := saveManifest(manifest); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		return runDoctor()
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Check",
		"Status",
		"Detail",
		"Auth path: " + filepath.Join(home, ".skillsdex", "auth.json") + " (private)",
		"Manifest path: " + filepath.Join(home, ".skillsdex", "manifest.json") + " (private)",
		"Config path: " + filepath.Join(home, ".skillsdex", "config.json") + " (not found)",
		"Default target: codex",
		"Auth: configured for " + server.URL,
		"Server: reachable as admin",
		"Library:",
		"Managed installs: 1",
		"Install status: clean=1",
		"Codex CLI",
		"Codex skills directory writable: yes",
		"skillsdex installed --refresh --strict",
		"skillsdex lint --all --strict",
		"skillsdex backup restore --file ./skillsdex-backup.zip --dry-run",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "skillsdex login --server") || strings.Contains(out, "skillsdex install-defaults") {
		t.Fatalf("doctor should not suggest initial setup commands after clean install:\n%s", out)
	}

	strictOut, err := captureStdout(t, func() error {
		return runDoctor("--strict")
	})
	if err != nil {
		t.Fatalf("strict doctor should pass after clean install: %v\n%s", err, strictOut)
	}
	if !strings.Contains(strictOut, "Doctor strict check: passed") {
		t.Fatalf("strict doctor output missing pass marker:\n%s", strictOut)
	}

	jsonOut, err := captureStdout(t, func() error {
		return runDoctor("--json")
	})
	if err != nil {
		t.Fatal(err)
	}
	var report doctorReport
	if err := json.Unmarshal([]byte(jsonOut), &report); err != nil {
		t.Fatalf("doctor JSON did not decode: %v\n%s", err, jsonOut)
	}
	if !report.Healthy || report.IssueCount != 0 || report.Auth.Status != "configured" || report.Server.Status != "reachable" || report.Server.Value != "admin" {
		t.Fatalf("unexpected healthy doctor JSON: %#v", report)
	}
	if report.ManagedInstalls.Count != 1 || report.ManagedInstalls.StatusCounts["clean"] != 1 || report.CodexSkillsDirectory.Status != "writable" {
		t.Fatalf("unexpected healthy doctor JSON install/codex status: %#v", report)
	}
	if report.CodexCLI.Status == "" {
		t.Fatalf("doctor JSON missing codex CLI status: %#v", report.CodexCLI)
	}
	if !stringSliceContains(report.NextSteps, "skillsdex lint --all --strict") || !stringSliceContains(report.NextSteps, "skillsdex backup restore --file ./skillsdex-backup.zip --dry-run") {
		t.Fatalf("doctor JSON next steps missing trust gates: %#v", report.NextSteps)
	}
}

func TestRunDoctorRefreshReportsSourceUpdates(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := createSkill(ctx, db, admin, SkillInput{
		Owner:          "admin",
		Slug:           "doctor-source",
		Name:           "Doctor Source",
		Description:    "Use when testing doctor source refresh checks.",
		SourceProvider: "agentskill.sh",
		SourceURL:      "https://agentskill.sh/@openclaw/doctor-source",
		SourceExplicit: true,
		Body:           "# Doctor Source\n\nOld instructions.",
	}); err != nil {
		t.Fatal(err)
	}
	token, err := createAPIToken(ctx, db, admin.ID, "doctor-source")
	if err != nil {
		t.Fatal(err)
	}
	sourceClient := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://agentskill.sh/api/agent/skills/openclaw%2Fdoctor-source/install" {
			t.Fatalf("unexpected source request: %s", r.URL.String())
		}
		return testResponse(http.StatusOK, `{
			"slug": "openclaw/doctor-source",
			"name": "Doctor Source",
			"description": "Use when testing doctor source refresh checks.",
			"skillMd": "---\nname: Doctor Source\ndescription: Use when testing doctor source refresh checks.\n---\n\n# Doctor Source\n\nNew instructions.\n"
		}`), nil
	})}
	a := &app{db: db, httpClient: sourceClient}
	mux := http.NewServeMux()
	a.routes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()
	auth := AuthConfig{ServerURL: server.URL, Token: token}
	if err := saveAuth(auth); err != nil {
		t.Fatal(err)
	}
	resp, err := newAPIClient(auth).fetchSkill("admin", "doctor-source")
	if err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{Version: 1}
	if _, err := installSkillResponse(&manifest, resp, "codex", confirmOptions{Yes: true}); err != nil {
		t.Fatal(err)
	}
	if err := saveManifest(manifest); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		return runDoctor("--refresh", "--strict")
	})
	if err == nil || !strings.Contains(err.Error(), "doctor found") {
		t.Fatalf("doctor --refresh --strict err = %v, want doctor found\n%s", err, out)
	}
	for _, want := range []string{"Install status: clean=1", "Source refresh: update-available=1", "source updates available=1"} {
		if !strings.Contains(out, want) {
			t.Fatalf("doctor refresh output missing %q:\n%s", want, out)
		}
	}
}

func TestRunDoctorWarnsAboutOpenAuthFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	authPath := filepath.Join(home, ".skillsdex", "auth.json")
	if err := os.MkdirAll(filepath.Dir(authPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authPath, []byte(`{"server_url":"http://127.0.0.1:1","token":"test-token"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(authPath, 0644); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		return runDoctor()
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Auth path: " + authPath + " (too open: 0644; run chmod 600",
		"Auth: configured for http://127.0.0.1:1",
		"Server: unreachable or token rejected",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("doctor output missing %q:\n%s", want, out)
		}
	}

	strictOut, err := captureStdout(t, func() error {
		return runDoctor("--strict")
	})
	if err == nil || !strings.Contains(err.Error(), "doctor found") {
		t.Fatalf("strict doctor err = %v, want doctor found", err)
	}
	if !strings.Contains(strictOut, "auth file permissions are too open") || !strings.Contains(strictOut, "server unreachable or API token rejected") {
		t.Fatalf("strict doctor output missing expected issues:\n%s", strictOut)
	}
}

func TestRunDoctorReportsInvalidConfigDefaultTarget(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))
	if err := saveLocalConfig(LocalConfig{DefaultTarget: "unknown"}); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		return runDoctor("--strict")
	})
	if err == nil || !strings.Contains(err.Error(), "doctor found") {
		t.Fatalf("strict doctor err = %v, want doctor found", err)
	}
	for _, want := range []string{
		"Config path: " + filepath.Join(home, ".skillsdex", "config.json") + " (private)",
		"Default target: invalid",
		"default target invalid",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("strict doctor output missing %q:\n%s", want, out)
		}
	}
}

func TestRunDoctorFixesOpenLocalStatePermissions(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	authPath := filepath.Join(home, ".skillsdex", "auth.json")
	manifestPath := filepath.Join(home, ".skillsdex", "manifest.json")
	configPath := filepath.Join(home, ".skillsdex", "config.json")
	if err := os.MkdirAll(filepath.Dir(authPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(authPath, []byte(`{"server_url":"http://127.0.0.1:1","token":"test-token"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, []byte(`{"version":1,"installs":[]}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`{"default_target":"codex"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(authPath, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(manifestPath, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(configPath, 0644); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		return runDoctor("--fix-permissions")
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Fixed auth file permissions: " + authPath,
		"Fixed manifest file permissions: " + manifestPath,
		"Fixed config file permissions: " + configPath,
		"Auth path: " + authPath + " (private)",
		"Manifest path: " + manifestPath + " (private)",
		"Config path: " + configPath + " (private)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("doctor fix output missing %q:\n%s", want, out)
		}
	}
	for _, path := range []string{authPath, manifestPath, configPath} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("%s mode = %v, want 0600", path, info.Mode().Perm())
		}
	}

	strictOut, err := captureStdout(t, func() error {
		return runDoctor("--strict")
	})
	if err == nil || !strings.Contains(err.Error(), "doctor found") {
		t.Fatalf("strict doctor err = %v, want doctor found", err)
	}
	if strings.Contains(strictOut, "permissions are too open") {
		t.Fatalf("strict doctor still reports fixed permissions:\n%s", strictOut)
	}
	if !strings.Contains(strictOut, "server unreachable or API token rejected") {
		t.Fatalf("strict doctor missing remaining server issue:\n%s", strictOut)
	}
	if !strings.Contains(strictOut, "no managed installs") {
		t.Fatalf("strict doctor missing no-install issue:\n%s", strictOut)
	}
}

func TestCleanFirstRunCLILoop(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	token, err := createAPIToken(ctx, db, admin.ID, "first-run")
	if err != nil {
		t.Fatal(err)
	}
	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	out, err := captureStdout(t, func() error {
		return runDoctor()
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Auth: not configured",
		"Managed installs: 0",
		tokenStdinLoginCommand(),
		"skillsdex install-defaults",
		"skillsdex sync --dry-run --strict",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("first doctor output missing %q:\n%s", want, out)
		}
	}

	out, err = captureStdout(t, func() error {
		return withStdin(t, token+"\n", func() error {
			return runLogin([]string{"--server", server.URL, "--token-stdin"})
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Logged in to "+server.URL+" as admin") {
		t.Fatalf("login output missing success:\n%s", out)
	}
	if strings.Contains(out, token) {
		t.Fatalf("login output leaked token:\n%s", out)
	}
	if _, err := captureStdout(t, func() error {
		return runLogin([]string{"--server", server.URL, "--token", token, "--token-stdin"})
	}); err == nil || !strings.Contains(err.Error(), "--token and --token-stdin cannot be used together") {
		t.Fatalf("login token conflict err = %v, want conflict", err)
	}

	out, err = captureStdout(t, func() error {
		return runDoctor()
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Auth: configured for " + server.URL,
		"Server: reachable as admin",
		"Library:",
		"Managed installs: 0",
		"skillsdex install-defaults",
		"skillsdex lint --all --strict",
		"skillsdex backup export --out ./skillsdex-backup.zip",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("authenticated doctor output missing %q:\n%s", want, out)
		}
	}

	out, err = captureStdout(t, func() error {
		return runSkills(nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "admin/five-advisor-review") {
		t.Fatalf("skills output missing default skill:\n%s", out)
	}

	if _, err := captureStdout(t, func() error {
		return runInstallDefaults([]string{"--target", "codex", "--yes"})
	}); err != nil {
		t.Fatal(err)
	}
	manifest, err := loadManifest()
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Installs) != len(defaultSkills()) {
		t.Fatalf("install count = %d, want %d", len(manifest.Installs), len(defaultSkills()))
	}
	skillPath := filepath.Join(home, ".agents", "skills", "admin--five-advisor-review", "SKILL.md")
	if _, err := os.Stat(skillPath); err != nil {
		t.Fatalf("expected installed skill at %s: %v", skillPath, err)
	}

	out, err = captureStdout(t, runInstalled)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"admin/five-advisor-review", "clean"} {
		if !strings.Contains(out, want) {
			t.Fatalf("installed output missing %q:\n%s", want, out)
		}
	}

	out, err = captureStdout(t, func() error {
		return runSync([]string{"--dry-run"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "admin/five-advisor-review: clean") {
		t.Fatalf("clean sync output missing expected skill:\n%s", out)
	}
	out, err = captureStdout(t, func() error {
		return runSync([]string{"--dry-run", "--json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	var cleanSync syncDryRunReport
	if err := json.Unmarshal([]byte(out), &cleanSync); err != nil {
		t.Fatalf("clean sync JSON did not decode: %v\n%s", err, out)
	}
	if !cleanSync.Healthy || cleanSync.PendingCount != 0 || cleanSync.FailureCount != 0 || cleanSync.CleanCount == 0 || cleanSync.StatusCounts["clean"] == 0 {
		t.Fatalf("unexpected clean sync JSON: %#v", cleanSync)
	}

	skill, err := getSkill(ctx, db, admin.ID, "admin", "five-advisor-review")
	if err != nil {
		t.Fatal(err)
	}
	updated, err := updateSkill(ctx, db, admin, "admin", skill.Slug, SkillInput{
		Owner:              "admin",
		Slug:               skill.Slug,
		Name:               skill.Name,
		Description:        skill.Description,
		ShortDescription:   skill.ShortDescription,
		RecommendedDefault: skill.RecommendedDefault,
		PinnedInstall:      skill.PinnedInstall,
		Body:               "# Five Advisor Review\n\nRemote update from first-run test.\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	beforeOverwrite, err := loadManifest()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillPath, []byte("# Local edit\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := captureStdout(t, func() error {
		return runSync([]string{"--dry-run"})
	}); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "# Local edit\n" {
		t.Fatalf("dry-run changed local edit:\n%s", got)
	}
	afterDryRun, err := loadManifest()
	if err != nil {
		t.Fatal(err)
	}
	_, beforeInstall := beforeOverwrite.find("admin", "five-advisor-review", "codex")
	_, dryRunInstall := afterDryRun.find("admin", "five-advisor-review", "codex")
	if beforeInstall == nil || dryRunInstall == nil {
		t.Fatalf("missing manifest install before=%#v after=%#v", beforeInstall, dryRunInstall)
	}
	if dryRunInstall.RemoteRevision != beforeInstall.RemoteRevision {
		t.Fatalf("dry-run changed manifest revision = %q, want %q", dryRunInstall.RemoteRevision, beforeInstall.RemoteRevision)
	}

	if _, err := captureStdout(t, func() error {
		return runSync([]string{"--no"})
	}); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "# Local edit\n" {
		t.Fatalf("--no changed local edit:\n%s", got)
	}

	if _, err := captureStdout(t, func() error {
		return runSync([]string{"--yes"})
	}); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "Remote update from first-run test") {
		t.Fatalf("--yes did not apply remote update:\n%s", got)
	}
	afterOverwrite, err := loadManifest()
	if err != nil {
		t.Fatal(err)
	}
	_, overwritten := afterOverwrite.find("admin", "five-advisor-review", "codex")
	if overwritten == nil {
		t.Fatal("updated manifest install missing")
	}
	if overwritten.RemoteRevision != updated.CurrentRevisionID {
		t.Fatalf("manifest revision = %q, want %q", overwritten.RemoteRevision, updated.CurrentRevisionID)
	}
}

func TestRunAdminResetPasswordRevokesAccess(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))
	dbPath := filepath.Join(t.TempDir(), "skillsdex.db")
	db, err := openDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	session, err := createWebSession(ctx, db, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	token, err := createAPIToken(ctx, db, admin.ID, "reset-test")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		return runAdmin([]string{"reset-password", "--db", dbPath, "--username", "admin", "--password", "changed-password-123"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Reset password for admin") || strings.Contains(out, "changed-password-123") {
		t.Fatalf("unexpected reset output:\n%s", out)
	}

	db, err = openDB(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	admin, err = getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if bcrypt.CompareHashAndPassword([]byte(admin.PasswordHash), []byte("changed-password-123")) != nil {
		t.Fatal("admin password hash was not updated")
	}
	if admin.MustChangePassword {
		t.Fatal("admin should not be forced through default password setup after local reset")
	}
	if _, err := userForSession(ctx, db, session); !isNotFound(err) {
		t.Fatalf("session should be revoked, err=%v", err)
	}
	if _, err := userForAPIToken(ctx, db, token); !isNotFound(err) {
		t.Fatalf("api token should be revoked, err=%v", err)
	}
	events, err := listAuditEvents(ctx, db, 10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, event := range events {
		if event.Action == "password_reset_local" && event.TargetLabel == "admin" {
			found = true
		}
	}
	if !found {
		t.Fatalf("password reset audit event missing: %#v", events)
	}
}

func TestRunSyncDryRunDoesNotWriteUpdate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	skill, err := createSkill(ctx, db, admin, SkillInput{
		Owner:       "admin",
		Slug:        "dry-run-sync",
		Name:        "Dry Run Sync",
		Description: "Use when testing dry-run sync.",
		Body:        "# Dry Run Sync\n\nFirst.",
	})
	if err != nil {
		t.Fatal(err)
	}
	token, err := createAPIToken(ctx, db, admin.ID, "test")
	if err != nil {
		t.Fatal(err)
	}

	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	if err := saveAuth(AuthConfig{ServerURL: server.URL, Token: token}); err != nil {
		t.Fatal(err)
	}
	resp, err := newAPIClient(AuthConfig{ServerURL: server.URL, Token: token}).fetchSkill("admin", "dry-run-sync")
	if err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{Version: 1}
	path, err := installSkillResponse(&manifest, resp, "codex", confirmOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := saveManifest(manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# Dry Run Sync\n\nLocal edit."), 0600); err != nil {
		t.Fatal(err)
	}

	if _, err := updateSkill(ctx, db, admin, "admin", skill.Slug, SkillInput{
		Owner:       "admin",
		Slug:        skill.Slug,
		Name:        "Dry Run Sync",
		Description: "Use when testing dry-run sync.",
		Body:        "# Dry Run Sync\n\nSecond.",
	}); err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, func() error {
		return runSync([]string{"--dry-run"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "admin/dry-run-sync: local file modified; would require confirmation before updating to revision") || !strings.Contains(out, "  modified\tSKILL.md") || !strings.Contains(out, "skillsdex sync --yes") {
		t.Fatalf("dry-run output missing change summary:\n%s", out)
	}
	strictOut, err := captureStdout(t, func() error {
		return runSync([]string{"--dry-run", "--strict"})
	})
	if err == nil || !strings.Contains(err.Error(), "sync updates available") {
		t.Fatalf("strict dry-run sync err = %v, want updates available", err)
	}
	if !strings.Contains(strictOut, "admin/dry-run-sync: local file modified; would require confirmation before updating to revision") || !strings.Contains(strictOut, "skillsdex sync --yes") {
		t.Fatalf("strict dry-run output missing pending update:\n%s", strictOut)
	}
	jsonOut, err := captureStdout(t, func() error {
		return runSync([]string{"--dry-run", "--strict", "--json"})
	})
	if err == nil || !strings.Contains(err.Error(), "sync updates available") {
		t.Fatalf("strict dry-run sync JSON err = %v, want updates available", err)
	}
	var report syncDryRunReport
	if err := json.Unmarshal([]byte(jsonOut), &report); err != nil {
		t.Fatalf("sync dry-run JSON did not decode: %v\n%s", err, jsonOut)
	}
	if report.Healthy || !report.DryRun || !report.Strict || report.PendingCount != 1 || report.FailureCount != 0 || report.StatusCounts["local-modified"] != 1 {
		t.Fatalf("unexpected sync dry-run JSON: %#v", report)
	}
	if len(report.Results) != 1 || report.Results[0].Identifier != "admin/dry-run-sync" || report.Results[0].Status != "local-modified" || len(report.Results[0].Changes) != 1 || report.Results[0].Changes[0].Status != "modified" {
		t.Fatalf("unexpected sync dry-run JSON rows: %#v", report.Results)
	}
	if !stringSliceContains(report.NextSteps, "skillsdex sync --yes") {
		t.Fatalf("sync dry-run JSON next steps missing apply command: %#v", report.NextSteps)
	}
	if _, err := captureStdout(t, func() error {
		return runSync([]string{"--json"})
	}); err == nil || !strings.Contains(err.Error(), "--json requires --dry-run") {
		t.Fatalf("sync --json err = %v, want dry-run requirement", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "Second") {
		t.Fatalf("dry-run wrote remote update:\n%s", got)
	}
	if !strings.Contains(string(got), "Local edit") {
		t.Fatalf("dry-run changed local edit:\n%s", got)
	}
	after, err := loadManifest()
	if err != nil {
		t.Fatal(err)
	}
	if after.Installs[0].RemoteRevision != manifest.Installs[0].RemoteRevision {
		t.Fatalf("dry-run changed manifest revision = %q, want %q", after.Installs[0].RemoteRevision, manifest.Installs[0].RemoteRevision)
	}
}

func TestRunInstalledStrictChecksLocalStatus(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	if out, err := captureStdout(t, func() error {
		return runInstalledArgs([]string{"--strict"})
	}); err == nil || !strings.Contains(err.Error(), "no managed installs") {
		t.Fatalf("installed --strict with no installs err = %v, output:\n%s", err, out)
	}
	emptyJSON, err := captureStdout(t, func() error {
		return runInstalledArgs([]string{"--strict", "--json"})
	})
	if err == nil || !strings.Contains(err.Error(), "no managed installs") {
		t.Fatalf("installed --strict --json empty err = %v, want no managed installs", err)
	}
	var emptyReport installedStatusReport
	if err := json.Unmarshal([]byte(emptyJSON), &emptyReport); err != nil {
		t.Fatalf("empty installed JSON did not decode: %v\n%s", err, emptyJSON)
	}
	if emptyReport.Healthy || emptyReport.InstallCount != 0 || !emptyReport.Strict {
		t.Fatalf("unexpected empty installed JSON: %#v", emptyReport)
	}

	path := filepath.Join(home, ".agents", "skills", "admin--strict-status", "SKILL.md")
	content := []byte("# Strict Status\n")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, content, 0600); err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{Version: 1, Installs: []Install{newInstall("admin", "strict-status", "codex", path, "rev_1", content)}}
	if err := saveManifest(manifest); err != nil {
		t.Fatal(err)
	}
	cleanOut, err := captureStdout(t, func() error {
		return runInstalledArgs([]string{"--strict"})
	})
	if err != nil {
		t.Fatalf("installed --strict clean err = %v\n%s", err, cleanOut)
	}
	if !strings.Contains(cleanOut, "admin/strict-status") || !strings.Contains(cleanOut, "clean") {
		t.Fatalf("installed strict clean output missing status:\n%s", cleanOut)
	}
	cleanJSON, err := captureStdout(t, func() error {
		return runInstalledArgs([]string{"--strict", "--json"})
	})
	if err != nil {
		t.Fatalf("installed --strict --json clean err = %v\n%s", err, cleanJSON)
	}
	var cleanReport installedStatusReport
	if err := json.Unmarshal([]byte(cleanJSON), &cleanReport); err != nil {
		t.Fatalf("clean installed JSON did not decode: %v\n%s", err, cleanJSON)
	}
	if !cleanReport.Healthy || cleanReport.InstallCount != 1 || cleanReport.IssueCount != 0 || cleanReport.StatusCounts["clean"] != 1 {
		t.Fatalf("unexpected clean installed JSON: %#v", cleanReport)
	}
	if len(cleanReport.Installs) != 1 || cleanReport.Installs[0].Identifier != "admin/strict-status" || cleanReport.Installs[0].NeedsAttention {
		t.Fatalf("unexpected clean installed JSON rows: %#v", cleanReport.Installs)
	}

	if err := os.WriteFile(path, []byte("# Local edit\n"), 0600); err != nil {
		t.Fatal(err)
	}
	modifiedOut, err := captureStdout(t, func() error {
		return runInstalledArgs([]string{"--strict"})
	})
	if err == nil || !strings.Contains(err.Error(), "1 managed install(s) need attention") {
		t.Fatalf("installed --strict modified err = %v, want issue count\n%s", err, modifiedOut)
	}
	if !strings.Contains(modifiedOut, "modified") {
		t.Fatalf("installed strict modified output missing status:\n%s", modifiedOut)
	}
	modifiedJSON, err := captureStdout(t, func() error {
		return runInstalledArgs([]string{"--strict", "--json"})
	})
	if err == nil || !strings.Contains(err.Error(), "1 managed install(s) need attention") {
		t.Fatalf("installed --strict --json modified err = %v, want issue count", err)
	}
	var modifiedReport installedStatusReport
	if err := json.Unmarshal([]byte(modifiedJSON), &modifiedReport); err != nil {
		t.Fatalf("modified installed JSON did not decode: %v\n%s", err, modifiedJSON)
	}
	if modifiedReport.Healthy || modifiedReport.IssueCount != 1 || modifiedReport.StatusCounts["modified"] != 1 || !modifiedReport.Installs[0].NeedsAttention {
		t.Fatalf("unexpected modified installed JSON: %#v", modifiedReport)
	}
}

func TestRunInstalledRefreshChecksImportedSources(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	skillPath := filepath.Join(home, ".agents", "skills", "admin--imported", "SKILL.md")
	raw := []byte("# Imported\n")
	if err := os.MkdirAll(filepath.Dir(skillPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(skillPath, raw, 0600); err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{Version: 1, Installs: []Install{{
		Owner:          "admin",
		Slug:           "imported",
		Target:         "codex",
		Path:           skillPath,
		RemoteRevision: "rev_1",
		InstalledHash:  sha256String(raw),
		Files:          []InstalledFile{{Path: "SKILL.md", Hash: sha256String(raw)}},
		Managed:        true,
	}}}
	if err := saveManifest(manifest); err != nil {
		t.Fatal(err)
	}

	var sawRefresh bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/skills/admin/imported":
			_, _ = w.Write([]byte(`{"skill":{"id":"skl_1","owner":"admin","slug":"imported","name":"Imported","description":"Use when testing installed refresh.","source_url":"https://github.com/acme/skills/tree/main/imported","latest_revision":"rev_1","revision_number":1,"created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T00:00:00Z"},"raw":"# Imported\n"}`))
		case "/v1/skills/admin/imported/refresh":
			sawRefresh = true
			if r.Method != http.MethodPost || r.URL.Query().Get("dry_run") != "1" {
				t.Fatalf("unexpected refresh request %s %s", r.Method, r.URL.String())
			}
			_, _ = w.Write([]byte(`{"skill":{"id":"skl_1","owner":"admin","slug":"imported","name":"Imported","description":"Use when testing installed refresh.","source_url":"https://github.com/acme/skills/tree/main/imported","latest_revision":"rev_1","revision_number":1,"created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T00:00:00Z"},"status":"update available","dry_run":true,"changes":[{"path":"SKILL.md","status":"modified"}]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	if err := saveAuth(AuthConfig{ServerURL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		return runInstalledArgs([]string{"--refresh"})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"SOURCE", "admin/imported", "clean", "update-available"} {
		if !strings.Contains(out, want) {
			t.Fatalf("installed --refresh output missing %q:\n%s", want, out)
		}
	}
	if !sawRefresh {
		t.Fatal("expected installed --refresh to call source refresh endpoint")
	}

	strictOut, err := captureStdout(t, func() error {
		return runInstalledArgs([]string{"--refresh", "--strict"})
	})
	if err == nil || !strings.Contains(err.Error(), "1 managed install(s) need attention") {
		t.Fatalf("installed --refresh --strict err = %v, want issue count\n%s", err, strictOut)
	}
	jsonOut, err := captureStdout(t, func() error {
		return runInstalledArgs([]string{"--refresh", "--strict", "--json"})
	})
	if err == nil || !strings.Contains(err.Error(), "1 managed install(s) need attention") {
		t.Fatalf("installed --refresh --strict --json err = %v, want issue count", err)
	}
	var report installedStatusReport
	if err := json.Unmarshal([]byte(jsonOut), &report); err != nil {
		t.Fatalf("installed refresh JSON did not decode: %v\n%s", err, jsonOut)
	}
	if report.Healthy || !report.Refresh || !report.Strict || report.IssueCount != 1 || report.StatusCounts["clean"] != 1 || report.SourceStatusCounts["update-available"] != 1 {
		t.Fatalf("unexpected installed refresh JSON: %#v", report)
	}
	if len(report.Installs) != 1 || report.Installs[0].SourceStatus != "update-available" || !report.Installs[0].NeedsAttention {
		t.Fatalf("unexpected installed refresh JSON row: %#v", report.Installs)
	}
}

func TestRunInstalledRefreshRequiresAuth(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))
	if err := saveManifest(Manifest{Version: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := captureStdout(t, func() error {
		return runInstalledArgs([]string{"--refresh"})
	}); err == nil {
		t.Fatal("expected installed --refresh without auth to fail")
	}
}

func TestRunSyncUpdatesSupportingFilesAndRemovesStaleManagedFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	skill, err := createSkill(ctx, db, admin, SkillInput{
		Owner:       "admin",
		Slug:        "folder-sync",
		Name:        "Folder Sync",
		Description: "Use when testing full folder sync.",
		Body:        "# Folder Sync\n\nFirst.",
		Files: []SkillFile{
			{Path: "references/guide.md", Content: "# Guide\n\nFirst.\n"},
			{Path: "templates/prompt.txt", Content: "first\n"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	token, err := createAPIToken(ctx, db, admin.ID, "test")
	if err != nil {
		t.Fatal(err)
	}

	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	auth := AuthConfig{ServerURL: server.URL, Token: token}
	if err := saveAuth(auth); err != nil {
		t.Fatal(err)
	}
	resp, err := newAPIClient(auth).fetchSkill("admin", "folder-sync")
	if err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{Version: 1}
	mainPath, err := installSkillResponse(&manifest, resp, "codex", confirmOptions{Yes: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := saveManifest(manifest); err != nil {
		t.Fatal(err)
	}
	base := filepath.Dir(mainPath)

	if _, err := updateSkill(ctx, db, admin, "admin", skill.Slug, SkillInput{
		Owner:       "admin",
		Slug:        skill.Slug,
		Name:        "Folder Sync",
		Description: "Use when testing full folder sync.",
		Body:        "# Folder Sync\n\nSecond.",
		Files: []SkillFile{
			{Path: "references/guide.md", Content: "# Guide\n\nSecond.\n"},
		},
	}); err != nil {
		t.Fatal(err)
	}
	dryRunOut, err := captureStdout(t, func() error {
		return runSync([]string{"--dry-run"})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"admin/folder-sync: would update package to revision",
		"  modified\tSKILL.md",
		"  modified\treferences/guide.md",
		"  removed\ttemplates/prompt.txt",
		"skillsdex sync --yes",
	} {
		if !strings.Contains(dryRunOut, want) {
			t.Fatalf("folder dry-run output missing %q:\n%s", want, dryRunOut)
		}
	}
	if err := runSync([]string{"--yes"}); err != nil {
		t.Fatal(err)
	}
	guide, err := os.ReadFile(filepath.Join(base, "references", "guide.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(guide) != "# Guide\n\nSecond.\n" {
		t.Fatalf("supporting file was not updated:\n%s", guide)
	}
	if _, err := os.Stat(filepath.Join(base, "templates", "prompt.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale managed supporting file should be removed, stat err=%v", err)
	}
	after, err := loadManifest()
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Installs) != 1 || len(after.Installs[0].Files) != 2 {
		t.Fatalf("manifest package file list not refreshed: %#v", after.Installs)
	}
}

func TestRunSyncReturnsErrorAfterRemoteIssuesButKeepsProcessing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	okSkill, err := createSkill(ctx, db, admin, SkillInput{
		Owner:       "admin",
		Slug:        "sync-ok",
		Name:        "Sync OK",
		Description: "Use when testing sync partial failures.",
		Body:        "# Sync OK\n\nFirst.",
	})
	if err != nil {
		t.Fatal(err)
	}
	token, err := createAPIToken(ctx, db, admin.ID, "test")
	if err != nil {
		t.Fatal(err)
	}

	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	auth := AuthConfig{ServerURL: server.URL, Token: token}
	if err := saveAuth(auth); err != nil {
		t.Fatal(err)
	}
	resp, err := newAPIClient(auth).fetchSkill("admin", okSkill.Slug)
	if err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{Version: 1}
	if _, err := installSkillResponse(&manifest, resp, "codex", confirmOptions{Yes: true}); err != nil {
		t.Fatal(err)
	}
	manifest.Installs = append(manifest.Installs, Install{
		Owner:          "admin",
		Slug:           "missing-remote",
		Target:         "codex",
		Path:           filepath.Join(home, ".agents", "skills", "admin--missing-remote", "SKILL.md"),
		RemoteRevision: "rev_missing",
		InstalledHash:  sha256String([]byte("# Missing\n")),
		Files:          []InstalledFile{{Path: "SKILL.md", Hash: sha256String([]byte("# Missing\n"))}},
		Managed:        true,
	})
	if err := saveManifest(manifest); err != nil {
		t.Fatal(err)
	}
	updated, err := updateSkill(ctx, db, admin, "admin", okSkill.Slug, SkillInput{
		Owner:       "admin",
		Slug:        okSkill.Slug,
		Name:        "Sync OK",
		Description: "Use when testing sync partial failures.",
		Body:        "# Sync OK\n\nSecond.",
	})
	if err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		return runSync([]string{"--yes"})
	})
	if err == nil || !strings.Contains(err.Error(), "1 sync issue(s) need attention") {
		t.Fatalf("sync err = %v, want issue count; output:\n%s", err, out)
	}
	if !strings.Contains(out, "admin/sync-ok: updated to revision") {
		t.Fatalf("sync did not keep processing healthy install:\n%s", out)
	}
	after, err := loadManifest()
	if err != nil {
		t.Fatal(err)
	}
	_, okInstall := after.find("admin", okSkill.Slug, "codex")
	if okInstall == nil || okInstall.RemoteRevision != updated.CurrentRevisionID {
		t.Fatalf("healthy install was not saved after partial sync failure: %#v", okInstall)
	}
	_, missingInstall := after.find("admin", "missing-remote", "codex")
	if missingInstall == nil || missingInstall.RemoteRevision != "rev_missing" {
		t.Fatalf("missing remote install should remain unchanged: %#v", missingInstall)
	}
}

func TestRunInstallPinnedInstallsPinnedSkills(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	admin, err := getUserByUsername(context.Background(), db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	pinned, err := createSkill(context.Background(), db, admin, SkillInput{
		Owner:         "admin",
		Slug:          "pinned-skill",
		Name:          "Pinned Skill",
		Description:   "Use on every new machine.",
		PinnedInstall: true,
		Body:          "# Pinned Skill",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := createSkill(context.Background(), db, admin, SkillInput{
		Owner:       "admin",
		Slug:        "unpinned-skill",
		Name:        "Unpinned Skill",
		Description: "Use only sometimes.",
		Body:        "# Unpinned Skill",
	}); err != nil {
		t.Fatal(err)
	}
	token, err := createAPIToken(context.Background(), db, admin.ID, "test")
	if err != nil {
		t.Fatal(err)
	}

	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	if err := saveAuth(AuthConfig{ServerURL: server.URL, Token: token}); err != nil {
		t.Fatal(err)
	}
	if err := runInstallPinned([]string{"--target", "codex"}); err != nil {
		t.Fatal(err)
	}

	manifest, err := loadManifest()
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Installs) != 1 {
		t.Fatalf("install count = %d, want 1", len(manifest.Installs))
	}
	if manifest.Installs[0].Slug != pinned.Slug {
		t.Fatalf("installed slug = %q, want %q", manifest.Installs[0].Slug, pinned.Slug)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "admin--pinned-skill", "SKILL.md")); err != nil {
		t.Fatalf("expected pinned skill to be installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "admin--unpinned-skill", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("unpinned skill should not be installed, err=%v", err)
	}
}

func TestRunInstallSetInstallsOnlySetSkills(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	db, err := openDB(filepath.Join(t.TempDir(), "skillsdex.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	admin, err := getUserByUsername(ctx, db, "admin")
	if err != nil {
		t.Fatal(err)
	}
	included, err := createSkill(ctx, db, admin, SkillInput{
		Owner:       "admin",
		Slug:        "set-included",
		Name:        "Set Included",
		Description: "Use when testing install set inclusion.",
		Body:        "# Set Included\n\nInstructions.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := createSkill(ctx, db, admin, SkillInput{
		Owner:       "admin",
		Slug:        "set-excluded",
		Name:        "Set Excluded",
		Description: "Use when testing install set exclusion.",
		Body:        "# Set Excluded\n\nInstructions.",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := createInstallSet(ctx, db, admin.ID, "daily", "Daily", "Daily driver skills"); err != nil {
		t.Fatal(err)
	}
	if _, err := updateInstallSet(ctx, db, admin.ID, "daily", "Daily", "Daily driver skills", []string{included.ID}); err != nil {
		t.Fatal(err)
	}
	token, err := createAPIToken(ctx, db, admin.ID, "test")
	if err != nil {
		t.Fatal(err)
	}

	a := &app{db: db}
	mux := http.NewServeMux()
	a.routes(mux)
	server := httptest.NewServer(mux)
	defer server.Close()

	if err := saveAuth(AuthConfig{ServerURL: server.URL, Token: token}); err != nil {
		t.Fatal(err)
	}
	if err := runInstallSet([]string{"daily", "--target", "codex"}); err != nil {
		t.Fatal(err)
	}
	manifest, err := loadManifest()
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Installs) != 1 || manifest.Installs[0].Slug != included.Slug {
		t.Fatalf("unexpected installs: %#v", manifest.Installs)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "admin--set-included", "SKILL.md")); err != nil {
		t.Fatalf("expected included skill to be installed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "admin--set-excluded", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("excluded skill should not be installed, err=%v", err)
	}
}

func TestRunSkillsListsSkillsAsTextAndJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		if r.Method != http.MethodGet || r.URL.Path != "/v1/skills" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("tag") == "testing" {
			_, _ = w.Write([]byte(`{"skills":[{"id":"skl_1","owner":"admin","slug":"review","name":"Review","description":"Use when reviewing.","tags":["review","testing"],"latest_revision":"rev_1","revision_number":3,"created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T01:00:00Z"}]}`))
			return
		}
		if r.URL.RawQuery != "" {
			t.Fatalf("unexpected skills query %s", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"skills":[{"id":"skl_1","owner":"admin","slug":"review","name":"Review","description":"Use when reviewing.","tags":["review","testing"],"latest_revision":"rev_1","revision_number":3,"created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T01:00:00Z"}]}`))
	}))
	defer server.Close()
	if err := saveAuth(AuthConfig{ServerURL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}

	textOut, err := captureStdout(t, func() error {
		return runSkills(nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"IDENTIFIER", "TAGS", "admin/review", "Review", "review, testing", "3"} {
		if !strings.Contains(textOut, want) {
			t.Fatalf("skills output missing %q:\n%s", want, textOut)
		}
	}

	jsonOut, err := captureStdout(t, func() error {
		return runSkills([]string{"--json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	var resp struct {
		Skills []Skill `json:"skills"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &resp); err != nil {
		t.Fatalf("skills json did not decode: %v\n%s", err, jsonOut)
	}
	if len(resp.Skills) != 1 || resp.Skills[0].OwnerSlug != "admin" || resp.Skills[0].Slug != "review" || strings.Join(resp.Skills[0].Tags, ",") != "review,testing" {
		t.Fatalf("unexpected skills json: %#v", resp)
	}
	tagOut, err := captureStdout(t, func() error {
		return runSkills([]string{"--tag", "testing"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tagOut, "admin/review") || !strings.Contains(tagOut, "review, testing") {
		t.Fatalf("skills --tag output missing filtered skill:\n%s", tagOut)
	}
	if requests != 3 {
		t.Fatalf("requests = %d, want 3", requests)
	}
	if err := runSkills([]string{"admin/review"}); err == nil {
		t.Fatal("expected positional skills arg to fail")
	}
}

func TestRunSetsListsAndShowsInstallSets(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	var sawList, sawShow bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/install-sets":
			sawList = true
			_, _ = w.Write([]byte(`{"sets":[{"id":"set_1","slug":"daily","name":"Daily","description":"Everyday skills","skill_count":2,"created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T01:02:00Z"}]}`))
		case "/v1/install-sets/daily":
			sawShow = true
			_, _ = w.Write([]byte(`{"set":{"id":"set_1","slug":"daily","name":"Daily","description":"Everyday skills","skill_count":2,"created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T01:02:00Z"},"skills":[{"id":"skl_1","owner":"admin","slug":"review","name":"Review","description":"Use when reviewing.","latest_revision":"rev_1","revision_number":3,"created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T01:00:00Z"},{"id":"skl_2","owner":"admin","slug":"debug","name":"Debug","description":"Use when debugging.","latest_revision":"rev_2","revision_number":1,"created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T01:01:00Z"}]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	if err := saveAuth(AuthConfig{ServerURL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}

	listOut, err := captureStdout(t, func() error {
		return runSets(nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"SLUG", "daily", "Daily", "Everyday skills", "2"} {
		if !strings.Contains(listOut, want) {
			t.Fatalf("sets list output missing %q:\n%s", want, listOut)
		}
	}
	listJSON, err := captureStdout(t, func() error {
		return runSets([]string{"--json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	var listResp installSetsResponse
	if err := json.Unmarshal([]byte(listJSON), &listResp); err != nil {
		t.Fatalf("sets list json did not decode: %v\n%s", err, listJSON)
	}
	if len(listResp.Sets) != 1 || listResp.Sets[0].Slug != "daily" || listResp.Sets[0].SkillCount != 2 {
		t.Fatalf("unexpected sets json: %#v", listResp)
	}

	showOut, err := captureStdout(t, func() error {
		return runSets([]string{"daily"})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"SET", "daily", "IDENTIFIER", "admin/review", "admin/debug"} {
		if !strings.Contains(showOut, want) {
			t.Fatalf("sets show output missing %q:\n%s", want, showOut)
		}
	}
	showJSON, err := captureStdout(t, func() error {
		return runSets([]string{"--json", "daily"})
	})
	if err != nil {
		t.Fatal(err)
	}
	var showResp installSetResponse
	if err := json.Unmarshal([]byte(showJSON), &showResp); err != nil {
		t.Fatalf("sets show json did not decode: %v\n%s", err, showJSON)
	}
	if showResp.Set.Slug != "daily" || len(showResp.Skills) != 2 || showResp.Skills[0].Slug != "review" || showResp.Skills[1].Slug != "debug" {
		t.Fatalf("unexpected set show json: %#v", showResp)
	}
	if !sawList || !sawShow {
		t.Fatalf("expected list and show requests, saw list=%v show=%v", sawList, sawShow)
	}
	if err := runSets([]string{"daily", "extra"}); err == nil {
		t.Fatal("expected too many set args to fail")
	}
}

func TestRunInvitesListsCreatesAndRevokes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	inviteJSON := `{"invite":{"id":"inv_1","token_preview":"sdx_preview...abc123","created_by":"admin","created_at":"2030-06-08T00:00:00Z","expires_at":"2030-06-09T00:00:00Z"},"token":"sdx_fulltoken","url":"https://skillsdex.example/signup?invite=sdx_fulltoken"}`
	var sawList, sawRevoke bool
	createRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/invites":
			sawList = true
			_, _ = w.Write([]byte(`{"invites":[{"id":"inv_1","token_preview":"sdx_preview...abc123","created_by":"admin","created_at":"2030-06-08T00:00:00Z","expires_at":"2030-06-09T00:00:00Z"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/invites":
			createRequests++
			var in SignupInviteCreateRequest
			raw, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			if len(raw) > 0 {
				if err := json.Unmarshal(raw, &in); err != nil {
					t.Fatalf("create invite json body did not decode: %v\n%s", err, string(raw))
				}
			}
			if createRequests == 2 && in.Email != "friend@example.com" {
				t.Fatalf("second create email = %q, want friend@example.com", in.Email)
			}
			if createRequests == 2 {
				_, _ = w.Write([]byte(`{"invite":{"id":"inv_1","token_preview":"sdx_preview...abc123","created_by":"admin","created_at":"2030-06-08T00:00:00Z","expires_at":"2030-06-09T00:00:00Z"},"token":"sdx_fulltoken","url":"https://skillsdex.example/signup?invite=sdx_fulltoken","email_to":"friend@example.com","email_id":"email_1"}`))
				return
			}
			_, _ = w.Write([]byte(inviteJSON))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/invites/inv_1/revoke":
			sawRevoke = true
			_, _ = w.Write([]byte(`{"invite":{"id":"inv_1","token_preview":"sdx_preview...abc123","created_by":"admin","created_at":"2030-06-08T00:00:00Z","expires_at":"2030-06-09T00:00:00Z","revoked_at":"2030-06-08T01:00:00Z"}}`))
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()
	if err := saveAuth(AuthConfig{ServerURL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}

	listOut, err := captureStdout(t, func() error {
		return runInvites(nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ID", "inv_1", "active", "sdx_preview...abc123"} {
		if !strings.Contains(listOut, want) {
			t.Fatalf("invites list output missing %q:\n%s", want, listOut)
		}
	}
	createOut, err := captureStdout(t, func() error {
		return runInvites([]string{"create"})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Created invite inv_1", "Signup URL:", "sdx_fulltoken"} {
		if !strings.Contains(createOut, want) {
			t.Fatalf("invite create output missing %q:\n%s", want, createOut)
		}
	}
	createJSON, err := captureStdout(t, func() error {
		return runInvites([]string{"create", "--email", "friend@example.com", "--json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	var created SignupInviteCreateResult
	if err := json.Unmarshal([]byte(createJSON), &created); err != nil {
		t.Fatalf("invite create json did not decode: %v\n%s", err, createJSON)
	}
	if created.Token != "sdx_fulltoken" || created.Invite.ID != "inv_1" || created.EmailTo != "friend@example.com" || created.EmailID != "email_1" {
		t.Fatalf("unexpected invite create json: %#v", created)
	}
	revokeOut, err := captureStdout(t, func() error {
		return runInvites([]string{"revoke", "inv_1"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(revokeOut, "Revoked invite inv_1") {
		t.Fatalf("revoke output missing confirmation:\n%s", revokeOut)
	}
	if !sawList || createRequests != 2 || !sawRevoke {
		t.Fatalf("expected list/create/revoke requests, saw list=%v create=%d revoke=%v", sawList, createRequests, sawRevoke)
	}
}

func TestRunPluginsTracksListsAndDryRunsInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	pluginJSON := `{"plugin":{"id":"plg_1","slug":"codex-autoresearch","name":"Codex Autoresearch","description":"Measured loops","source_url":"https://github.com/TheGreenCedar/codex-autoresearch","target":"laptop","install_command":"codex plugin install https://github.com/TheGreenCedar/codex-autoresearch","created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T01:02:00Z"}}`
	var sawAdd, sawList, sawShow bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/plugins":
			sawAdd = true
			var in PluginInstallInput
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Fatal(err)
			}
			if in.Slug != "codex-autoresearch" || in.Target != "laptop" || !strings.Contains(in.InstallCommand, "codex plugin install") {
				t.Fatalf("unexpected plugin input: %#v", in)
			}
			_, _ = w.Write([]byte(pluginJSON))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/plugins":
			sawList = true
			if got := r.URL.Query().Get("target"); got != "laptop" {
				t.Fatalf("target query = %q, want laptop", got)
			}
			_, _ = w.Write([]byte(`{"plugins":[{"id":"plg_1","slug":"codex-autoresearch","name":"Codex Autoresearch","description":"Measured loops","source_url":"https://github.com/TheGreenCedar/codex-autoresearch","target":"laptop","install_command":"codex plugin install https://github.com/TheGreenCedar/codex-autoresearch","created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T01:02:00Z"}]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/plugins/codex-autoresearch":
			sawShow = true
			_, _ = w.Write([]byte(pluginJSON))
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()
	if err := saveAuth(AuthConfig{ServerURL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}

	addOut, err := captureStdout(t, func() error {
		return runPlugin([]string{"add", "codex-autoresearch", "--name", "Codex Autoresearch", "--url", "https://github.com/TheGreenCedar/codex-autoresearch", "--target", "laptop", "--command", "codex plugin install https://github.com/TheGreenCedar/codex-autoresearch"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(addOut, "Tracked plugin codex-autoresearch") {
		t.Fatalf("add output missing tracked message:\n%s", addOut)
	}

	listOut, err := captureStdout(t, func() error {
		return runPlugins([]string{"--target", "laptop"})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"SLUG", "codex-autoresearch", "laptop", "codex plugin install"} {
		if !strings.Contains(listOut, want) {
			t.Fatalf("plugins list output missing %q:\n%s", want, listOut)
		}
	}

	showOut, err := captureStdout(t, func() error {
		return runPlugins([]string{"codex-autoresearch"})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Plugin: codex-autoresearch", "Source:", "Install command:"} {
		if !strings.Contains(showOut, want) {
			t.Fatalf("plugin show output missing %q:\n%s", want, showOut)
		}
	}

	dryRunOut, err := captureStdout(t, func() error {
		return runPlugin([]string{"install", "codex-autoresearch", "--dry-run"})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Plugin: codex-autoresearch", "Command: codex plugin install", "Mode: dry run"} {
		if !strings.Contains(dryRunOut, want) {
			t.Fatalf("plugin dry-run output missing %q:\n%s", want, dryRunOut)
		}
	}
	if !sawAdd || !sawList || !sawShow {
		t.Fatalf("expected add/list/show requests, saw add=%v list=%v show=%v", sawAdd, sawList, sawShow)
	}
}

func TestRunShowPrintsPackageFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	const skillMarkdown = "---\nname: Folder Show\ndescription: Use when showing package files.\n---\n\n# Folder Show\n\nInstructions.\n"
	const guideMarkdown = "# Guide\n\nUse this reference.\n"

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		if r.Method != http.MethodGet || r.URL.Path != "/v1/skills/admin/folder-show" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(skillResponse{
			Skill: Skill{
				OwnerSlug: "admin",
				Slug:      "folder-show",
				Name:      "Folder Show",
			},
			Raw: skillMarkdown,
			Files: []SkillFile{
				{Path: "SKILL.md", Content: skillMarkdown, ContentType: "text/markdown; charset=utf-8", SizeBytes: int64(len(skillMarkdown))},
				{Path: "references/guide.md", Content: guideMarkdown, ContentType: "text/markdown; charset=utf-8", SizeBytes: int64(len(guideMarkdown))},
			},
		}); err != nil {
			t.Fatal(err)
		}
	}))
	defer server.Close()
	if err := saveAuth(AuthConfig{ServerURL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		return runShow([]string{"admin/folder-show", "--file", "references/guide.md"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != guideMarkdown {
		t.Fatalf("show --file output = %q, want %q", out, guideMarkdown)
	}

	out, err = captureStdout(t, func() error {
		return runShow([]string{"admin/folder-show", "--file=SKILL.md"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if out != skillMarkdown {
		t.Fatalf("show --file=SKILL.md output = %q, want %q", out, skillMarkdown)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
}

func TestParseShowArgsRejectsUnsafeFilePath(t *testing.T) {
	if _, err := parseShowArgs([]string{"admin/folder-show", "--file", "../secret"}); err == nil || !strings.Contains(err.Error(), "must not contain traversal") {
		t.Fatalf("parseShowArgs traversal err = %v, want traversal rejection", err)
	}
	if _, err := parseShowArgs([]string{"admin/folder-show", "--raw", "--file", "SKILL.md"}); err == nil || !strings.Contains(err.Error(), "cannot be used together") {
		t.Fatalf("parseShowArgs raw/file err = %v, want conflict rejection", err)
	}
}

func TestRunMarkPreviewsAndAppliesSkillFlags(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	current := Skill{
		OwnerSlug:          "admin",
		Slug:               "review",
		Name:               "Review",
		Description:        "Use when reviewing code.",
		RecommendedDefault: true,
		PinnedInstall:      false,
	}
	var puts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/skills/admin/review":
			if err := json.NewEncoder(w).Encode(skillResponse{Skill: current, Raw: "# Review\n"}); err != nil {
				t.Fatal(err)
			}
		case r.Method == http.MethodPut && r.URL.Path == "/v1/skills/admin/review/flags":
			puts++
			var flags SkillFlagUpdate
			if err := json.NewDecoder(r.Body).Decode(&flags); err != nil {
				t.Fatal(err)
			}
			if flags.PinnedInstall == nil || !*flags.PinnedInstall {
				t.Fatalf("pinned flag = %#v, want true", flags.PinnedInstall)
			}
			if flags.RecommendedDefault == nil || *flags.RecommendedDefault {
				t.Fatalf("recommended flag = %#v, want false", flags.RecommendedDefault)
			}
			current.PinnedInstall = true
			current.RecommendedDefault = false
			if err := json.NewEncoder(w).Encode(map[string]Skill{"skill": current}); err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	if err := saveAuth(AuthConfig{ServerURL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}

	previewOut, err := captureStdout(t, func() error {
		return runMark([]string{"admin/review", "--pinned", "--not-recommended"})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Would update admin/review install flags", "Pinned install: no -> yes", "Recommended default: yes -> no", "skillsdex mark admin/review --pinned --not-recommended --apply --yes"} {
		if !strings.Contains(previewOut, want) {
			t.Fatalf("mark preview output missing %q:\n%s", want, previewOut)
		}
	}
	if puts != 0 {
		t.Fatalf("preview PUT count = %d, want 0", puts)
	}

	applyOut, err := captureStdout(t, func() error {
		return runMark([]string{"admin/review", "--pinned", "--not-recommended", "--apply", "--yes"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(applyOut, "Updated admin/review: pinned=yes, recommended=no.") {
		t.Fatalf("mark apply output unexpected:\n%s", applyOut)
	}
	if puts != 1 {
		t.Fatalf("apply PUT count = %d, want 1", puts)
	}
}

func TestParseMarkArgsRejectsConflicts(t *testing.T) {
	for _, args := range [][]string{
		{"admin/review"},
		{"admin/review", "--pinned", "--unpinned"},
		{"admin/review", "--recommended", "--not-recommended"},
		{"admin/review", "--apply", "--dry-run"},
		{"admin/review", "--pinned", "--yes", "--no"},
	} {
		if _, err := parseMarkArgs(args); err == nil {
			t.Fatalf("parseMarkArgs(%v) succeeded, want error", args)
		}
	}
}

func TestAPIClientFormatsJSONErrorMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/skills/admin/missing" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		writeJSONError(w, http.StatusBadRequest, "skill has no source metadata")
	}))
	defer server.Close()
	client := newAPIClient(AuthConfig{ServerURL: server.URL, Token: "test-token"})

	err := client.getJSON("/v1/skills/admin/missing", &skillResponse{})
	if err == nil {
		t.Fatal("expected API error")
	}
	got := err.Error()
	if !strings.Contains(got, "GET /v1/skills/admin/missing: 400 Bad Request: skill has no source metadata") {
		t.Fatalf("formatted JSON error = %q", got)
	}
	if strings.Contains(got, `{"error"`) {
		t.Fatalf("formatted JSON error leaked raw payload: %q", got)
	}
}

func TestAPIClientFormatsTextAndEmptyErrorMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/text":
			http.Error(w, "plain failure", http.StatusBadGateway)
		case "/empty":
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := newAPIClient(AuthConfig{ServerURL: server.URL, Token: "test-token"})

	if err := client.postJSON("/text", map[string]string{}, nil); err == nil || !strings.Contains(err.Error(), "POST /text: 502 Bad Gateway: plain failure") {
		t.Fatalf("text API error = %v, want plain failure", err)
	}
	if _, err := client.getBytes("/empty"); err == nil || err.Error() != "GET /empty: 503 Service Unavailable" {
		t.Fatalf("empty API error = %v, want concise status", err)
	}
}

func TestAPIClientUsesBoundedContextRequests(t *testing.T) {
	client := newAPIClient(AuthConfig{ServerURL: "http://127.0.0.1:8765/", Token: "test-token"})
	if client.http.Timeout != cliRequestTimeout {
		t.Fatalf("client timeout = %s, want %s", client.http.Timeout, cliRequestTimeout)
	}

	before := time.Now()
	req, cancel, err := client.newRequest(http.MethodGet, "/v1/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	deadline, ok := req.Context().Deadline()
	if !ok {
		t.Fatal("request context has no deadline")
	}
	if deadline.Before(before) || deadline.After(before.Add(cliRequestTimeout+time.Second)) {
		t.Fatalf("request deadline = %s, want within CLI timeout", deadline)
	}
	if req.Header.Get("Authorization") != "Bearer test-token" {
		t.Fatalf("authorization header = %q", req.Header.Get("Authorization"))
	}
	if req.URL.String() != "http://127.0.0.1:8765/v1/me" {
		t.Fatalf("request URL = %q", req.URL.String())
	}
}

func TestRunSetupPreviewsRecommendedDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/me":
			_, _ = w.Write([]byte(`{"username":"admin"}`))
		case "/v1/install-sets":
			_, _ = w.Write([]byte(`{"sets":[]}`))
		case "/v1/skills":
			_, _ = w.Write([]byte(`{"skills":[{"id":"skl_1","owner":"admin","slug":"review","name":"Review","description":"Use when reviewing a change for concrete risks.","short_description":"Review changes","recommended_default":true,"latest_revision":"rev_1","revision_number":1,"created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T00:00:00Z"}]}`))
		case "/v1/skills/admin/review":
			_, _ = w.Write([]byte(`{"skill":{"id":"skl_1","owner":"admin","slug":"review","name":"Review","description":"Use when reviewing a change for concrete risks.","short_description":"Review changes","recommended_default":true,"latest_revision":"rev_1","revision_number":1,"created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T00:00:00Z"},"raw":"---\nname: Review\ndescription: Use when reviewing a change for concrete risks.\n---\n\n# Review\n\nCheck the changed behavior, tests, and risk areas before reporting confirmed findings."}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	if err := saveAuth(AuthConfig{ServerURL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		return runSetup(nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"reachable as admin", "preview only", "Selected recommended defaults", "Would install admin/review", "skillsdex setup --target codex --defaults --apply --yes", "Quality gate:", "skillsdex lint --all --strict", "Backup confidence:", "skillsdex backup restore --file ./skillsdex-backup.zip --dry-run"} {
		if !strings.Contains(out, want) {
			t.Fatalf("setup preview output missing %q:\n%s", want, out)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "admin--review", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("dry-run should not write skill, err=%v", err)
	}
	manifest, err := loadManifest()
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Installs) != 0 {
		t.Fatalf("dry-run changed manifest: %#v", manifest.Installs)
	}

	jsonOut, err := captureStdout(t, func() error {
		return runSetup([]string{"--json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	var report setupPlanReport
	if err := json.Unmarshal([]byte(jsonOut), &report); err != nil {
		t.Fatalf("setup JSON did not decode: %v\n%s", err, jsonOut)
	}
	if report.ServerURL != server.URL || report.Username != "admin" || report.Target != "codex" || report.Mode != "defaults" || !report.PreviewOnly || report.Apply {
		t.Fatalf("unexpected setup JSON report: %#v", report)
	}
	if report.ApplyCommand != "skillsdex setup --target codex --defaults --apply --yes" {
		t.Fatalf("setup JSON apply command = %q", report.ApplyCommand)
	}
	for _, want := range []string{"skillsdex sync --dry-run --strict", "skillsdex lint --all --strict", "skillsdex backup restore --file ./skillsdex-backup.zip --dry-run"} {
		if !stringSliceContains(report.NextSteps, want) {
			t.Fatalf("setup JSON next steps missing %q: %#v", want, report.NextSteps)
		}
	}
	if _, err := captureStdout(t, func() error {
		return runSetup([]string{"--json", "--apply"})
	}); err == nil || !strings.Contains(err.Error(), "--json is preview-only") {
		t.Fatalf("setup --json --apply err = %v, want preview-only requirement", err)
	}
}

func TestRunSetupChoosesSingleInstallSetBeforeDefaults(t *testing.T) {
	ta := newTestApp(t)
	ta.authenticateCLI(t)
	ctx := context.Background()
	skill := ta.createSkill(t, SkillInput{
		Owner:              "admin",
		Slug:               "workflow",
		Name:               "Workflow",
		Description:        "Use when planning a multi-step workflow.",
		ShortDescription:   "Plan workflows",
		Body:               "# Workflow\n\nPlan the next testable slice before editing.",
		RecommendedDefault: true,
	})
	if _, err := createInstallSet(ctx, ta.DB, ta.Admin.ID, "laptop", "Laptop", "Daily machine"); err != nil {
		t.Fatal(err)
	}
	if _, err := updateInstallSet(ctx, ta.DB, ta.Admin.ID, "laptop", "Laptop", "Daily machine", []string{skill.ID}); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		return runSetup(nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"reachable as admin", "Selected install set: laptop", "Would install admin/workflow", "skillsdex setup --target codex --set laptop --apply --yes"} {
		if !strings.Contains(out, want) {
			t.Fatalf("setup auto output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Selected recommended defaults") {
		t.Fatalf("setup should prefer the single install set over defaults:\n%s", out)
	}
	if _, err := os.Stat(ta.installedSkillPath("admin", "workflow")); !os.IsNotExist(err) {
		t.Fatalf("preview setup wrote skill, err=%v", err)
	}
}

func TestRunSetupAppliesExplicitSetAndPreviewsSync(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/me":
			_, _ = w.Write([]byte(`{"username":"admin"}`))
		case "/v1/install-sets/laptop":
			_, _ = w.Write([]byte(`{"set":{"id":"set_1","slug":"laptop","name":"Laptop","description":"Daily machine","skill_count":1,"created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T00:00:00Z"},"skills":[{"id":"skl_1","owner":"admin","slug":"review","name":"Review","description":"Use when reviewing a change for concrete risks.","short_description":"Review changes","latest_revision":"rev_1","revision_number":1,"created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T00:00:00Z"}]}`))
		case "/v1/skills/admin/review":
			_, _ = w.Write([]byte(`{"skill":{"id":"skl_1","owner":"admin","slug":"review","name":"Review","description":"Use when reviewing a change for concrete risks.","short_description":"Review changes","latest_revision":"rev_1","revision_number":1,"created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T00:00:00Z"},"raw":"---\nname: Review\ndescription: Use when reviewing a change for concrete risks.\n---\n\n# Review\n\nCheck the changed behavior, tests, and risk areas before reporting confirmed findings."}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	if err := saveAuth(AuthConfig{ServerURL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		return runSetup([]string{"--set", "laptop", "--apply", "--yes"})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Selected install set: laptop", "Installed 1 skill(s) from set laptop.", "Sync preview:", "admin/review: clean", "Quality gate:", "skillsdex lint --all --strict", "Backup confidence:", "skillsdex backup export --out ./skillsdex-backup.zip"} {
		if !strings.Contains(out, want) {
			t.Fatalf("setup apply output missing %q:\n%s", want, out)
		}
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "admin--review", "SKILL.md")); err != nil {
		t.Fatalf("expected skill to be installed: %v", err)
	}
}

func TestRunPublicSearchAndTopUseAuthenticatedAPI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	var sawSearch, sawTop bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/v1/public/search":
			sawSearch = true
			if r.URL.Query().Get("q") != "code review" || r.URL.Query().Get("limit") != "7" {
				t.Fatalf("unexpected search query: %s", r.URL.RawQuery)
			}
		case "/v1/public/top":
			sawTop = true
			if r.URL.Query().Get("limit") != "3" {
				t.Fatalf("unexpected top query: %s", r.URL.RawQuery)
			}
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"owner":"openclaw","slug":"review","name":"Review","description":"Use when reviewing.","githubStars":12,"contentQualityScore":91,"securityScore":88}],"total":1}`))
	}))
	defer server.Close()
	if err := saveAuth(AuthConfig{ServerURL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}

	searchOut, err := captureStdout(t, func() error {
		return runPublicSearch([]string{"code", "review", "--limit", "7"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sawSearch || !strings.Contains(searchOut, "@openclaw/review") || !strings.Contains(searchOut, "91") {
		t.Fatalf("unexpected search output saw=%v:\n%s", sawSearch, searchOut)
	}

	topOut, err := captureStdout(t, func() error {
		return runPublicTop([]string{"--limit=3"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sawTop || !strings.Contains(topOut, "@openclaw/review") || !strings.Contains(topOut, "Review") {
		t.Fatalf("unexpected top output saw=%v:\n%s", sawTop, topOut)
	}
}

func TestRunImportSourceUsesAuthenticatedAPI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/import" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(body), `@openclaw/review`) {
			t.Fatalf("unexpected import body: %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"Identifier":"admin/review","Name":"Review","Status":"created"}]}`))
	}))
	defer server.Close()
	if err := saveAuth(AuthConfig{ServerURL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		return runImportSource([]string{"@openclaw/review"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "admin/review") || !strings.Contains(out, "created") {
		t.Fatalf("unexpected import-source output:\n%s", out)
	}
}

func TestRunImportSourceCanPreviewInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	var sawImport, sawFetch bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/import":
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected import method %s", r.Method)
			}
			sawImport = true
			_, _ = w.Write([]byte(`{"results":[{"Identifier":"admin/review","Name":"Review","Status":"created"},{"Identifier":"admin/bad","Name":"Bad","Status":"source did not parse"}]}`))
		case "/v1/skills/admin/review":
			if r.Method != http.MethodGet {
				t.Fatalf("unexpected fetch method %s", r.Method)
			}
			sawFetch = true
			_, _ = w.Write([]byte(`{"skill":{"id":"skl_1","owner":"admin","slug":"review","name":"Review","description":"Use when reviewing imported skills.","short_description":"Review imported skills","latest_revision":"rev_1","revision_number":1,"created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T00:00:00Z"},"raw":"---\nname: Review\ndescription: Use when reviewing imported skills.\n---\n\n# Review\n\nInspect imported skill behavior before installing."}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	if err := saveAuth(AuthConfig{ServerURL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		return runImportSource([]string{"@openclaw/review", "--install", "--dry-run"})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"admin/review", "created", "admin/bad", "source did not parse", "Would install admin/review", "Would install 1 imported skill(s)."} {
		if !strings.Contains(out, want) {
			t.Fatalf("import install preview output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "admin/bad to") {
		t.Fatalf("failed import row should not be installed:\n%s", out)
	}
	if !sawImport || !sawFetch {
		t.Fatalf("expected import and fetch requests, saw import=%v fetch=%v", sawImport, sawFetch)
	}
	manifest, err := loadManifest()
	if err != nil {
		t.Fatal(err)
	}
	if len(manifest.Installs) != 0 {
		t.Fatalf("dry-run install changed manifest: %#v", manifest.Installs)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "admin--review", "SKILL.md")); !os.IsNotExist(err) {
		t.Fatalf("dry-run install wrote skill, err=%v", err)
	}
}

func TestRunImportSourceGitHubFolderInstallDryRunPreservesSupportingFiles(t *testing.T) {
	github := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		jsonResponse := func(body string) *http.Response {
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(body)),
			}
		}
		rawResponse := func(body string) *http.Response {
			return &http.Response{
				StatusCode: http.StatusOK,
				Status:     "200 OK",
				Header:     http.Header{"Content-Type": []string{"text/plain; charset=utf-8"}},
				Body:       io.NopCloser(strings.NewReader(body)),
			}
		}
		switch r.URL.String() {
		case "https://api.github.com/repos/acme/skills/contents/workflow/SKILL.md?ref=main":
			return jsonResponse(`{"name":"SKILL.md","path":"workflow/SKILL.md","type":"file","download_url":"https://raw.githubusercontent.com/acme/skills/main/workflow/SKILL.md","html_url":"https://github.com/acme/skills/blob/main/workflow/SKILL.md"}`), nil
		case "https://raw.githubusercontent.com/acme/skills/main/workflow/SKILL.md":
			return rawResponse("---\nname: workflow\ndescription: Use when planning testable workflow changes.\nmetadata:\n  short-description: Plan workflows\n---\n\n# Workflow\n\nPlan the next testable slice and keep references beside the skill.\n"), nil
		case "https://api.github.com/repos/acme/skills/contents/workflow?ref=main":
			return jsonResponse(`[{"name":"SKILL.md","path":"workflow/SKILL.md","type":"file","download_url":"https://raw.githubusercontent.com/acme/skills/main/workflow/SKILL.md"},{"name":"references","path":"workflow/references","type":"dir"}]`), nil
		case "https://api.github.com/repos/acme/skills/contents/workflow/references?ref=main":
			return jsonResponse(`[{"name":"guide.md","path":"workflow/references/guide.md","type":"file","download_url":"https://raw.githubusercontent.com/acme/skills/main/workflow/references/guide.md"}]`), nil
		case "https://raw.githubusercontent.com/acme/skills/main/workflow/references/guide.md":
			return rawResponse("# Guide\n\nKeep the workflow reference with the skill.\n"), nil
		default:
			t.Fatalf("unexpected GitHub request %s", r.URL.String())
			return nil, nil
		}
	})}
	ta := newTestApp(t, withSourceClient(github))
	ta.authenticateCLI(t)

	out, err := captureStdout(t, func() error {
		return runImportSource([]string{"https://github.com/acme/skills/tree/main/workflow", "--install", "--dry-run"})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"admin/workflow", "created", "Would install admin/workflow", "references/guide.md", "Would install 1 imported skill(s)."} {
		if !strings.Contains(out, want) {
			t.Fatalf("GitHub folder import output missing %q:\n%s", want, out)
		}
	}
	if _, err := os.Stat(ta.installedSkillPath("admin", "workflow")); !os.IsNotExist(err) {
		t.Fatalf("dry-run import install wrote skill, err=%v", err)
	}
	skill, err := getSkill(context.Background(), ta.DB, ta.Admin.ID, "admin", "workflow")
	if err != nil {
		t.Fatal(err)
	}
	if skill.SourceProvider != "github" || skill.SourceURL != "https://github.com/acme/skills/blob/main/workflow/SKILL.md" || skill.SourceAuthor != "acme" || skill.SourceName != "skills" || skill.SourcePath != "workflow/SKILL.md" || skill.SourceRef != "main" {
		t.Fatalf("source metadata = %#v", skill)
	}
	files, _, err := currentSkillFiles(context.Background(), ta.DB, ta.Admin.ID, "admin", "workflow")
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, file := range files {
		paths = append(paths, file.Path)
	}
	if !reflect.DeepEqual(paths, []string{"SKILL.md", "references/guide.md"}) {
		t.Fatalf("imported package files = %#v", paths)
	}
}

func TestRunImportSourceInstallFlagsRequireInstall(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	if err := runImportSource([]string{"@openclaw/review", "--dry-run"}); err == nil || !strings.Contains(err.Error(), "usage: skillsdex import-source") {
		t.Fatalf("import-source --dry-run err = %v, want usage", err)
	}
}

func TestRunPublishPreviewsLocalSkillFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	skillFile := filepath.Join(home, "SKILL.md")
	if err := os.WriteFile(skillFile, []byte("---\nname: Review\ndescription: Use when reviewing a local skill before publishing.\nmetadata:\n  short-description: Review local skills\n---\n\n# Review\n\nInspect the local skill instructions, supporting files, trigger wording, expected output, install behavior, quality warnings, and any sensitive material before publishing it into SkillsDex."), 0600); err != nil {
		t.Fatal(err)
	}
	var sawPost bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/me":
			_, _ = w.Write([]byte(`{"username":"admin"}`))
		case "/v1/skills/admin/review":
			writeJSONError(w, http.StatusNotFound, "not found")
		case "/v1/skills":
			sawPost = true
			t.Fatalf("dry-run publish should not POST")
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	if err := saveAuth(AuthConfig{ServerURL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		return runPublish([]string{skillFile})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"admin/review: ready", "Would create admin/review", "Package files:", "  SKILL.md (", "skillsdex publish"} {
		if !strings.Contains(out, want) {
			t.Fatalf("publish preview output missing %q:\n%s", want, out)
		}
	}
	if sawPost {
		t.Fatal("dry-run posted unexpectedly")
	}
}

func TestRunPublishAppliesLocalSkillFolderUpdate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	dir := filepath.Join(home, "review")
	if err := os.MkdirAll(filepath.Join(dir, "references"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: Review\ndescription: Use when reviewing a local skill before publishing.\nmetadata:\n  short-description: Review local skills\n---\n\n# Review\n\nInspect the local skill instructions, supporting files, trigger wording, expected output, install behavior, quality warnings, and any sensitive material before publishing it into SkillsDex."), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "references", "guide.md"), []byte("# Guide\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("API_TOKEN=secret\n"), 0600); err != nil {
		t.Fatal(err)
	}

	var sawPut bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/me":
			_, _ = w.Write([]byte(`{"username":"admin"}`))
		case "/v1/skills/admin/review":
			if r.Method == http.MethodGet {
				_, _ = w.Write([]byte(`{"skill":{"id":"skl_1","owner":"admin","slug":"review","name":"Review","description":"Use when reviewing.","latest_revision":"rev_1","revision_number":1,"created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T00:00:00Z"},"raw":"# Old"}`))
				return
			}
			if r.Method != http.MethodPut {
				t.Fatalf("unexpected method %s", r.Method)
			}
			sawPut = true
			var in SkillInput
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Fatal(err)
			}
			if in.Slug != "review" || len(in.Files) != 1 || in.Files[0].Path != "references/guide.md" || !strings.Contains(in.Files[0].Content, "# Guide") {
				t.Fatalf("unexpected publish input: %#v", in)
			}
			_, _ = w.Write([]byte(`{"skill":{"id":"skl_1","owner":"admin","slug":"review","name":"Review","description":"Use when reviewing a local skill before publishing.","short_description":"Review local skills","latest_revision":"rev_2","revision_number":2,"created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T01:00:00Z"}}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	if err := saveAuth(AuthConfig{ServerURL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		return runPublish([]string{dir, "--apply", "--yes"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sawPut || !strings.Contains(out, "Updated admin/review revision 2") || !strings.Contains(out, "2 package file(s)") {
		t.Fatalf("unexpected publish apply output sawPut=%v:\n%s", sawPut, out)
	}
	if !strings.Contains(out, "Package files:") || !strings.Contains(out, "  references/guide.md (8 bytes)") {
		t.Fatalf("publish apply output missing package file summary:\n%s", out)
	}
	if !strings.Contains(out, "Skipped 1 local package path(s):") || !strings.Contains(out, "  .env") {
		t.Fatalf("publish apply output missing skipped path:\n%s", out)
	}
}

func TestSkillInputFromLocalDirSkipsLocalMetadata(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "references"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, subdir := range []string{".git", ".vscode", "__pycache__", "node_modules/pkg"} {
		if err := os.MkdirAll(filepath.Join(dir, subdir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: Review\ndescription: Use when reviewing a local skill before publishing.\nmetadata:\n  short-description: Review local skills\n---\n\n# Review\n\nInspect the local skill instructions, supporting files, trigger wording, expected output, install behavior, quality warnings, and any sensitive material before publishing it into SkillsDex."), 0600); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"references/guide.md":       "# Guide\n",
		".env.example":              "API_TOKEN=\n",
		".env":                      "API_TOKEN=secret\n",
		".env.local":                "API_TOKEN=secret\n",
		".envrc":                    "export API_TOKEN=secret\n",
		".DS_Store":                 "metadata",
		"notes.swp":                 "swap",
		".git/config":               "[remote]\n",
		".vscode/settings.json":     "{}",
		"__pycache__/helpers.pyc":   "cache",
		"node_modules/pkg/index.js": "module.exports = {}\n",
	}
	for rel, content := range files {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(rel)), []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}

	pkg, err := skillInputFromLocalDir(dir, "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, file := range pkg.Input.Files {
		got = append(got, file.Path)
	}
	want := []string{".env.example", "references/guide.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("published package files = %#v, want %#v", got, want)
	}
	if pkg.Input.Files[1].SizeBytes != int64(len("# Guide\n")) {
		t.Fatalf("supporting file size = %d, want %d", pkg.Input.Files[1].SizeBytes, len("# Guide\n"))
	}
	wantSkipped := []string{".DS_Store", ".env", ".env.local", ".envrc", ".git/", ".vscode/", "__pycache__/", "node_modules/", "notes.swp"}
	if !reflect.DeepEqual(pkg.Skipped, wantSkipped) {
		t.Fatalf("skipped package paths = %#v, want %#v", pkg.Skipped, wantSkipped)
	}
}

func TestRunPublishStrictBlocksWeakSkill(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	skillFile := filepath.Join(home, "SKILL.md")
	if err := os.WriteFile(skillFile, []byte("---\nname: Weak\ndescription: Do things.\n---\n\nTODO\n"), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/me" {
			t.Fatalf("strict publish should only fetch /v1/me, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"username":"admin"}`))
	}))
	defer server.Close()
	if err := saveAuth(AuthConfig{ServerURL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		return runPublish([]string{skillFile, "--strict"})
	})
	if err == nil || !strings.Contains(err.Error(), "quality warnings found") {
		t.Fatalf("strict publish err = %v, want warnings found", err)
	}
	if !strings.Contains(out, "needs attention") {
		t.Fatalf("strict publish output missing lint report:\n%s", out)
	}
}

func TestRunRefreshPreviewsAndAppliesSourceRefresh(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		if r.URL.Path != "/v1/skills/admin/review/refresh" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("dry_run") == "1" {
			_, _ = w.Write([]byte(`{"skill":{"id":"skl_1","owner":"admin","slug":"review","name":"Review","description":"Use when reviewing.","source_url":"https://github.com/acme/skills/tree/main/review","latest_revision":"rev_1","revision_number":1,"created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T00:00:00Z"},"status":"update available","dry_run":true,"changes":[{"path":"SKILL.md","status":"modified","diff":"@@\n- Old instructions.\n+ New instructions.\n"},{"path":"references/new.md","status":"added","diff":"@@\n+ # New reference\n"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"skill":{"id":"skl_1","owner":"admin","slug":"review","name":"Review","description":"Use when reviewing.","source_url":"https://github.com/acme/skills/tree/main/review","latest_revision":"rev_2","revision_number":2,"created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T01:00:00Z"},"status":"updated from source","dry_run":false}`))
	}))
	defer server.Close()
	if err := saveAuth(AuthConfig{ServerURL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}

	previewOut, err := captureStdout(t, func() error {
		return runRefresh([]string{"admin/review"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(previewOut, "Previewing source refresh") || !strings.Contains(previewOut, "admin/review: update available") {
		t.Fatalf("unexpected preview output:\n%s", previewOut)
	}
	if !strings.Contains(previewOut, "modified\tSKILL.md") || !strings.Contains(previewOut, "added\treferences/new.md") {
		t.Fatalf("preview output missing change summary:\n%s", previewOut)
	}
	if strings.Contains(previewOut, "New instructions") {
		t.Fatalf("preview without --diff should not print diff:\n%s", previewOut)
	}
	if !strings.Contains(previewOut, "skillsdex refresh admin/review --apply --yes") || !strings.Contains(previewOut, "skillsdex sync --dry-run --strict") {
		t.Fatalf("preview output missing next steps:\n%s", previewOut)
	}
	diffOut, err := captureStdout(t, func() error {
		return runRefresh([]string{"admin/review", "--diff"})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"modified\tSKILL.md", "    - Old instructions.", "    + New instructions.", "    + # New reference"} {
		if !strings.Contains(diffOut, want) {
			t.Fatalf("diff preview output missing %q:\n%s", want, diffOut)
		}
	}
	strictOut, err := captureStdout(t, func() error {
		return runRefresh([]string{"admin/review", "--strict"})
	})
	if err == nil || !strings.Contains(err.Error(), "source update available") {
		t.Fatalf("strict refresh err = %v, want source update available", err)
	}
	if !strings.Contains(strictOut, "admin/review: update available") {
		t.Fatalf("strict refresh output missing update status:\n%s", strictOut)
	}
	jsonOut, err := captureStdout(t, func() error {
		return runRefresh([]string{"admin/review", "--strict", "--json"})
	})
	if err == nil || !strings.Contains(err.Error(), "source update available") {
		t.Fatalf("strict refresh JSON err = %v, want source update available", err)
	}
	var report refreshReport
	if err := json.Unmarshal([]byte(jsonOut), &report); err != nil {
		t.Fatalf("refresh JSON did not decode: %v\n%s", err, jsonOut)
	}
	if report.Healthy || !report.DryRun || !report.Strict || report.All || report.Target != "admin/review" || report.UpdateCount != 1 || report.StatusCounts["update available"] != 1 {
		t.Fatalf("unexpected refresh JSON report: %#v", report)
	}
	if len(report.Results) != 1 || report.Results[0].Identifier != "admin/review" || len(report.Results[0].Changes) != 2 {
		t.Fatalf("unexpected refresh JSON results: %#v", report.Results)
	}
	if !stringSliceContains(report.NextSteps, "skillsdex refresh admin/review --apply --yes") || !stringSliceContains(report.NextSteps, "skillsdex sync --dry-run --strict") {
		t.Fatalf("refresh JSON next steps missing apply/sync commands: %#v", report.NextSteps)
	}
	if _, err := captureStdout(t, func() error {
		return runRefresh([]string{"admin/review", "--apply", "--yes", "--json"})
	}); err == nil || !strings.Contains(err.Error(), "--json is preview-only") {
		t.Fatalf("refresh --apply --json err = %v, want preview-only requirement", err)
	}
	applyOut, err := captureStdout(t, func() error {
		return runRefresh([]string{"admin/review", "--apply", "--yes"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(applyOut, "admin/review: updated from source") {
		t.Fatalf("unexpected apply output:\n%s", applyOut)
	}
	if !strings.Contains(applyOut, "skillsdex sync --dry-run --strict") {
		t.Fatalf("apply output missing sync next step:\n%s", applyOut)
	}
}

func TestRunRefreshAllFiltersImportedActiveSkills(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	var refreshed []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/skills":
			_, _ = w.Write([]byte(`{"skills":[{"id":"skl_1","owner":"admin","slug":"imported","name":"Imported","description":"Use when imported.","source_url":"https://github.com/acme/skills/tree/main/imported","latest_revision":"rev_1","revision_number":1,"created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T00:00:00Z"},{"id":"skl_2","owner":"admin","slug":"local","name":"Local","description":"Use when local.","latest_revision":"rev_2","revision_number":1,"created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T00:00:00Z"},{"id":"skl_3","owner":"admin","slug":"archived","name":"Archived","description":"Use when archived.","source_url":"https://github.com/acme/skills/tree/main/archived","archived_at":"2026-06-08T01:00:00Z","latest_revision":"rev_3","revision_number":1,"created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T00:00:00Z"}]}`))
		case "/v1/skills/admin/imported/refresh":
			refreshed = append(refreshed, r.URL.RawQuery)
			if r.URL.Query().Get("dry_run") != "1" {
				t.Fatalf("expected dry-run refresh, got %s", r.URL.String())
			}
			_, _ = w.Write([]byte(`{"skill":{"id":"skl_1","owner":"admin","slug":"imported","name":"Imported","description":"Use when imported.","source_url":"https://github.com/acme/skills/tree/main/imported","latest_revision":"rev_1","revision_number":1,"created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T00:00:00Z"},"status":"already current","dry_run":true}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	if err := saveAuth(AuthConfig{ServerURL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		return runRefresh([]string{"--all"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Previewing source refresh for 1 imported skill(s)") || !strings.Contains(out, "admin/imported: already current") {
		t.Fatalf("unexpected refresh all output:\n%s", out)
	}
	if len(refreshed) != 1 {
		t.Fatalf("expected one refresh request, got %#v", refreshed)
	}
}

func TestRunRefreshAllStrictFailsWhenUpdatesAreAvailable(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/skills":
			_, _ = w.Write([]byte(`{"skills":[{"id":"skl_1","owner":"admin","slug":"review","name":"Review","description":"Use when reviewing.","source_url":"https://github.com/acme/skills/tree/main/review","latest_revision":"rev_1","revision_number":1,"created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T00:00:00Z"}]}`))
		case "/v1/skills/admin/review/refresh":
			if r.URL.Query().Get("dry_run") != "1" {
				t.Fatalf("expected dry-run refresh, got %s", r.URL.String())
			}
			_, _ = w.Write([]byte(`{"skill":{"id":"skl_1","owner":"admin","slug":"review","name":"Review","description":"Use when reviewing.","source_url":"https://github.com/acme/skills/tree/main/review","latest_revision":"rev_1","revision_number":1,"created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T00:00:00Z"},"status":"update available","dry_run":true,"changes":[{"path":"SKILL.md","status":"modified"}]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	if err := saveAuth(AuthConfig{ServerURL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		return runRefresh([]string{"--all", "--strict"})
	})
	if err == nil || !strings.Contains(err.Error(), "1 source update(s) available") {
		t.Fatalf("refresh all strict err = %v, want update count", err)
	}
	if !strings.Contains(out, "admin/review: update available") || !strings.Contains(out, "modified\tSKILL.md") {
		t.Fatalf("refresh all strict output missing update summary:\n%s", out)
	}
	jsonOut, err := captureStdout(t, func() error {
		return runRefresh([]string{"--all", "--strict", "--json"})
	})
	if err == nil || !strings.Contains(err.Error(), "1 source update(s) available") {
		t.Fatalf("refresh all strict JSON err = %v, want update count", err)
	}
	var report refreshReport
	if err := json.Unmarshal([]byte(jsonOut), &report); err != nil {
		t.Fatalf("refresh all JSON did not decode: %v\n%s", err, jsonOut)
	}
	if report.Healthy || !report.All || !report.Strict || report.CheckedCount != 1 || report.UpdateCount != 1 || report.FailureCount != 0 || report.StatusCounts["update available"] != 1 {
		t.Fatalf("unexpected refresh all JSON: %#v", report)
	}
	if len(report.Results) != 1 || report.Results[0].Identifier != "admin/review" || report.Results[0].Status != "update available" {
		t.Fatalf("unexpected refresh all JSON results: %#v", report.Results)
	}
}

func TestRunRefreshAllReturnsErrorWhenAnyRefreshFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/skills":
			_, _ = w.Write([]byte(`{"skills":[{"id":"skl_1","owner":"admin","slug":"ok","name":"OK","description":"Use when ok.","source_url":"https://github.com/acme/skills/tree/main/ok","latest_revision":"rev_1","revision_number":1,"created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T00:00:00Z"},{"id":"skl_2","owner":"admin","slug":"bad","name":"Bad","description":"Use when bad.","source_url":"https://github.com/acme/skills/tree/main/bad","latest_revision":"rev_2","revision_number":1,"created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T00:00:00Z"}]}`))
		case "/v1/skills/admin/ok/refresh":
			_, _ = w.Write([]byte(`{"skill":{"id":"skl_1","owner":"admin","slug":"ok","name":"OK","description":"Use when ok.","source_url":"https://github.com/acme/skills/tree/main/ok","latest_revision":"rev_1","revision_number":1,"created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T00:00:00Z"},"status":"already current","dry_run":true}`))
		case "/v1/skills/admin/bad/refresh":
			writeJSONError(w, http.StatusBadRequest, "upstream unavailable")
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	if err := saveAuth(AuthConfig{ServerURL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		return runRefresh([]string{"--all"})
	})
	if err == nil || !strings.Contains(err.Error(), "1 source refresh(es) failed") {
		t.Fatalf("refresh all err = %v, want failed count", err)
	}
	if !strings.Contains(out, "admin/ok: already current") {
		t.Fatalf("successful refresh output missing:\n%s", out)
	}
}

func TestRunLintChecksLocalSkillFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	path := filepath.Join(home, "SKILL.md")
	if err := os.WriteFile(path, []byte("---\nname: Weak\ndescription: Do things.\n---\n\nTODO\n"), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, func() error {
		return runLint([]string{"--file", path})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"needs attention", "Description is too short", "Trigger language is unclear", "TODO text found"} {
		if !strings.Contains(out, want) {
			t.Fatalf("lint output missing %q:\n%s", want, out)
		}
	}
	if _, err := captureStdout(t, func() error {
		return runLint([]string{"--strict", "--file", path})
	}); err == nil || !strings.Contains(err.Error(), "quality warnings found") {
		t.Fatalf("strict lint err = %v, want warnings found", err)
	}
}

func TestRunLintChecksLocalSkillDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	dir := filepath.Join(home, "review-skill")
	if err := os.MkdirAll(filepath.Join(dir, "references"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "scripts"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("---\nname: Review\ndescription: Use when reviewing a skill folder before publishing.\nmetadata:\n  short-description: Review skill folders\n---\n\n# Review\n\nInspect the local skill package instructions, supporting files, trigger wording, expected output, install behavior, quality warnings, and any sensitive material before publishing it into SkillsDex."), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "references", "private.md"), []byte("Remove any password or access token examples before publishing."), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scripts", "check.sh"), []byte("#!/bin/sh\nexit 0\n"), 0600); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		return runLint([]string{"--file", dir})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{dir + ": needs attention", "Supporting files may contain sensitive material", "Script is not executable", "scripts/check.sh"} {
		if !strings.Contains(out, want) {
			t.Fatalf("directory lint output missing %q:\n%s", want, out)
		}
	}
}

func TestRunLintCanPrintJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	path := filepath.Join(home, "SKILL.md")
	if err := os.WriteFile(path, []byte("---\nname: Weak\ndescription: Do things.\n---\n\nTODO\n"), 0600); err != nil {
		t.Fatal(err)
	}
	out, err := captureStdout(t, func() error {
		return runLint([]string{"--json", "--strict", "--file", path})
	})
	if err == nil || !strings.Contains(err.Error(), "quality warnings found") {
		t.Fatalf("strict lint err = %v, want warnings found", err)
	}
	var report lintReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		t.Fatalf("lint JSON did not decode: %v\n%s", err, out)
	}
	if report.Target != path || report.Summary != "needs attention" || report.WarningCount == 0 || report.IssueCount != len(report.Issues) {
		t.Fatalf("unexpected lint report: %#v", report)
	}
	if len(report.Issues) == 0 || report.Issues[0].Title == "" {
		t.Fatalf("expected issue details in report: %#v", report.Issues)
	}
}

func TestRunLintChecksRemoteSkillWithContext(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	var sawSkill, sawList bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/skills/admin/review":
			sawSkill = true
			_, _ = w.Write([]byte(`{"skill":{"id":"skl_1","owner":"admin","slug":"review","name":"Review","description":"Use when reviewing a change for bugs and regressions.","short_description":"Review changes for concrete risks","source_provider":"github","source_url":"https://github.com/acme/skills/tree/main/review","latest_revision":"rev_1","revision_number":1,"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"},"raw":"---\nname: Review\ndescription: Use when reviewing a change for bugs and regressions.\nmetadata:\n  short-description: Review changes for concrete risks\n---\n\n# Review\n\nInspect the goal, changed files, tests, and runtime behavior. Report confirmed findings first with severity, file references, reproduction notes, and missing coverage. Keep questions separate from bugs."}`))
		case "/v1/skills":
			sawList = true
			_, _ = w.Write([]byte(`{"skills":[{"id":"skl_1","owner":"admin","slug":"review","name":"Review","description":"Use when reviewing a change for bugs and regressions.","short_description":"Review changes for concrete risks","latest_revision":"rev_1","revision_number":1,"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"},{"id":"skl_2","owner":"admin","slug":"code-review","name":"Code Review","description":"Use when reviewing a change for bugs and regressions.","short_description":"Review changes for concrete risks","latest_revision":"rev_2","revision_number":1,"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	if err := saveAuth(AuthConfig{ServerURL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		return runLint([]string{"admin/review"})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"admin/review: review", "Possible duplicate skill", "Source may be stale", "admin/code-review"} {
		if !strings.Contains(out, want) {
			t.Fatalf("remote lint output missing %q:\n%s", want, out)
		}
	}
	if !sawSkill || !sawList {
		t.Fatalf("expected skill and list requests, saw skill=%v list=%v", sawSkill, sawList)
	}
}

func TestRunLintAllChecksActiveRemoteSkills(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	var sawReview, sawWeak bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/skills":
			_, _ = w.Write([]byte(`{"skills":[{"id":"skl_1","owner":"admin","slug":"weak","name":"Weak","description":"Do things.","short_description":"","latest_revision":"rev_1","revision_number":1,"created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T00:00:00Z"},{"id":"skl_2","owner":"admin","slug":"review","name":"Review","description":"Use when reviewing a change for concrete risks.","short_description":"Review changes","latest_revision":"rev_2","revision_number":1,"created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T00:00:00Z"},{"id":"skl_3","owner":"admin","slug":"archived","name":"Archived","description":"Do not fetch.","short_description":"","latest_revision":"rev_3","revision_number":1,"archived_at":"2026-06-08T01:00:00Z","created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T00:00:00Z"}]}`))
		case "/v1/skills/admin/review":
			sawReview = true
			_, _ = w.Write([]byte(`{"skill":{"id":"skl_2","owner":"admin","slug":"review","name":"Review","description":"Use when reviewing a change for concrete risks.","short_description":"Review changes","latest_revision":"rev_2","revision_number":1,"created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T00:00:00Z"},"raw":"---\nname: Review\ndescription: Use when reviewing a change for concrete risks.\nmetadata:\n  short-description: Review changes\n---\n\n# Review\n\nInspect the changed behavior, test coverage, runtime behavior, data-loss risk, compatibility impact, migration safety, and user-visible regressions before reporting confirmed findings with severity and evidence."}`))
		case "/v1/skills/admin/weak":
			sawWeak = true
			_, _ = w.Write([]byte(`{"skill":{"id":"skl_1","owner":"admin","slug":"weak","name":"Weak","description":"Do things.","short_description":"","latest_revision":"rev_1","revision_number":1,"created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T00:00:00Z"},"raw":"---\nname: Weak\ndescription: Do things.\n---\n\nTODO"}`))
		case "/v1/skills/admin/archived":
			t.Fatal("archived skill should not be fetched")
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	if err := saveAuth(AuthConfig{ServerURL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		return runLint([]string{"--all"})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Linted 2 active skill(s): needs attention", "admin/review: ready", "admin/weak: needs attention"} {
		if !strings.Contains(out, want) {
			t.Fatalf("lint all output missing %q:\n%s", want, out)
		}
	}
	if !sawReview || !sawWeak {
		t.Fatalf("expected active skill fetches, saw review=%v weak=%v", sawReview, sawWeak)
	}
}

func TestRunLintAllCanPrintJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/skills":
			_, _ = w.Write([]byte(`{"skills":[{"id":"skl_1","owner":"admin","slug":"weak","name":"Weak","description":"Do things.","short_description":"","latest_revision":"rev_1","revision_number":1,"created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T00:00:00Z"}]}`))
		case "/v1/skills/admin/weak":
			_, _ = w.Write([]byte(`{"skill":{"id":"skl_1","owner":"admin","slug":"weak","name":"Weak","description":"Do things.","short_description":"","latest_revision":"rev_1","revision_number":1,"created_at":"2026-06-08T00:00:00Z","updated_at":"2026-06-08T00:00:00Z"},"raw":"---\nname: Weak\ndescription: Do things.\n---\n\nTODO"}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	if err := saveAuth(AuthConfig{ServerURL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		return runLint([]string{"--all", "--json", "--strict"})
	})
	if err == nil || !strings.Contains(err.Error(), "quality warnings found") {
		t.Fatalf("strict lint err = %v, want warnings found", err)
	}
	var reports lintReports
	if err := json.Unmarshal([]byte(out), &reports); err != nil {
		t.Fatalf("lint all JSON did not decode: %v\n%s", err, out)
	}
	if reports.SkillCount != 1 || reports.Summary != "needs attention" || reports.WarningCount == 0 || len(reports.Reports) != 1 {
		t.Fatalf("unexpected aggregate report: %#v", reports)
	}
	if reports.Reports[0].Target != "admin/weak" || reports.Reports[0].IssueCount == 0 {
		t.Fatalf("unexpected child report: %#v", reports.Reports[0])
	}
}

func TestRunGitHubExportDownloadsRepoShape(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, err := zw.Create("skills/admin/review/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("# Review\n")); err != nil {
		t.Fatal(err)
	}
	readme, err := zw.Create("README.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readme.Write([]byte("# Export\n")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/export/github" || r.Method != http.MethodGet {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(buf.Bytes())
	}))
	defer server.Close()
	if err := saveAuth(AuthConfig{ServerURL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}

	outDir := filepath.Join(home, "repo")
	out, err := captureStdout(t, func() error {
		return runGitHub([]string{"export", "--dir", outDir, "--yes"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Exported 2 file(s)") {
		t.Fatalf("unexpected github export output:\n%s", out)
	}
	got, err := os.ReadFile(filepath.Join(outDir, "skills", "admin", "review", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "# Review\n" {
		t.Fatalf("exported skill = %q", got)
	}
}

func TestRunBackupExportAndRestoreUseAuthenticatedAPI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	var backup bytes.Buffer
	zw := zip.NewWriter(&backup)
	manifest, err := zw.Create("skillsdex-export.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manifest.Write([]byte(`{"exported_at":"2026-06-08T00:00:00Z","user":"admin","skills":[]}`)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	var sawExport, sawRestore bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("authorization = %q", r.Header.Get("Authorization"))
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/backup":
			sawExport = true
			w.Header().Set("Content-Type", "application/zip")
			_, _ = w.Write(backup.Bytes())
		case r.Method == http.MethodPost && r.URL.Path == "/v1/backup":
			sawRestore = true
			if ct := r.Header.Get("Content-Type"); ct != "application/zip" {
				t.Fatalf("restore content type = %q", ct)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(body, backup.Bytes()) {
				t.Fatal("restore uploaded unexpected backup bytes")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"results":[{"Identifier":"admin/restored","Name":"Restored","Status":"created"}]}`))
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	if err := saveAuth(AuthConfig{ServerURL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}

	outFile := filepath.Join(home, "backups", "skillsdex.zip")
	out, err := captureStdout(t, func() error {
		return runBackup([]string{"export", "--out", outFile})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sawExport || !strings.Contains(out, "Exported backup to "+outFile) {
		t.Fatalf("unexpected backup export saw=%v output:\n%s", sawExport, out)
	}
	got, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, backup.Bytes()) {
		t.Fatal("backup export wrote unexpected bytes")
	}
	info, err := os.Stat(outFile)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("backup mode = %v, want 0600", info.Mode().Perm())
	}

	sawRestore = false
	dryRunOut, err := captureStdout(t, func() error {
		return runBackup([]string{"restore", "--file", outFile, "--dry-run"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if sawRestore {
		t.Fatalf("backup restore --dry-run uploaded data:\n%s", dryRunOut)
	}
	for _, want := range []string{"Restore dry-run: backup was not uploaded.", "Backup is valid: " + outFile, "0 skill(s): 0 active"} {
		if !strings.Contains(dryRunOut, want) {
			t.Fatalf("backup restore dry-run output missing %q:\n%s", want, dryRunOut)
		}
	}

	dryRunJSON, err := captureStdout(t, func() error {
		return runBackup([]string{"restore", "--file", outFile, "--dry-run", "--json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	var dryRunReport backupInspectOutput
	if err := json.Unmarshal([]byte(dryRunJSON), &dryRunReport); err != nil {
		t.Fatalf("restore dry-run JSON did not decode: %v\n%s", err, dryRunJSON)
	}
	if !dryRunReport.Valid || !dryRunReport.RestoreDryRun || dryRunReport.File != outFile || dryRunReport.Summary.SkillCount != 0 {
		t.Fatalf("unexpected restore dry-run JSON: %#v", dryRunReport)
	}

	if _, err := captureStdout(t, func() error {
		return runBackup([]string{"restore", "--file", outFile, "--json"})
	}); err == nil || !strings.Contains(err.Error(), "--json requires --dry-run") {
		t.Fatalf("restore --json err = %v, want dry-run requirement", err)
	}

	restoreOut, err := captureStdout(t, func() error {
		return runBackup([]string{"restore", "--file", outFile})
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sawRestore || !strings.Contains(restoreOut, "admin/restored") || !strings.Contains(restoreOut, "created") {
		t.Fatalf("unexpected backup restore saw=%v output:\n%s", sawRestore, restoreOut)
	}

}

func TestRunBackupInspectValidatesAndSummarizesLocalBackup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SKILLSDEX_HOME", filepath.Join(home, ".skillsdex"))

	backup := testSkillsDexBackup(t, exportManifest{
		ExportedAt: "2026-06-08T00:00:00Z",
		User:       "admin",
		Skills: []exportSkillMeta{
			{
				Identifier:         "admin/active",
				Name:               "Active Skill",
				Description:        "Use when testing backup inspection.",
				RevisionNumber:     3,
				RecommendedDefault: true,
				PinnedInstall:      true,
				SourceProvider:     "github",
				SourceURL:          "https://github.com/example/skills/tree/main/active",
				Path:               "admin--active/SKILL.md",
			},
			{
				Identifier:     "admin/old",
				Name:           "Old Skill",
				Description:    "Use when testing archived backup inspection.",
				RevisionNumber: 1,
				Archived:       true,
				Path:           "admin--old/SKILL.md",
			},
		},
	}, map[string]string{
		"admin--active/SKILL.md":            "# Active\n\nUse this active skill during backup inspection tests.",
		"admin--active/references/guide.md": "# Guide\n",
		"admin--old/SKILL.md":               "# Old\n\nArchived instructions for backup inspection tests.",
	})
	outFile := filepath.Join(home, "skillsdex-backup.zip")
	if err := os.WriteFile(outFile, backup, 0600); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, func() error {
		return runBackup([]string{"inspect", "--file", outFile})
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"Backup is valid: " + outFile,
		"Exported by admin at 2026-06-08T00:00:00Z",
		"2 skill(s): 1 active, 1 archived, 1 pinned, 1 recommended, 1 source-backed, 1 supporting file(s)",
		"IDENTIFIER",
		"admin/active",
		"Active Skill",
		"3",
		"active",
		"2",
		"pinned,recommended",
		"GitHub",
		"admin/old",
		"archived",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("backup inspect output missing %q:\n%s", want, out)
		}
	}

	jsonOut, err := captureStdout(t, func() error {
		return runBackup([]string{"inspect", "--file", outFile, "--json"})
	})
	if err != nil {
		t.Fatal(err)
	}
	var report backupInspectOutput
	if err := json.Unmarshal([]byte(jsonOut), &report); err != nil {
		t.Fatalf("inspect JSON did not decode: %v\n%s", err, jsonOut)
	}
	if !report.Valid || report.RestoreDryRun || report.File != outFile {
		t.Fatalf("unexpected inspect JSON metadata: %#v", report)
	}
	if report.Summary.SkillCount != 2 || report.Summary.ActiveCount != 1 || report.Summary.ArchivedCount != 1 || report.Summary.SupportingFiles != 1 {
		t.Fatalf("unexpected inspect JSON summary: %#v", report.Summary)
	}
	if len(report.Summary.Rows) != 2 || report.Summary.Rows[0].Identifier != "admin/active" || report.Summary.Rows[0].FileCount != 2 {
		t.Fatalf("unexpected inspect JSON rows: %#v", report.Summary.Rows)
	}
}

func TestRunBackupInspectRejectsInvalidBackup(t *testing.T) {
	home := t.TempDir()
	outFile := filepath.Join(home, "bad-backup.zip")
	backup := testSkillsDexBackup(t, exportManifest{
		ExportedAt: "2026-06-08T00:00:00Z",
		User:       "admin",
		Skills: []exportSkillMeta{{
			Identifier:  "admin/bad",
			Name:        "Bad",
			Description: "Use when testing invalid backup inspection.",
			Path:        "admin--bad/../SKILL.md",
		}},
	}, map[string]string{
		"admin--bad/../SKILL.md": "# Bad\n",
	})
	if err := os.WriteFile(outFile, backup, 0600); err != nil {
		t.Fatal(err)
	}

	_, err := captureStdout(t, func() error {
		return runBackup([]string{"inspect", "--file", outFile})
	})
	if err == nil || !strings.Contains(err.Error(), "backup skill path must not contain traversal") {
		t.Fatalf("inspect err = %v, want traversal validation", err)
	}
}

func testSkillsDexBackup(t *testing.T, manifest exportManifest, files map[string]string) []byte {
	t.Helper()
	var backup bytes.Buffer
	zw := zip.NewWriter(&backup)
	for name, content := range files {
		f, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	manifestFile, err := zw.Create("skillsdex-export.json")
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manifestFile.Write(manifestBytes); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return backup.Bytes()
}

func TestCLIParserBehaviorSpecs(t *testing.T) {
	t.Run("sync flags compose and reject conflicting confirmation", func(t *testing.T) {
		tests := []struct {
			name    string
			args    []string
			want    syncOptions
			wantErr string
		}{
			{name: "dry run strict JSON", args: []string{"--dry-run", "--strict", "--json"}, want: syncOptions{Confirm: confirmOptions{DryRun: true}, Strict: true, JSON: true}},
			{name: "yes and no conflict", args: []string{"--yes", "--no"}, wantErr: "--yes and --no cannot be used together"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, err := parseSyncArgs(tt.args)
				if tt.wantErr != "" {
					if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
						t.Fatalf("parseSyncArgs err = %v, want %q", err, tt.wantErr)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, tt.want) {
					t.Fatalf("sync options = %#v, want %#v", got, tt.want)
				}
			})
		}
	})

	t.Run("setup chooses one install source", func(t *testing.T) {
		tests := []struct {
			name    string
			args    []string
			want    setupOptions
			wantErr string
		}{
			{name: "explicit install set apply yes", args: []string{"--set", "laptop", "--apply", "--yes"}, want: setupOptions{Mode: setupInstallSet, SetSlug: "laptop", Apply: true, Confirm: confirmOptions{Yes: true}}},
			{name: "pinned and defaults conflict", args: []string{"--pinned", "--defaults"}, wantErr: "choose only one install source"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, err := parseSetupArgs(tt.args)
				if tt.wantErr != "" {
					if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
						t.Fatalf("parseSetupArgs err = %v, want %q", err, tt.wantErr)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, tt.want) {
					t.Fatalf("setup options = %#v, want %#v", got, tt.want)
				}
			})
		}
	})

	t.Run("refresh targets exactly one scope", func(t *testing.T) {
		tests := []struct {
			name       string
			args       []string
			wantTarget string
			wantAll    bool
			wantStrict bool
			wantJSON   bool
			wantErr    string
		}{
			{name: "single target strict JSON", args: []string{"admin/review", "--strict", "--json"}, wantTarget: "admin/review", wantStrict: true, wantJSON: true},
			{name: "all rejects explicit target", args: []string{"--all", "admin/review"}, wantErr: "--all cannot be combined with an identifier"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				target, all, _, strict, _, jsonOutput, _, err := parseRefreshArgs(tt.args)
				if tt.wantErr != "" {
					if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
						t.Fatalf("parseRefreshArgs err = %v, want %q", err, tt.wantErr)
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if target != tt.wantTarget || all != tt.wantAll || strict != tt.wantStrict || jsonOutput != tt.wantJSON {
					t.Fatalf("refresh parse = target:%q all:%v strict:%v json:%v", target, all, strict, jsonOutput)
				}
			})
		}
	})
}

func TestZipDestinationBehaviorSpecs(t *testing.T) {
	root, err := filepath.Abs(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		entry   string
		wantRel string
		wantErr string
	}{
		{name: "skill file under root", entry: "skills/admin/review/SKILL.md", wantRel: filepath.Join("skills", "admin", "review", "SKILL.md")},
		{name: "reference file under root", entry: "skills/admin/review/references/guide.md", wantRel: filepath.Join("skills", "admin", "review", "references", "guide.md")},
		{name: "empty entry", entry: "", wantErr: "unsafe export path"},
		{name: "dot path", entry: ".", wantErr: "unsafe export path"},
		{name: "parent traversal", entry: "../evil/SKILL.md", wantErr: "unsafe export path"},
		{name: "nested traversal", entry: "skills/../evil/SKILL.md", wantErr: "unsafe export path"},
		{name: "windows separators", entry: `skills\admin\review\SKILL.md`, wantErr: "unsafe export path"},
		{name: "absolute path", entry: "/tmp/evil/SKILL.md", wantErr: "unsafe export path"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := safeZipDestination(root, tt.entry)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("safeZipDestination err = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := filepath.Join(root, tt.wantRel)
			if got != want {
				t.Fatalf("destination = %q, want %q", got, want)
			}
		})
	}
}

func TestExtractZipToDirRejectsTraversal(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, err := zw.Create("../evil/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte("# Evil")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	_, err = extractZipToDir(zr, t.TempDir(), confirmOptions{Yes: true})
	if err == nil || !strings.Contains(err.Error(), "unsafe export path") {
		t.Fatalf("expected unsafe export path error, got %v", err)
	}
}
