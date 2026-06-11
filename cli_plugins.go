package skillsdex

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"text/tabwriter"
)

func runPlugins(args []string) error {
	opts, err := parsePluginsArgs(args)
	if err != nil {
		return errors.New("usage: skillsdex plugins [plugin-slug] [--target TARGET] [--json]")
	}
	client, err := authenticatedClient()
	if err != nil {
		return err
	}
	if opts.Slug != "" {
		resp, err := client.fetchPlugin(opts.Slug)
		if err != nil {
			return err
		}
		if opts.JSON {
			return printJSON(resp)
		}
		printPlugin(resp.Plugin)
		return nil
	}
	var resp pluginsResponse
	path := "/v1/plugins"
	if opts.Target != "" {
		path += "?target=" + url.QueryEscape(opts.Target)
	}
	if err := client.getJSON(path, &resp); err != nil {
		return err
	}
	if opts.JSON {
		return printJSON(resp)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "SLUG\tNAME\tTARGET\tSOURCE\tCOMMAND")
	for _, plugin := range resp.Plugins {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", plugin.Slug, plugin.Name, plugin.Target, plugin.SourceURL, plugin.InstallCommand)
	}
	return tw.Flush()
}

func runPlugin(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: skillsdex plugin (add|install) ...")
	}
	switch args[0] {
	case "add":
		opts, err := parsePluginAddArgs(args[1:])
		if err != nil {
			return errors.New("usage: skillsdex plugin add <slug> --name NAME --url URL --command COMMAND [--target TARGET] [--description TEXT]")
		}
		client, err := authenticatedClient()
		if err != nil {
			return err
		}
		var resp pluginResponse
		if err := client.postJSON("/v1/plugins", opts.Input, &resp); err != nil {
			return err
		}
		fmt.Printf("Tracked plugin %s: %s\n", resp.Plugin.Slug, resp.Plugin.Name)
		if resp.Plugin.Target != "" {
			fmt.Printf("Target: %s\n", resp.Plugin.Target)
		}
		fmt.Printf("Install command: %s\n", resp.Plugin.InstallCommand)
		return nil
	case "install":
		opts, err := parsePluginInstallArgs(args[1:])
		if err != nil {
			return errors.New("usage: skillsdex plugin install <slug> [--dry-run] [--yes|--no]")
		}
		client, err := authenticatedClient()
		if err != nil {
			return err
		}
		resp, err := client.fetchPlugin(opts.Slug)
		if err != nil {
			return err
		}
		return runTrackedPluginInstall(resp.Plugin, opts.Confirm)
	default:
		return errors.New("usage: skillsdex plugin (add|install) ...")
	}
}

func printPlugin(plugin PluginInstall) {
	fmt.Printf("Plugin: %s\n", plugin.Slug)
	fmt.Printf("Name: %s\n", plugin.Name)
	if plugin.Target != "" {
		fmt.Printf("Target: %s\n", plugin.Target)
	}
	if plugin.Description != "" {
		fmt.Printf("Description: %s\n", plugin.Description)
	}
	fmt.Printf("Source: %s\n", plugin.SourceURL)
	fmt.Printf("Install command: %s\n", plugin.InstallCommand)
}

func runTrackedPluginInstall(plugin PluginInstall, confirm confirmOptions) error {
	argv, err := parsePluginInstallCommand(plugin.InstallCommand)
	if err != nil {
		return err
	}
	fmt.Printf("Plugin: %s\n", plugin.Slug)
	fmt.Printf("Command: %s\n", shellJoin(argv))
	if confirm.DryRun {
		fmt.Println("Mode: dry run")
		return nil
	}
	if err := requireConfirmation("Run plugin install command for "+plugin.Slug+"?", confirm); err != nil {
		return err
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
