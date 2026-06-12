package skillsdex

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
)

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
	report := buildInstalledStatusReport(opts, manifest, client)
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

func buildInstalledStatusReport(opts installedOptions, manifest Manifest, client *apiClient) installedStatusReport {
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
	return report
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
