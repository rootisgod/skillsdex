package skillsdex

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

func runLogin(args []string) error {
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	server := fs.String("server", "http://127.0.0.1:8765", "SkillsDex server URL")
	tokenFlag := fs.String("token", "", "API token")
	tokenStdin := fs.Bool("token-stdin", false, "read API token from stdin")
	_ = fs.Parse(args)

	token := strings.TrimSpace(*tokenFlag)
	if token != "" && *tokenStdin {
		return errors.New("--token and --token-stdin cannot be used together")
	}
	if *tokenStdin {
		read, err := readTokenFromStdin()
		if err != nil {
			return err
		}
		token = read
	}
	if token == "" {
		fmt.Print("API token: ")
		read, err := readTokenFromStdin()
		if err != nil {
			return err
		}
		token = read
	}
	if token == "" {
		return errors.New("API token is required")
	}
	cfg := AuthConfig{ServerURL: strings.TrimRight(*server, "/"), Token: token}
	client := newAPIClient(cfg)
	var me map[string]any
	if err := client.getJSON("/v1/me", &me); err != nil {
		return fmt.Errorf("token check failed: %w", err)
	}
	if err := saveAuth(cfg); err != nil {
		return err
	}
	fmt.Printf("Logged in to %s as %s\n", cfg.ServerURL, me["username"])
	return nil
}

func tokenStdinLoginCommand() string {
	return `printf '%s\n' "$SKILLSDEX_TOKEN" | skillsdex login --server http://127.0.0.1:8765 --token-stdin`
}

func readTokenFromStdin() (string, error) {
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func runLogout() error {
	path, err := authPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	fmt.Println("Logged out")
	return nil
}

func runWhoami() error {
	client, err := authenticatedClient()
	if err != nil {
		return err
	}
	var me map[string]any
	if err := client.getJSON("/v1/me", &me); err != nil {
		return err
	}
	fmt.Printf("%s on %s\n", me["username"], client.base)
	return nil
}
