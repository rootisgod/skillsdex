package skillsdex

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"golang.org/x/term"
	"os"
	"path/filepath"
	"strings"
)

func runAdmin(args []string) error {
	if len(args) == 0 || args[0] != "reset-password" {
		return errors.New("usage: skillsdex admin reset-password [--db PATH] [--username admin] [--password PASSWORD]")
	}
	fs := flag.NewFlagSet("admin reset-password", flag.ExitOnError)
	dbPath := fs.String("db", "", "SQLite database path")
	username := fs.String("username", "admin", "admin username")
	passwordFlag := fs.String("password", "", "new password")
	_ = fs.Parse(args[1:])
	if fs.NArg() != 0 {
		return errors.New("usage: skillsdex admin reset-password [--db PATH] [--username admin] [--password PASSWORD]")
	}
	password, err := passwordFromFlagOrPrompt(*passwordFlag)
	if err != nil {
		return err
	}
	if strings.TrimSpace(*dbPath) == "" {
		*dbPath, err = defaultDBPath()
		if err != nil {
			return err
		}
	}
	db, err := openDB(*dbPath)
	if err != nil {
		return dbPathError(*dbPath, err)
	}
	defer db.Close()
	ctx := context.Background()
	u, err := getUserByUsername(ctx, db, strings.TrimSpace(*username))
	if err != nil {
		return err
	}
	if !u.IsAdmin {
		return fmt.Errorf("%s is not an admin user", u.Username)
	}
	if err := setUserPassword(ctx, db, u.ID, password); err != nil {
		return err
	}
	if err := revokeUserAccess(ctx, db, u.ID); err != nil {
		return err
	}
	_ = createAuditEvent(ctx, db, AuditEvent{
		Action:        "password_reset_local",
		TargetType:    "user",
		TargetID:      u.ID,
		TargetLabel:   u.Username,
		ActorUsername: "local-cli",
		DetailJSON:    `{"sessions_revoked":true,"api_tokens_revoked":true}`,
	})
	fmt.Printf("Reset password for %s and revoked existing sessions/API tokens.\n", u.Username)
	return nil
}

func defaultDBPath() (string, error) {
	dir, err := skillsdexDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "skillsdex.db"), nil
}

func passwordFromFlagOrPrompt(password string) (string, error) {
	password = strings.TrimSpace(password)
	if password != "" {
		return password, nil
	}
	if !isTerminal(os.Stdin) {
		return "", errors.New("password is required in non-interactive mode")
	}
	fmt.Print("New password: ")
	first, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", err
	}
	fmt.Print("Confirm password: ")
	second, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", err
	}
	if string(first) != string(second) {
		return "", errors.New("passwords do not match")
	}
	return strings.TrimSpace(string(first)), nil
}
