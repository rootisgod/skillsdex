package main

import (
	"flag"
	"fmt"
	"os"
)

const version = "0.1.0-phase1"

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
	case "skills":
		if err := runSkills(); err != nil {
			fmt.Fprintf(os.Stderr, "skills: %v\n", err)
			os.Exit(1)
		}
	case "show":
		if err := runShow(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "show: %v\n", err)
			os.Exit(1)
		}
	case "install":
		if err := runInstall(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "install: %v\n", err)
			os.Exit(1)
		}
	case "installed":
		if err := runInstalled(); err != nil {
			fmt.Fprintf(os.Stderr, "installed: %v\n", err)
			os.Exit(1)
		}
	case "sync":
		if err := runSync(); err != nil {
			fmt.Fprintf(os.Stderr, "sync: %v\n", err)
			os.Exit(1)
		}
	case "uninstall":
		if err := runUninstall(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "uninstall: %v\n", err)
			os.Exit(1)
		}
	case "doctor":
		if err := runDoctor(); err != nil {
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
  skillsdex login
  skillsdex logout
  skillsdex whoami
  skillsdex skills
  skillsdex show <owner>/<slug> [--raw]
  skillsdex install <owner>/<slug> [--target codex]
  skillsdex installed
  skillsdex sync
  skillsdex uninstall <owner>/<slug> [--target codex]
  skillsdex doctor
`, version)
}
