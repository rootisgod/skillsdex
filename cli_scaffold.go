package skillsdex

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
)

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
