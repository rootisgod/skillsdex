package skillsdex

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

func detectedInstructionTargets(home string) (map[string]string, error) {
	targets := map[string]string{}
	for _, agent := range []string{"codex", "claude", "gemini"} {
		if _, err := exec.LookPath(agent); err != nil {
			continue
		}
		path, err := instructionPath(home, agent, os.Getenv("CODEX_HOME"))
		if err != nil {
			return nil, err
		}
		targets[agent] = path
	}
	return targets, nil
}

func runAgentSetup(args []string) error {
	if !isTerminal(os.Stdin) {
		return errors.New("guided setup needs an interactive terminal; use agent connect and agent sync for scripts")
	}
	return runAgentSetupFlow(args, func(prompt string) error {
		return requireConfirmation(prompt, confirmOptions{})
	})
}

func runAgentSetupFlow(args []string, confirm func(string) error) error {
	fs := flag.NewFlagSet("agent setup", flag.ContinueOnError)
	server := fs.String("server", "", "SkillsDex server URL")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: skillsdex agent setup --server URL")
	}
	dir, err := skillsdexDir()
	if err != nil {
		return err
	}
	connectionPath := filepath.Join(dir, "device.json")
	if _, err := os.Stat(connectionPath); errors.Is(err, os.ErrNotExist) {
		if err := validateDeviceServer(*server); err != nil {
			return err
		}
		fmt.Println("1. Connect this machine\nOpen Machines in your dashboard and choose Connect a machine.")
		if err := runAgentCommand([]string{"connect", "--server", *server}); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if err := rejectInstructionLinks(connectionPath); err != nil {
		return err
	}
	data, err := os.ReadFile(connectionPath)
	if err != nil {
		return err
	}
	data, err = unprotectDeviceConnection(data)
	if err != nil {
		return err
	}
	var connection deviceConnection
	if err := json.Unmarshal(data, &connection); err != nil {
		return err
	}
	if err := validateDeviceServer(connection.Server); err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	targets, err := detectedInstructionTargets(home)
	if err != nil {
		return err
	}
	fmt.Println("\n2. Review your agents")
	if len(targets) == 0 {
		return errors.New("no supported agents found on PATH; install Codex, Claude Code or Gemini CLI, then run setup again")
	}
	for _, agent := range []string{"codex", "claude", "gemini"} {
		if path, ok := targets[agent]; ok {
			fmt.Printf("  %s — %s\n", agent, path)
		}
	}
	client := instructionSyncClient{Base: connection.Server, Credential: connection.Credential, HTTP: deviceHTTPClient()}
	response, err := client.request(context.Background(), "GET", "/api/v1/sync/manifest", nil, "")
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("could not preview instructions: server returned %d", response.StatusCode)
	}
	var manifest InstructionManifest
	if err := json.NewDecoder(io.LimitReader(response.Body, 512*1024)).Decode(&manifest); err != nil {
		return err
	}
	if manifest.Revision < 1 {
		return errors.New("save your first instructions in the dashboard, then run setup again")
	}
	if manifest.Hash != sha256String([]byte(manifest.Body)) || len(manifest.Body) > 65536 {
		return errors.New("invalid shared instructions")
	}
	fmt.Printf("\n3. Review shared instructions — version %d\n\n%s\n", manifest.Revision, manifest.Body)
	if err := confirm("Use these shared instructions for the detected agents? Existing local changes will be preserved."); err != nil {
		return err
	}
	if err := runAgentCommandWithRevision([]string{"sync"}, manifest.Revision); err != nil {
		if errors.Is(err, errLocalInstructionConflict) {
			fmt.Println("\nSome agents need a choice. Run skillsdex agent resolve AGENT to compare local and shared instructions, then choose --keep-mine or --use-shared and run setup again.")
		}
		return err
	}
	fmt.Println("\n4. Keep this machine up to date")
	if err := confirm("Start SkillsDex automatically when you sign in?"); err == nil {
		if err := installAgentAutostart(); err != nil {
			return fmt.Errorf("instructions are ready, but automatic startup needs attention: %w", err)
		}
		fmt.Println("\nYou're ready. Your instructions will stay up to date automatically.")
	} else {
		fmt.Println("\nYour instructions are ready. Enable background updates later with: skillsdex agent autostart")
	}
	fmt.Println("Start new agent sessions to use the updated files.")
	return nil
}
