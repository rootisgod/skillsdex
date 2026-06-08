package main

import (
	"flag"
	"fmt"
	"os"
)

var version = "0.1.0-phase1"

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "serve":
		fs := flag.NewFlagSet("serve", flag.ExitOnError)
		addr := fs.String("addr", "0.0.0.0:8765", "listen address")
		dbPath := fs.String("db", "", "SQLite database path")
		_ = fs.Parse(os.Args[2:])
		if err := runServe(*addr, *dbPath); err != nil {
			fmt.Fprintf(os.Stderr, "serve: %v\n", err)
			os.Exit(1)
		}
	case "login":
		if err := runLogin(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "login: %v\n", err)
			os.Exit(1)
		}
	case "logout":
		if err := runLogout(); err != nil {
			fmt.Fprintf(os.Stderr, "logout: %v\n", err)
			os.Exit(1)
		}
	case "whoami":
		if err := runWhoami(); err != nil {
			fmt.Fprintf(os.Stderr, "whoami: %v\n", err)
			os.Exit(1)
		}
	case "config":
		if err := runConfig(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "config: %v\n", err)
			os.Exit(1)
		}
	case "skills":
		if err := runSkills(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "skills: %v\n", err)
			os.Exit(1)
		}
	case "search":
		if err := runPublicSearch(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "search: %v\n", err)
			os.Exit(1)
		}
	case "top":
		if err := runPublicTop(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "top: %v\n", err)
			os.Exit(1)
		}
	case "import-source":
		if err := runImportSource(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "import-source: %v\n", err)
			os.Exit(1)
		}
	case "publish":
		if err := runPublish(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "publish: %v\n", err)
			os.Exit(1)
		}
	case "refresh":
		if err := runRefresh(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "refresh: %v\n", err)
			os.Exit(1)
		}
	case "github":
		if err := runGitHub(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "github: %v\n", err)
			os.Exit(1)
		}
	case "backup":
		if err := runBackup(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "backup: %v\n", err)
			os.Exit(1)
		}
	case "admin":
		if err := runAdmin(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "admin: %v\n", err)
			os.Exit(1)
		}
	case "show":
		if err := runShow(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "show: %v\n", err)
			os.Exit(1)
		}
	case "mark":
		if err := runMark(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "mark: %v\n", err)
			os.Exit(1)
		}
	case "new":
		if err := runNewSkill(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "new: %v\n", err)
			os.Exit(1)
		}
	case "templates":
		if err := runTemplates(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "templates: %v\n", err)
			os.Exit(1)
		}
	case "lint":
		if err := runLint(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "lint: %v\n", err)
			os.Exit(1)
		}
	case "setup":
		if err := runSetup(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "setup: %v\n", err)
			os.Exit(1)
		}
	case "install":
		if err := runInstall(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "install: %v\n", err)
			os.Exit(1)
		}
	case "install-defaults":
		if err := runInstallDefaults(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "install-defaults: %v\n", err)
			os.Exit(1)
		}
	case "install-pinned":
		if err := runInstallPinned(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "install-pinned: %v\n", err)
			os.Exit(1)
		}
	case "sets":
		if err := runSets(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "sets: %v\n", err)
			os.Exit(1)
		}
	case "install-set":
		if err := runInstallSet(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "install-set: %v\n", err)
			os.Exit(1)
		}
	case "installed":
		if err := runInstalledArgs(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "installed: %v\n", err)
			os.Exit(1)
		}
	case "sync":
		if err := runSync(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "sync: %v\n", err)
			os.Exit(1)
		}
	case "uninstall":
		if err := runUninstall(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "uninstall: %v\n", err)
			os.Exit(1)
		}
	case "doctor":
		if err := runDoctor(os.Args[2:]...); err != nil {
			fmt.Fprintf(os.Stderr, "doctor: %v\n", err)
			os.Exit(1)
		}
	case "version", "--version", "-v":
		fmt.Println(version)
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `skillsdex %s

Usage:
  skillsdex serve
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
  skillsdex installed [--strict] [--refresh] [--json]
  skillsdex sync [--dry-run] [--strict] [--json] [--yes|--no]
  skillsdex uninstall <owner>/<slug> [--target codex] [--yes|--no]
  skillsdex doctor [--strict] [--refresh] [--fix-permissions] [--json]
`, version)
}
