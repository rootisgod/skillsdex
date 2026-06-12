package skillsdex

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"

	tea "github.com/charmbracelet/bubbletea"
)

type tuiDataMsg struct {
	Auth      AuthConfig
	Username  string
	Target    string
	Skills    []Skill
	Sets      []InstallSet
	Installed installedStatusReport
	Sync      syncDryRunReport
	Doctor    doctorReport
	Err       error
}

type tuiSkillDetailMsg struct {
	Identifier string
	Response   skillResponse
	Err        error
}

type tuiSetDetailMsg struct {
	Slug     string
	Response installSetResponse
	Err      error
}

type tuiActionMsg struct {
	Label  string
	Output string
	Err    error
}

func tuiLoadDataCmd() tea.Cmd {
	return func() tea.Msg {
		msg := tuiDataMsg{Target: "codex"}
		if cfg, err := loadLocalConfig(); err == nil && cfg.DefaultTarget != "" {
			msg.Target = cfg.DefaultTarget
		}
		manifest, manifestErr := loadManifest()
		auth, authErr := loadAuth()
		var client *apiClient
		if authErr == nil {
			msg.Auth = auth
			client = newAPIClient(auth)
			var me map[string]any
			if err := client.getJSON("/v1/me", &me); err != nil {
				msg.Err = fmt.Errorf("server check failed: %w", err)
				client = nil
			} else {
				msg.Username = fmt.Sprint(me["username"])
			}
		} else {
			msg.Err = authErr
		}
		if client != nil {
			var skills struct {
				Skills []Skill `json:"skills"`
			}
			if err := client.getJSON("/v1/skills", &skills); err != nil {
				msg.Err = err
			} else {
				msg.Skills = skills.Skills
				sort.Slice(msg.Skills, func(i, j int) bool {
					return msg.Skills[i].OwnerSlug+"/"+msg.Skills[i].Slug < msg.Skills[j].OwnerSlug+"/"+msg.Skills[j].Slug
				})
			}
			var sets installSetsResponse
			if err := client.getJSON("/v1/install-sets", &sets); err != nil {
				msg.Err = err
			} else {
				msg.Sets = sets.Sets
				sort.Slice(msg.Sets, func(i, j int) bool {
					return msg.Sets[i].Slug < msg.Sets[j].Slug
				})
			}
		}
		if manifestErr != nil {
			msg.Err = manifestErr
		} else {
			msg.Installed = buildInstalledStatusReport(installedOptions{Refresh: client != nil}, manifest, client)
			if client != nil {
				syncReport, err := buildSyncDryRunReport(syncOptions{Confirm: confirmOptions{DryRun: true}}, manifest, client)
				if err != nil && msg.Err == nil {
					msg.Err = err
				}
				msg.Sync = syncReport
			}
		}
		if report, _, err := buildDoctorReport(doctorOptions{Refresh: client != nil}); err == nil {
			msg.Doctor = report
		} else if msg.Err == nil {
			msg.Err = err
		}
		return msg
	}
}

func tuiFetchSkillCmd(client *apiClient, owner, slug string) tea.Cmd {
	return func() tea.Msg {
		resp, err := client.fetchSkill(owner, slug)
		return tuiSkillDetailMsg{Identifier: owner + "/" + slug, Response: resp, Err: err}
	}
}

func tuiFetchSetCmd(client *apiClient, slug string) tea.Cmd {
	return func() tea.Msg {
		resp, err := client.fetchInstallSet(slug)
		return tuiSetDetailMsg{Slug: slug, Response: resp, Err: err}
	}
}

func tuiCapturedActionCmd(label string, fn func() error) tea.Cmd {
	return func() tea.Msg {
		output, err := captureCLIOutput(fn)
		return tuiActionMsg{Label: label, Output: output, Err: err}
	}
}

var captureOutputMu sync.Mutex

func captureCLIOutput(fn func() error) (string, error) {
	captureOutputMu.Lock()
	defer captureOutputMu.Unlock()

	oldStdout := os.Stdout
	oldStderr := os.Stderr
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		return "", err
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		_ = stdoutR.Close()
		_ = stdoutW.Close()
		return "", err
	}
	var stdoutBuf, stderrBuf bytes.Buffer
	done := make(chan error, 2)
	go func() {
		_, err := io.Copy(&stdoutBuf, stdoutR)
		done <- err
	}()
	go func() {
		_, err := io.Copy(&stderrBuf, stderrR)
		done <- err
	}()

	os.Stdout = stdoutW
	os.Stderr = stderrW
	runErr := fn()
	_ = stdoutW.Close()
	_ = stderrW.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr
	stdoutErr := <-done
	stderrErr := <-done
	_ = stdoutR.Close()
	_ = stderrR.Close()
	if runErr == nil {
		if stdoutErr != nil {
			runErr = stdoutErr
		} else if stderrErr != nil {
			runErr = stderrErr
		}
	}
	output := strings.TrimSpace(stdoutBuf.String())
	if stderr := strings.TrimSpace(stderrBuf.String()); stderr != "" {
		if output != "" {
			output += "\n"
		}
		output += stderr
	}
	return output, runErr
}
