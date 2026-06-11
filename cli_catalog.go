package skillsdex

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"text/tabwriter"
)

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
