package skillsdex

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var version = "0.1.0-phase1"

func RunCLI(args []string) int {
	if len(args) < 1 {
		cliUsage()
		return 2
	}

	switch args[0] {
	case "serve":
		code, err := runDeprecatedServe(args[1:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "serve: %v\n", err)
		}
		return code
	case "login":
		if err := runLogin(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "login: %v\n", err)
			return 1
		}
	case "logout":
		if err := runLogout(); err != nil {
			fmt.Fprintf(os.Stderr, "logout: %v\n", err)
			return 1
		}
	case "whoami":
		if err := runWhoami(); err != nil {
			fmt.Fprintf(os.Stderr, "whoami: %v\n", err)
			return 1
		}
	case "config":
		if err := runConfig(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "config: %v\n", err)
			return 1
		}
	case "skills":
		if err := runSkills(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "skills: %v\n", err)
			return 1
		}
	case "search":
		if err := runPublicSearch(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "search: %v\n", err)
			return 1
		}
	case "top":
		if err := runPublicTop(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "top: %v\n", err)
			return 1
		}
	case "import-source":
		if err := runImportSource(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "import-source: %v\n", err)
			return 1
		}
	case "publish":
		if err := runPublish(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "publish: %v\n", err)
			return 1
		}
	case "refresh":
		if err := runRefresh(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "refresh: %v\n", err)
			return 1
		}
	case "github":
		if err := runGitHub(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "github: %v\n", err)
			return 1
		}
	case "backup":
		if err := runBackup(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "backup: %v\n", err)
			return 1
		}
	case "admin":
		if err := runAdmin(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "admin: %v\n", err)
			return 1
		}
	case "show":
		if err := runShow(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "show: %v\n", err)
			return 1
		}
	case "mark":
		if err := runMark(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "mark: %v\n", err)
			return 1
		}
	case "new":
		if err := runNewSkill(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "new: %v\n", err)
			return 1
		}
	case "templates":
		if err := runTemplates(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "templates: %v\n", err)
			return 1
		}
	case "lint":
		if err := runLint(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "lint: %v\n", err)
			return 1
		}
	case "setup":
		if err := runSetup(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "setup: %v\n", err)
			return 1
		}
	case "install":
		if err := runInstall(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "install: %v\n", err)
			return 1
		}
	case "install-defaults":
		if err := runInstallDefaults(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "install-defaults: %v\n", err)
			return 1
		}
	case "install-pinned":
		if err := runInstallPinned(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "install-pinned: %v\n", err)
			return 1
		}
	case "sets":
		if err := runSets(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "sets: %v\n", err)
			return 1
		}
	case "invites":
		if err := runInvites(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "invites: %v\n", err)
			return 1
		}
	case "plugins":
		if err := runPlugins(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "plugins: %v\n", err)
			return 1
		}
	case "plugin":
		if err := runPlugin(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "plugin: %v\n", err)
			return 1
		}
	case "install-set":
		if err := runInstallSet(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "install-set: %v\n", err)
			return 1
		}
	case "installed":
		if err := runInstalledArgs(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "installed: %v\n", err)
			return 1
		}
	case "sync":
		if err := runSync(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "sync: %v\n", err)
			return 1
		}
	case "uninstall":
		if err := runUninstall(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "uninstall: %v\n", err)
			return 1
		}
	case "doctor":
		if err := runDoctor(args[1:]...); err != nil {
			fmt.Fprintf(os.Stderr, "doctor: %v\n", err)
			return 1
		}
	case "version", "--version", "-v":
		fmt.Println(version)
	default:
		cliUsage()
		return 2
	}
	return 0
}

func RunDaemon(args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "serve":
			args = args[1:]
		case "version", "--version", "-v":
			fmt.Println(version)
			return 0
		case "help", "--help", "-h":
			daemonUsage()
			return 0
		default:
			if !strings.HasPrefix(args[0], "-") {
				daemonUsage()
				return 2
			}
		}
	}
	if err := runServeCommand(args); err != nil {
		fmt.Fprintf(os.Stderr, "skillsdexd: %v\n", err)
		return 1
	}
	return 0
}

type stringListFlag []string

func (f *stringListFlag) String() string {
	return fmt.Sprint([]string(*f))
}

func (f *stringListFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func runServeCommand(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	addr := fs.String("addr", "127.0.0.1:8765", "listen address")
	dbPath := fs.String("db", "", "SQLite database path")
	var trustedProxies stringListFlag
	fs.Var(&trustedProxies, "trusted-proxy", "trusted reverse proxy IP or CIDR for X-Forwarded-* headers; repeat or comma-separate")
	if err := fs.Parse(args); err != nil {
		return err
	}
	return runServe(*addr, *dbPath, trustedProxies)
}

func runDeprecatedServe(args []string) (int, error) {
	daemonPath, err := findSkillsDexDaemon()
	if err != nil {
		return 127, err
	}
	fmt.Fprintln(os.Stderr, "skillsdex serve is deprecated; use skillsdexd serve instead.")
	cmd := exec.Command(daemonPath, append([]string{"serve"}, args...)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return exitErr.ExitCode(), nil
		}
		return 1, err
	}
	return 0, nil
}

func findSkillsDexDaemon() (string, error) {
	if executable, err := os.Executable(); err == nil {
		candidate := filepath.Join(filepath.Dir(executable), "skillsdexd")
		if isExecutableFile(candidate) {
			return candidate, nil
		}
	}
	path, err := exec.LookPath("skillsdexd")
	if err != nil {
		return "", fmt.Errorf("skillsdexd not found; install the server binary and run `skillsdexd serve`")
	}
	return path, nil
}

func isExecutableFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir() && info.Mode()&0111 != 0
}

func cliUsage() {
	fmt.Fprintf(os.Stderr, `skillsdex %s

Usage:
  skillsdex login [--server URL] [--token TOKEN|--token-stdin]
  skillsdex logout
  skillsdex whoami
  skillsdex config get default_target
  skillsdex config set default_target codex
  skillsdex skills [--json]
  skillsdex search <query> [--limit N]
  skillsdex top [--limit N]
  skillsdex import-source <github-url|agentskill-url|@owner/slug> [--install] [--target codex] [--dry-run] [--yes|--no]
  skillsdex publish PATH [--slug SLUG] [--apply|--dry-run] [--strict] [--yes|--no]
  skillsdex refresh (--all | <owner>/<slug>) [--apply|--dry-run] [--strict] [--diff] [--json] [--yes|--no]
  skillsdex github export [--dir PATH] [--yes|--no]
  skillsdex backup export --out PATH
  skillsdex backup inspect --file PATH [--json]
  skillsdex backup restore --file PATH [--dry-run] [--json]
  skillsdex admin reset-password [--db PATH] [--username admin] [--password PASSWORD]
  skillsdex show <owner>/<slug> [--raw|--file PATH]
  skillsdex mark <owner>/<slug> [--pinned|--unpinned] [--recommended|--not-recommended] [--apply|--dry-run] [--yes|--no]
  skillsdex new PATH [--template workflow|review|research] [--slug SLUG] [--name NAME] [--with-folders] [--yes|--no]
  skillsdex templates
  skillsdex lint [--strict] [--json] (--all | --file PATH | <owner>/<slug>)
  skillsdex setup [--target codex] [--set SLUG|--pinned|--defaults] [--apply|--dry-run] [--json] [--yes|--no]
  skillsdex install <owner>/<slug> [--target codex] [--dry-run] [--yes|--no]
  skillsdex install-defaults [--target codex] [--dry-run] [--yes|--no]
  skillsdex install-pinned [--target codex] [--dry-run] [--yes|--no]
  skillsdex sets [set-slug] [--json]
  skillsdex install-set <set-slug> [--target codex] [--dry-run] [--yes|--no]
  skillsdex invites [--json]
  skillsdex invites create [--email EMAIL] [--json]
  skillsdex invites revoke <invite-id>
  skillsdex plugins [plugin-slug] [--target TARGET] [--json]
  skillsdex plugin add <slug> --name NAME --url URL --command COMMAND [--target TARGET] [--description TEXT]
  skillsdex plugin install <slug> [--dry-run] [--yes|--no]
  skillsdex installed [--strict] [--refresh] [--json]
  skillsdex sync [--dry-run] [--strict] [--json] [--yes|--no]
  skillsdex uninstall <owner>/<slug> [--target codex] [--yes|--no]
  skillsdex doctor [--strict] [--refresh] [--fix-permissions] [--json]

Server:
  skillsdexd serve [--addr ADDR] [--db PATH] [--trusted-proxy IP_OR_CIDR]
  skillsdex serve is deprecated and delegates to skillsdexd when available.
`, version)
}

func daemonUsage() {
	fmt.Fprintf(os.Stderr, `skillsdexd %s

Usage:
  skillsdexd serve [--addr ADDR] [--db PATH] [--trusted-proxy IP_OR_CIDR]
  skillsdexd [--addr ADDR] [--db PATH] [--trusted-proxy IP_OR_CIDR]
`, version)
}
