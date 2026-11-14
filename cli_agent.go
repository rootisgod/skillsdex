package skillsdex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type deviceConnection struct {
	Server     string `json:"server"`
	Credential string `json:"credential"`
}

func deviceHTTPClient() *http.Client {
	return &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return errors.New("device API redirects are not followed")
	}}
}

func validateDeviceServer(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return errors.New("server must be an HTTPS origin")
	}
	if u.Scheme == "https" {
		return nil
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme == "http" && (u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())) {
		return nil
	}
	return errors.New("HTTPS is required except for loopback development servers")
}

func runAgentCommand(args []string) error {
	return runAgentCommandWithRevision(args, 0)
}

func runAgentCommandWithRevision(args []string, expectedRevision int) error {
	if len(args) == 0 {
		return errors.New("usage: skillsdex agent setup --server URL; skillsdex agent sync; skillsdex agent watch; skillsdex agent resolve AGENT [--keep-mine|--use-shared]; skillsdex agent autostart")
	}
	if args[0] == "setup" {
		return runAgentSetup(args[1:])
	}
	if args[0] == "autostart" {
		if len(args) != 1 {
			return errors.New("usage: skillsdex agent autostart")
		}
		if err := installAgentAutostart(); err != nil {
			return err
		}
		fmt.Println("Automatic updates are enabled for this user.")
		return nil
	}
	dir, err := skillsdexDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	connectionPath := filepath.Join(dir, "device.json")
	if args[0] == "watch" {
		if len(args) != 1 {
			return errors.New("agent watch does not accept arguments")
		}
		return runAgentWatcher(dir)
	}
	lock, err := acquireAgentLock(filepath.Join(dir, "agent-sync.lock"))
	if err != nil {
		return err
	}
	defer lock.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	if args[0] == "connect" {
		fs := flag.NewFlagSet("agent connect", flag.ContinueOnError)
		server := fs.String("server", "", "server origin")
		hostname, err := os.Hostname()
		if err != nil {
			return err
		}
		name := fs.String("name", hostname, "machine name")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if err := validateDeviceServer(*server); err != nil {
			return err
		}
		if strings.TrimSpace(*name) == "" {
			return errors.New("--name is required")
		}
		if _, err := os.Lstat(connectionPath); err == nil {
			return errors.New("this machine is already connected; revoke its access and remove device.json before reconnecting")
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		var code []byte
		if isTerminal(os.Stdin) {
			fmt.Print("Paste your connection code, then press Enter: ")
			line, readErr := bufio.NewReader(io.LimitReader(os.Stdin, 1024)).ReadString('\n')
			code = []byte(line)
			err = readErr
			if errors.Is(err, io.EOF) {
				err = nil
			}
		} else {
			code, err = io.ReadAll(io.LimitReader(os.Stdin, 1024))
		}
		if err != nil {
			return err
		}
		payload, err := json.Marshal(map[string]string{"enrolment": strings.TrimSpace(string(code)), "name": *name, "platform": runtime.GOOS})
		if err != nil {
			return err
		}
		client := instructionSyncClient{Base: *server, HTTP: deviceHTTPClient()}
		response, err := client.request(ctx, http.MethodPost, "/api/v1/devices/register", payload, "")
		if err != nil {
			return err
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusCreated {
			return fmt.Errorf("registration returned %d", response.StatusCode)
		}
		var result struct {
			Credential string `json:"credential"`
		}
		if err := json.NewDecoder(io.LimitReader(response.Body, 8192)).Decode(&result); err != nil {
			return err
		}
		if result.Credential == "" {
			return errors.New("registration returned an empty credential")
		}
		data, err := json.Marshal(deviceConnection{Server: *server, Credential: result.Credential})
		if err != nil {
			return err
		}
		data, err = protectDeviceConnection(data)
		if err != nil {
			return fmt.Errorf("protect device credential: %w", err)
		}
		if err := rejectInstructionLinks(connectionPath); err != nil {
			return err
		}
		file, err := os.OpenFile(connectionPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return err
		}
		if _, err := file.Write(data); err != nil {
			file.Close()
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		fmt.Println("Machine connected. Run skillsdex agent sync to apply instructions, or skillsdex agent watch to keep syncing.")
		return nil
	}
	if args[0] != "sync" && args[0] != "watch" && args[0] != "resolve" {
		return errors.New("unknown agent command")
	}
	if len(args) != 1 && args[0] != "resolve" {
		return errors.New("agent sync and watch do not accept arguments")
	}
	if err := rejectInstructionLinks(connectionPath); err != nil {
		return err
	}
	data, err := os.ReadFile(connectionPath)
	if err != nil {
		return fmt.Errorf("connect this machine first: %w", err)
	}
	var connection deviceConnection
	data, err = unprotectDeviceConnection(data)
	if err != nil {
		return fmt.Errorf("read protected device credential: %w", err)
	}
	if err := json.Unmarshal(data, &connection); err != nil {
		return err
	}
	if err := validateDeviceServer(connection.Server); err != nil {
		return err
	}
	client := instructionSyncClient{Base: connection.Server, Credential: connection.Credential, HTTP: deviceHTTPClient(), ExpectedRevision: expectedRevision}
	statePath := filepath.Join(dir, "instruction-sync.json")
	if err := rejectInstructionLinks(statePath); err != nil {
		return err
	}
	state := instructionClientState{}
	if data, err := os.ReadFile(statePath); err == nil {
		if err := json.Unmarshal(data, &state); err != nil {
			return fmt.Errorf("invalid sync state: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	persist := func() error { return saveInstructionClientState(statePath, state) }
	if args[0] == "resolve" {
		return resolveInstructionCommand(args[1:], home, &state, persist)
	}
	targets := map[string]string{}
	for _, agent := range []string{"codex", "claude", "gemini"} {
		if _, err := exec.LookPath(agent); err != nil {
			continue
		}
		path, err := instructionPath(home, agent, os.Getenv("CODEX_HOME"))
		if err != nil {
			return err
		}
		targets[agent] = path
	}
	err = client.syncOnce(ctx, &state, targets, persist)
	if err == nil {
		fmt.Printf("Instruction sync checked %d detected agents.\n", len(targets))
	}
	return err
}

func runAgentWatcher(dir string) error {
	watchLock, err := acquireAgentLock(filepath.Join(dir, "agent-watch.lock"))
	if err != nil {
		return err
	}
	defer watchLock.Close()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	delay := 45 * time.Second
	for {
		// Each pass reads fresh state under the sync lock. Local choices can be
		// made while the watcher sleeps and are respected by the next pass.
		err := runAgentCommand([]string{"sync"})
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "sync: %v\n", err)
			delay *= 2
			if delay > 5*time.Minute {
				delay = 5 * time.Minute
			}
		} else {
			delay = 45 * time.Second
		}
		timer := time.NewTimer(delay + time.Duration(rand.Intn(5000))*time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}
