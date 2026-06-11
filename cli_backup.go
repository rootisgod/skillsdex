package skillsdex

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
)

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
