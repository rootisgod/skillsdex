package skillsdex

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
)

type tuiOptions struct {
	NoAltScreen bool
}

func runTUI(args []string) error {
	opts, err := parseTUIArgs(args)
	if err != nil {
		if err == errTUIHelp {
			return nil
		}
		return err
	}
	model := newTUIModel(opts)
	programOptions := []tea.ProgramOption{}
	if !opts.NoAltScreen {
		programOptions = append(programOptions, tea.WithAltScreen())
	}
	_, err = tea.NewProgram(model, programOptions...).Run()
	return err
}

func parseTUIArgs(args []string) (tuiOptions, error) {
	var opts tuiOptions
	for _, arg := range args {
		switch arg {
		case "--no-alt-screen":
			opts.NoAltScreen = true
		case "--help", "-h":
			fmt.Fprintln(os.Stderr, "usage: skillsdex tui [--no-alt-screen]")
			return opts, errTUIHelp
		default:
			return opts, fmt.Errorf("unknown tui option %q", arg)
		}
	}
	return opts, nil
}

var errTUIHelp = fmt.Errorf("tui help requested")
