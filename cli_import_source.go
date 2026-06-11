package skillsdex

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
)

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
