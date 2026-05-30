package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"

	"golang.org/x/term"
)

type apiClient struct {
	base  string
	token string
	http  *http.Client
}

type skillResponse struct {
	Skill Skill  `json:"skill"`
	Raw   string `json:"raw"`
}

func runLogin(args []string) error {
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	server := fs.String("server", "http://127.0.0.1:8765", "SkillsDex server URL")
	tokenFlag := fs.String("token", "", "API token")
	_ = fs.Parse(args)

	token := strings.TrimSpace(*tokenFlag)
	if token == "" {
		fmt.Print("API token: ")
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		token = strings.TrimSpace(line)
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

func runSkills() error {
	client, err := authenticatedClient()
	if err != nil {
		return err
	}
	var resp struct {
		Skills []Skill `json:"skills"`
	}
	if err := client.getJSON("/v1/skills", &resp); err != nil {
		return err
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "IDENTIFIER\tNAME\tREVISION\tUPDATED")
	for _, s := range resp.Skills {
		fmt.Fprintf(tw, "%s/%s\t%s\t%d\t%s\n", s.OwnerSlug, s.Slug, s.Name, s.RevisionNumber, s.UpdatedAt.Format("2006-01-02 15:04"))
	}
	return tw.Flush()
}

func runShow(args []string) error {
	identifier, raw, err := parseShowArgs(args)
	if err != nil {
		return errors.New("usage: skillsdex show <owner>/<slug> [--raw]")
	}
	owner, slug, err := parseIdentifier(identifier)
	if err != nil {
		return err
	}
	client, err := authenticatedClient()
	if err != nil {
		return err
	}
	resp, err := client.fetchSkill(owner, slug)
	if err != nil {
		return err
	}
	if raw {
		fmt.Print(resp.Raw)
		return nil
	}
	s := resp.Skill
	fmt.Printf("Identifier: %s/%s\nName: %s\nDescription: %s\nShort description: %s\nRevision: %d\nUpdated: %s\n",
		s.OwnerSlug, s.Slug, s.Name, s.Description, s.ShortDescription, s.RevisionNumber, s.UpdatedAt.Format("2006-01-02 15:04"))
	if s.ArchivedAt != "" {
		fmt.Printf("Archived: %s\n", s.ArchivedAt)
	}
	return nil
}

func runInstall(args []string) error {
	identifier, target, err := parseTargetArgs(args)
	if err != nil {
		return errors.New("usage: skillsdex install <owner>/<slug> [--target codex]")
	}
	owner, slug, err := parseIdentifier(identifier)
	if err != nil {
		return err
	}
	if err := validateSlug("owner", owner); err != nil {
		return err
	}
	if err := validateSlug("slug", slug); err != nil {
		return err
	}
	client, err := authenticatedClient()
	if err != nil {
		return err
	}
	resp, err := client.fetchSkill(owner, slug)
	if err != nil {
		return err
	}
	if resp.Skill.ArchivedAt != "" {
		return errors.New("remote skill is archived")
	}
	content := []byte(resp.Raw)
	path, err := targetPath(target, owner, slug)
	if err != nil {
		return err
	}
	manifest, err := loadManifest()
	if err != nil {
		return err
	}
	_, existing := manifest.find(owner, slug, target)
	if err := confirmOverwriteIfNeeded(path, existing); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err := os.WriteFile(path, content, 0600); err != nil {
		return err
	}
	manifest.upsert(newInstall(owner, slug, target, path, resp.Skill.CurrentRevisionID, content))
	if err := saveManifest(manifest); err != nil {
		return err
	}
	fmt.Printf("Installed %s/%s to %s\n", owner, slug, path)
	return nil
}

func runInstalled() error {
	manifest, err := loadManifest()
	if err != nil {
		return err
	}
	var client *apiClient
	if c, err := authenticatedClient(); err == nil {
		client = c
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "IDENTIFIER\tTARGET\tPATH\tREVISION\tSTATUS")
	for _, in := range manifest.Installs {
		var remote *Skill
		var remoteErr error
		if client != nil {
			resp, err := client.fetchSkill(in.Owner, in.Slug)
			if err != nil {
				remoteErr = err
			} else {
				remote = &resp.Skill
			}
		}
		fmt.Fprintf(tw, "%s/%s\t%s\t%s\t%s\t%s\n", in.Owner, in.Slug, in.Target, in.Path, in.RemoteRevision, installStatus(in, remote, remoteErr))
	}
	return tw.Flush()
}

func runSync() error {
	manifest, err := loadManifest()
	if err != nil {
		return err
	}
	client, err := authenticatedClient()
	if err != nil {
		return err
	}
	sort.Slice(manifest.Installs, func(i, j int) bool {
		return manifest.Installs[i].Owner+"/"+manifest.Installs[i].Slug < manifest.Installs[j].Owner+"/"+manifest.Installs[j].Slug
	})
	changed := false
	for _, in := range manifest.Installs {
		id := in.Owner + "/" + in.Slug
		resp, err := client.fetchSkill(in.Owner, in.Slug)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: remote-deleted or unavailable; leaving local file in place\n", id)
			continue
		}
		if resp.Skill.ArchivedAt != "" {
			fmt.Fprintf(os.Stderr, "%s: remote archived; leaving local file in place\n", id)
			continue
		}
		hash, err := fileHash(in.Path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(os.Stderr, "%s: cannot read local file: %v\n", id, err)
			continue
		}
		if err == nil && resp.Skill.CurrentRevisionID == in.RemoteRevision {
			fmt.Printf("%s: clean\n", id)
			continue
		}
		if err == nil && hash != in.InstalledHash {
			if err := requireConfirmation(fmt.Sprintf("%s was edited locally. Overwrite with remote revision?", in.Path)); err != nil {
				fmt.Fprintf(os.Stderr, "%s: skipped: %v\n", id, err)
				continue
			}
		}
		content := []byte(resp.Raw)
		if err := os.MkdirAll(filepath.Dir(in.Path), 0700); err != nil {
			return err
		}
		if err := os.WriteFile(in.Path, content, 0600); err != nil {
			return err
		}
		manifest.upsert(newInstall(in.Owner, in.Slug, in.Target, in.Path, resp.Skill.CurrentRevisionID, content))
		changed = true
		fmt.Printf("%s: updated to revision %d\n", id, resp.Skill.RevisionNumber)
	}
	if changed {
		return saveManifest(manifest)
	}
	return nil
}

func runUninstall(args []string) error {
	identifier, target, err := parseTargetArgs(args)
	if err != nil {
		return errors.New("usage: skillsdex uninstall <owner>/<slug> [--target codex]")
	}
	owner, slug, err := parseIdentifier(identifier)
	if err != nil {
		return err
	}
	manifest, err := loadManifest()
	if err != nil {
		return err
	}
	_, in := manifest.find(owner, slug, target)
	if in == nil {
		return errors.New("install is not managed by SkillsDex")
	}
	hash, err := fileHash(in.Path)
	if err == nil && hash != in.InstalledHash {
		if err := requireConfirmation(fmt.Sprintf("%s was edited locally. Remove it?", in.Path)); err != nil {
			return err
		}
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		if err := os.Remove(in.Path); err != nil {
			return err
		}
		_ = os.Remove(filepath.Dir(in.Path))
	}
	manifest.remove(owner, slug, target)
	if err := saveManifest(manifest); err != nil {
		return err
	}
	fmt.Printf("Uninstalled %s/%s\n", owner, slug)
	return nil
}

func runDoctor() error {
	dir, _ := skillsdexDir()
	auth, authErr := loadAuth()
	manifest, _ := manifestPath()
	codexDir, codexErr := codexSkillsDir()
	fmt.Printf("SkillsDex directory: %s\n", dir)
	fmt.Printf("Manifest path: %s\n", manifest)
	fmt.Printf("Default target: codex\n")
	if authErr == nil {
		fmt.Printf("Auth: configured for %s\n", auth.ServerURL)
	} else {
		fmt.Printf("Auth: not configured (%v)\n", authErr)
	}
	if codexErr != nil {
		fmt.Printf("Codex skills directory: unavailable (%v)\n", codexErr)
		return nil
	}
	fmt.Printf("Codex skills directory: %s\n", codexDir)
	if err := os.MkdirAll(codexDir, 0700); err != nil {
		fmt.Printf("Codex skills directory writable: no (%v)\n", err)
		return nil
	}
	test := filepath.Join(codexDir, ".skillsdex-write-test")
	if err := os.WriteFile(test, []byte("ok"), 0600); err != nil {
		fmt.Printf("Codex skills directory writable: no (%v)\n", err)
	} else {
		_ = os.Remove(test)
		fmt.Println("Codex skills directory writable: yes")
	}
	return nil
}

func authenticatedClient() (*apiClient, error) {
	cfg, err := loadAuth()
	if err != nil {
		return nil, err
	}
	return newAPIClient(cfg), nil
}

func newAPIClient(cfg AuthConfig) *apiClient {
	return &apiClient{base: strings.TrimRight(cfg.ServerURL, "/"), token: cfg.Token, http: http.DefaultClient}
}

func (c *apiClient) fetchSkill(owner, slug string) (skillResponse, error) {
	var resp skillResponse
	err := c.getJSON("/v1/skills/"+owner+"/"+slug, &resp)
	return resp, err
}

func (c *apiClient) getJSON(path string, out any) error {
	req, err := http.NewRequest(http.MethodGet, c.base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("GET %s: %s: %s", path, res.Status, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func (c *apiClient) putJSON(path string, in, out any) error {
	body, _ := json.Marshal(in)
	req, err := http.NewRequest(http.MethodPut, c.base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("PUT %s: %s: %s", path, res.Status, strings.TrimSpace(string(b)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func confirmOverwriteIfNeeded(path string, managed *Install) error {
	_, statErr := os.Stat(path)
	if errors.Is(statErr, os.ErrNotExist) {
		return nil
	}
	if statErr != nil {
		return statErr
	}
	if managed == nil {
		return requireConfirmation(fmt.Sprintf("%s exists but is not managed by SkillsDex. Overwrite?", path))
	}
	hash, err := fileHash(path)
	if err != nil {
		return err
	}
	if hash != managed.InstalledHash {
		return requireConfirmation(fmt.Sprintf("%s was edited locally. Overwrite?", path))
	}
	return nil
}

func requireConfirmation(prompt string) error {
	if !isTerminal(os.Stdin) {
		return errors.New("refusing to overwrite in non-interactive mode")
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

func parseShowArgs(args []string) (string, bool, error) {
	var identifier string
	raw := false
	for _, arg := range args {
		switch arg {
		case "--raw":
			raw = true
		default:
			if strings.HasPrefix(arg, "-") {
				return "", false, fmt.Errorf("unknown flag %s", arg)
			}
			if identifier != "" {
				return "", false, errors.New("too many arguments")
			}
			identifier = arg
		}
	}
	if identifier == "" {
		return "", false, errors.New("missing identifier")
	}
	return identifier, raw, nil
}

func parseTargetArgs(args []string) (string, string, error) {
	var identifier string
	target := "codex"
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--target" {
			if i+1 >= len(args) {
				return "", "", errors.New("missing target")
			}
			target = args[i+1]
			i++
			continue
		}
		if strings.HasPrefix(arg, "--target=") {
			target = strings.TrimPrefix(arg, "--target=")
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return "", "", fmt.Errorf("unknown flag %s", arg)
		}
		if identifier != "" {
			return "", "", errors.New("too many arguments")
		}
		identifier = arg
	}
	if identifier == "" {
		return "", "", errors.New("missing identifier")
	}
	return identifier, target, nil
}
