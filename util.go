package skillsdex

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

var slugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

func newID(prefix string) string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(b)
}

func newToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return "sdx_" + hex.EncodeToString(b)
}

func nowUTC() time.Time {
	return time.Now().UTC().Truncate(time.Second)
}

func sha256String(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func apiTokenHash(token string) string {
	return sha256String([]byte(strings.TrimSpace(token)))
}

func sessionTokenHash(token string) string {
	return sha256String([]byte(strings.TrimSpace(token)))
}

func inviteTokenHash(token string) string {
	return sha256String([]byte(strings.TrimSpace(token)))
}

func tokenPreview(token string) string {
	token = strings.TrimSpace(token)
	if len(token) <= 18 {
		return token
	}
	return token[:12] + "..." + token[len(token)-6:]
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func nullableTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	t := parseTime(s)
	return &t
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func validateSlug(kind, value string) error {
	if !slugRE.MatchString(value) {
		return fmt.Errorf("%s must use lowercase letters, numbers, and dashes, and start with a letter or number", kind)
	}
	return nil
}

func validatePluginInstallInput(in PluginInstallInput) error {
	if err := validateSlug("plugin slug", strings.TrimSpace(in.Slug)); err != nil {
		return err
	}
	if strings.TrimSpace(in.Name) == "" {
		return errors.New("name is required")
	}
	if strings.TrimSpace(in.SourceURL) == "" {
		return errors.New("source URL is required")
	}
	parsed, err := url.Parse(strings.TrimSpace(in.SourceURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return errors.New("source URL must be an absolute URL")
	}
	if parsed.Scheme != "https" {
		return errors.New("source URL must use https")
	}
	if target := strings.TrimSpace(in.Target); target != "" {
		if err := validateSlug("plugin target", target); err != nil {
			return err
		}
	}
	if _, err := parsePluginInstallCommand(in.InstallCommand); err != nil {
		return err
	}
	return nil
}

func parsePluginInstallCommand(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("install command is required")
	}
	if len(raw) > 2048 {
		return nil, errors.New("install command is too long")
	}
	var args []string
	var b strings.Builder
	var quote rune
	escaped := false
	hadToken := false
	for _, r := range raw {
		if escaped {
			b.WriteRune(r)
			hadToken = true
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			hadToken = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
				hadToken = true
				continue
			}
			b.WriteRune(r)
			hadToken = true
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			hadToken = true
			continue
		}
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if hadToken {
				args = append(args, b.String())
				b.Reset()
				hadToken = false
			}
			continue
		}
		b.WriteRune(r)
		hadToken = true
	}
	if escaped {
		return nil, errors.New("install command has trailing escape")
	}
	if quote != 0 {
		return nil, errors.New("install command has unterminated quote")
	}
	if hadToken {
		args = append(args, b.String())
	}
	if len(args) == 0 || strings.TrimSpace(args[0]) == "" {
		return nil, errors.New("install command is required")
	}
	if isShellCommandName(args[0]) {
		return nil, errors.New("install command must not invoke a shell")
	}
	return args, nil
}

func isShellCommandName(command string) bool {
	command = strings.ToLower(strings.TrimSpace(command))
	command = path.Base(strings.ReplaceAll(command, "\\", "/"))
	switch command {
	case "sh", "bash", "zsh", "fish", "cmd", "cmd.exe", "powershell", "powershell.exe", "pwsh", "pwsh.exe":
		return true
	default:
		return false
	}
}

func parseTagList(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\t' || r == ' '
	})
	tags := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.ToLower(strings.TrimSpace(part))
		if part != "" {
			tags = append(tags, part)
		}
	}
	normalized, err := normalizeTags(tags)
	if err != nil {
		return tags
	}
	return normalized
}

func tagsText(tags []string) string {
	return strings.Join(tags, ", ")
}

func parseIdentifier(id string) (string, string, error) {
	parts := strings.Split(id, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("identifier must be owner/slug")
	}
	return parts[0], parts[1], nil
}

func validateSkillFilePath(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("file path is required")
	}
	if strings.Contains(value, "\\") || path.IsAbs(value) {
		return fmt.Errorf("file path %q must be relative", value)
	}
	clean := path.Clean(value)
	if clean != value || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return fmt.Errorf("file path %q must not contain traversal", value)
	}
	if strings.HasPrefix(clean, ".git/") || clean == ".git" {
		return fmt.Errorf("file path %q is not allowed", value)
	}
	return nil
}

func validateTextSkillFile(filePath, content string) error {
	if !isAllowedTextSkillFilePath(filePath) {
		return fmt.Errorf("%s must be a supported text file type", filePath)
	}
	if !utf8.ValidString(content) || strings.ContainsRune(content, '\x00') {
		return fmt.Errorf("%s must contain UTF-8 text", filePath)
	}
	return nil
}

func isAllowedTextSkillFilePath(filePath string) bool {
	if filePath == "SKILL.md" {
		return true
	}
	switch strings.ToLower(path.Ext(filePath)) {
	case ".md", ".markdown", ".txt", ".json", ".yaml", ".yml", ".toml", ".sh", ".py", ".js", ".ts", ".css", ".html":
		return true
	default:
		return false
	}
}

func renderSkillMarkdown(in SkillInput) string {
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("name: ")
	b.WriteString(yamlQuote(in.Name))
	b.WriteString("\n")
	b.WriteString("description: ")
	b.WriteString(yamlQuote(in.Description))
	b.WriteString("\n")
	if strings.TrimSpace(in.ShortDescription) != "" {
		b.WriteString("metadata:\n")
		b.WriteString("  short-description: ")
		b.WriteString(yamlQuote(in.ShortDescription))
		b.WriteString("\n")
	}
	b.WriteString("---\n\n")
	b.WriteString(strings.TrimRight(in.Body, "\r\n"))
	b.WriteString("\n")
	return b.String()
}

func yamlQuote(s string) string {
	return strconv.Quote(s)
}

func simpleMarkdownHTML(md string) string {
	var out strings.Builder
	inList := false
	lines := strings.Split(md, "\n")
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if trim == "" {
			if inList {
				out.WriteString("</ul>")
				inList = false
			}
			continue
		}
		if strings.HasPrefix(trim, "---") {
			continue
		}
		if strings.HasPrefix(trim, "### ") {
			if inList {
				out.WriteString("</ul>")
				inList = false
			}
			out.WriteString("<h3>" + html.EscapeString(strings.TrimSpace(trim[4:])) + "</h3>")
			continue
		}
		if strings.HasPrefix(trim, "## ") {
			if inList {
				out.WriteString("</ul>")
				inList = false
			}
			out.WriteString("<h2>" + html.EscapeString(strings.TrimSpace(trim[3:])) + "</h2>")
			continue
		}
		if strings.HasPrefix(trim, "# ") {
			if inList {
				out.WriteString("</ul>")
				inList = false
			}
			out.WriteString("<h1>" + html.EscapeString(strings.TrimSpace(trim[2:])) + "</h1>")
			continue
		}
		if strings.HasPrefix(trim, "- ") {
			if !inList {
				out.WriteString("<ul>")
				inList = true
			}
			out.WriteString("<li>" + inlineMarkdown(trim[2:]) + "</li>")
			continue
		}
		if inList {
			out.WriteString("</ul>")
			inList = false
		}
		out.WriteString("<p>" + inlineMarkdown(trim) + "</p>")
	}
	if inList {
		out.WriteString("</ul>")
	}
	return out.String()
}

func inlineMarkdown(s string) string {
	escaped := html.EscapeString(s)
	code := regexp.MustCompile("`([^`]+)`")
	return code.ReplaceAllString(escaped, "<code>$1</code>")
}
