package skillsdex

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf16"
)

//go:embed scripts/install-agent-task.ps1
var agentTaskInstaller string

func installAgentAutostart() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	stateDir, err := skillsdexDir()
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(stateDir, "device.json")); err != nil {
		return errors.New("connect this machine before enabling automatic startup")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	run := func(name string, args ...string) error {
		cmd := exec.CommandContext(ctx, name, args...)
		hideAgentCommand(cmd)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s: %w\n%s", name, err, output)
		}
		return nil
	}
	if runtime.GOOS == "windows" {
		script := "& {\n" + agentTaskInstaller + "\n} -Executable '" + strings.ReplaceAll(executable, "'", "''") + "'"
		units := utf16.Encode([]rune(script))
		encoded := make([]byte, len(units)*2)
		for i, u := range units {
			binary.LittleEndian.PutUint16(encoded[i*2:], u)
		}
		return run("powershell.exe", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-EncodedCommand", base64.StdEncoding.EncodeToString(encoded))
	}
	content, err := agentServiceDefinition(runtime.GOOS, executable, stateDir, os.Getenv("PATH"), os.Getenv("CODEX_HOME"))
	if err != nil {
		return err
	}
	var path string
	switch runtime.GOOS {
	case "linux":
		path = filepath.Join(home, ".config", "systemd", "user", "skillsdex-agent.service")
	case "darwin":
		path = filepath.Join(home, "Library", "LaunchAgents", "org.skillsdex.agent.plist")
	default:
		return errors.New("automatic startup is not supported on this platform")
	}
	if err := rejectInstructionLinks(path); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	if err := writeAgentService(path, content); err != nil {
		return err
	}
	if runtime.GOOS == "linux" {
		if err := run("systemctl", "--user", "daemon-reload"); err != nil {
			return err
		}
		return run("systemctl", "--user", "enable", "--now", "skillsdex-agent.service")
	}
	if exec.CommandContext(ctx, "launchctl", "print", fmt.Sprintf("gui/%d/org.skillsdex.agent", os.Getuid())).Run() == nil {
		return nil
	}
	return run("launchctl", "bootstrap", fmt.Sprintf("gui/%d", os.Getuid()), path)
}

// Allow retries of our exact installation, without replacing a customised service.
func writeAgentService(path, content string) error {
	if err := rejectInstructionLinks(path); err != nil {
		return err
	}
	existing, err := os.ReadFile(path)
	if err == nil {
		if string(existing) == content {
			return nil
		}
		return errors.New("service file differs from this setup; review it before reinstalling: " + path)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err := file.WriteString(content); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func agentServiceDefinition(platform, executable, stateDir, pathEnv, codexHome string) (string, error) {
	for _, v := range []string{executable, stateDir, pathEnv, codexHome} {
		if strings.ContainsAny(v, "\r\n\x00") {
			return "", errors.New("service settings must not contain line breaks or NUL")
		}
	}
	if !filepath.IsAbs(executable) || !filepath.IsAbs(stateDir) {
		return "", errors.New("service executable and state directory must be absolute")
	}
	switch platform {
	case "linux":
		quote := func(v string) string {
			return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, `%`, `%%`).Replace(v) + `"`
		}
		// systemd performs variable expansion in ExecStart; escape literal dollars.
		command := quote(strings.ReplaceAll(executable, "$", "$$"))
		return "# Managed by SkillsDex\n[Unit]\nDescription=SkillsDex agent sync\n[Service]\nType=simple\nExecStart=" + command + " agent watch\nRestart=on-failure\nRestartSec=15\nUMask=0077\nEnvironment=" + quote("SKILLSDEX_HOME="+stateDir) + "\nEnvironment=" + quote("PATH="+pathEnv) + "\nEnvironment=" + quote("CODEX_HOME="+codexHome) + "\n[Install]\nWantedBy=default.target\n", nil
	case "darwin":
		escape := func(v string) string { var b bytes.Buffer; xml.EscapeText(&b, []byte(v)); return b.String() }
		return `<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>Label</key><string>org.skillsdex.agent</string><key>ProgramArguments</key><array><string>` + escape(executable) + `</string><string>agent</string><string>watch</string></array><key>RunAtLoad</key><true/><key>KeepAlive</key><true/><key>ThrottleInterval</key><integer>15</integer><key>EnvironmentVariables</key><dict><key>SKILLSDEX_HOME</key><string>` + escape(stateDir) + `</string><key>PATH</key><string>` + escape(pathEnv) + `</string><key>CODEX_HOME</key><string>` + escape(codexHome) + `</string></dict></dict></plist>`, nil
	default:
		return "", errors.New("unsupported service platform")
	}
}
