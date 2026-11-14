package skillsdex

import (
	"errors"
	"fmt"
	"io"
	"os"
)

func resolveInstructionCommand(args []string, home string, state *instructionClientState, persist func() error) error {
	if len(args) < 1 || len(args) > 2 {
		return errors.New("usage: skillsdex agent resolve AGENT [--keep-mine|--use-shared]")
	}
	path, err := instructionPath(home, args[0], os.Getenv("CODEX_HOME"))
	if err != nil {
		return err
	}
	if state.Manifest.Revision < 1 || state.Manifest.Hash != sha256String([]byte(state.Manifest.Body)) {
		return errors.New("run agent sync first to download valid shared instructions")
	}
	if err := rejectInstructionLinks(path); err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var local []byte
	if file != nil {
		local, err = io.ReadAll(io.LimitReader(file, 65537))
		file.Close()
		if err != nil {
			return err
		}
	}
	if len(local) > 65536 {
		return errors.New("local instructions exceed 64 KiB; review this file manually")
	}
	fmt.Printf("%s\n\nYOUR LOCAL INSTRUCTIONS\n%s\n\nSHARED INSTRUCTIONS (version %d)\n%s\n", path, local, state.Manifest.Revision, state.Manifest.Body)
	if len(args) == 1 {
		fmt.Printf("\nKeep mine: skillsdex agent resolve %s --keep-mine\nUse shared: skillsdex agent resolve %s --use-shared\n", args[0], args[0])
		return nil
	}
	if err := resolveInstructionChoice(state, args[0], path, string(local), args[1]); err != nil {
		return err
	}
	if err := persist(); err != nil {
		return err
	}
	fmt.Println("Choice saved. Run agent sync or restart the watcher to report the result.")
	return nil
}

func resolveInstructionChoice(state *instructionClientState, agent, path, reviewed, choice string) error {
	if state.Applied == nil {
		state.Applied = map[string]instructionApplied{}
	}
	switch choice {
	case "--keep-mine":
		prior := state.Applied[agent]
		prior.Path = path
		prior.KeepLocal = true
		state.Applied[agent] = prior
		return nil
	case "--use-shared":
		if state.Manifest.Revision < 1 || state.Manifest.Hash != sha256String([]byte(state.Manifest.Body)) {
			return errors.New("invalid shared instructions")
		}
		baseline := sha256String([]byte(reviewed))
		if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) && reviewed == "" {
			baseline = ""
		}
		if err := applyInstructionFile(path, baseline, []byte(state.Manifest.Body)); err != nil {
			return err
		}
		state.Applied[agent] = instructionApplied{Path: path, Hash: state.Manifest.Hash, Revision: state.Manifest.Revision}
		return nil
	default:
		return errors.New("choose --keep-mine or --use-shared")
	}
}
