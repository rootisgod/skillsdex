package skillsdex

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

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

func parseInvitesArgs(args []string) (invitesOptions, error) {
	var opts invitesOptions
	for _, arg := range args {
		switch arg {
		case "--json":
			opts.JSON = true
		default:
			return invitesOptions{}, fmt.Errorf("unknown argument %s", arg)
		}
	}
	return opts, nil
}

func parseInviteCreateArgs(args []string) (inviteCreateOptions, error) {
	var opts inviteCreateOptions
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--json":
			opts.JSON = true
		case "--email":
			i++
			if i >= len(args) {
				return inviteCreateOptions{}, errors.New("--email requires a value")
			}
			opts.Email = strings.TrimSpace(args[i])
		default:
			if strings.HasPrefix(arg, "--email=") {
				opts.Email = strings.TrimSpace(strings.TrimPrefix(arg, "--email="))
				continue
			}
			return inviteCreateOptions{}, fmt.Errorf("unknown argument %s", arg)
		}
	}
	return opts, nil
}

func parsePluginsArgs(args []string) (pluginsOptions, error) {
	var opts pluginsOptions
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--json":
			opts.JSON = true
		case arg == "--target":
			if i+1 >= len(args) {
				return pluginsOptions{}, errors.New("missing target")
			}
			opts.Target = strings.TrimSpace(args[i+1])
			i++
		case strings.HasPrefix(arg, "--target="):
			opts.Target = strings.TrimSpace(strings.TrimPrefix(arg, "--target="))
		case strings.HasPrefix(arg, "-"):
			return pluginsOptions{}, fmt.Errorf("unknown flag %s", arg)
		default:
			arg = strings.TrimSpace(arg)
			if arg == "" {
				return pluginsOptions{}, errors.New("empty plugin slug")
			}
			if opts.Slug != "" {
				return pluginsOptions{}, errors.New("too many arguments")
			}
			opts.Slug = arg
		}
	}
	if opts.Slug != "" {
		if err := validateSlug("plugin slug", opts.Slug); err != nil {
			return pluginsOptions{}, err
		}
	}
	if opts.Target != "" {
		if err := validateSlug("plugin target", opts.Target); err != nil {
			return pluginsOptions{}, err
		}
	}
	return opts, nil
}

func parsePluginAddArgs(args []string) (pluginAddOptions, error) {
	var opts pluginAddOptions
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--name":
			if i+1 >= len(args) {
				return pluginAddOptions{}, errors.New("missing name")
			}
			opts.Input.Name = args[i+1]
			i++
		case strings.HasPrefix(arg, "--name="):
			opts.Input.Name = strings.TrimPrefix(arg, "--name=")
		case arg == "--description":
			if i+1 >= len(args) {
				return pluginAddOptions{}, errors.New("missing description")
			}
			opts.Input.Description = args[i+1]
			i++
		case strings.HasPrefix(arg, "--description="):
			opts.Input.Description = strings.TrimPrefix(arg, "--description=")
		case arg == "--url":
			if i+1 >= len(args) {
				return pluginAddOptions{}, errors.New("missing url")
			}
			opts.Input.SourceURL = args[i+1]
			i++
		case strings.HasPrefix(arg, "--url="):
			opts.Input.SourceURL = strings.TrimPrefix(arg, "--url=")
		case arg == "--target":
			if i+1 >= len(args) {
				return pluginAddOptions{}, errors.New("missing target")
			}
			opts.Input.Target = args[i+1]
			i++
		case strings.HasPrefix(arg, "--target="):
			opts.Input.Target = strings.TrimPrefix(arg, "--target=")
		case arg == "--command":
			if i+1 >= len(args) {
				return pluginAddOptions{}, errors.New("missing command")
			}
			opts.Input.InstallCommand = args[i+1]
			i++
		case strings.HasPrefix(arg, "--command="):
			opts.Input.InstallCommand = strings.TrimPrefix(arg, "--command=")
		case strings.HasPrefix(arg, "-"):
			return pluginAddOptions{}, fmt.Errorf("unknown flag %s", arg)
		default:
			if opts.Input.Slug != "" {
				return pluginAddOptions{}, errors.New("too many arguments")
			}
			opts.Input.Slug = strings.TrimSpace(arg)
		}
	}
	if err := validatePluginInstallInput(opts.Input); err != nil {
		return pluginAddOptions{}, err
	}
	return opts, nil
}

func parsePluginInstallArgs(args []string) (pluginInstallOptions, error) {
	var opts pluginInstallOptions
	for _, arg := range args {
		switch {
		case arg == "--dry-run":
			opts.Confirm.DryRun = true
		case arg == "--yes" || arg == "-y":
			opts.Confirm.Yes = true
		case arg == "--no" || arg == "-n":
			opts.Confirm.No = true
		case strings.HasPrefix(arg, "-"):
			return pluginInstallOptions{}, fmt.Errorf("unknown flag %s", arg)
		default:
			if opts.Slug != "" {
				return pluginInstallOptions{}, errors.New("too many arguments")
			}
			opts.Slug = strings.TrimSpace(arg)
		}
	}
	if opts.Slug == "" {
		return pluginInstallOptions{}, errors.New("missing plugin slug")
	}
	if err := validateSlug("plugin slug", opts.Slug); err != nil {
		return pluginInstallOptions{}, err
	}
	if opts.Confirm.Yes && opts.Confirm.No {
		return pluginInstallOptions{}, errors.New("--yes and --no cannot be used together")
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
