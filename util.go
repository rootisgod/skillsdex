package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"html"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
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
