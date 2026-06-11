package skillsdex

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"golang.org/x/term"
	"io"
	"os"
	"strings"
)

func confirmFileOverwrite(path string, next []byte, confirm confirmOptions) error {
	current, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if bytes.Equal(current, next) {
		return nil
	}
	showOverwriteDiff(path, current, next)
	return requireConfirmation(fmt.Sprintf("%s already exists. Overwrite?", path), confirm)
}

func requireConfirmation(prompt string, confirm confirmOptions) error {
	if confirm.Yes && confirm.No {
		return errors.New("--yes and --no cannot be used together")
	}
	if confirm.Yes {
		return nil
	}
	if confirm.No {
		return errors.New("declined by --no")
	}
	if !isTerminal(os.Stdin) {
		return errors.New("refusing to overwrite in non-interactive mode; rerun with --yes to allow or --no to skip")
	}
	fmt.Printf("%s [y/N] ", prompt)
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	if ans != "y" && ans != "yes" {
		return errors.New("declined")
	}
	return nil
}

func isTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}
