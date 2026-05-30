package main

import (
	"html"
	"strings"
)

func unifiedDiff(oldText, newText string) string {
	oldLines := strings.Split(strings.TrimRight(oldText, "\n"), "\n")
	newLines := strings.Split(strings.TrimRight(newText, "\n"), "\n")
	if len(oldLines) == 1 && oldLines[0] == "" {
		oldLines = nil
	}
	if len(newLines) == 1 && newLines[0] == "" {
		newLines = nil
	}
	n, m := len(oldLines), len(newLines)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if oldLines[i] == newLines[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}
	var out strings.Builder
	i, j := 0, 0
	for i < n && j < m {
		if oldLines[i] == newLines[j] {
			out.WriteString("  " + oldLines[i] + "\n")
			i++
			j++
		} else if dp[i+1][j] >= dp[i][j+1] {
			out.WriteString("- " + oldLines[i] + "\n")
			i++
		} else {
			out.WriteString("+ " + newLines[j] + "\n")
			j++
		}
	}
	for i < n {
		out.WriteString("- " + oldLines[i] + "\n")
		i++
	}
	for j < m {
		out.WriteString("+ " + newLines[j] + "\n")
		j++
	}
	return out.String()
}

func diffHTML(diff string) string {
	var out strings.Builder
	out.WriteString(`<pre class="diff">`)
	for _, line := range strings.Split(strings.TrimRight(diff, "\n"), "\n") {
		class := "ctx"
		if strings.HasPrefix(line, "+ ") {
			class = "add"
		} else if strings.HasPrefix(line, "- ") {
			class = "del"
		}
		out.WriteString(`<span class="` + class + `">` + html.EscapeString(line) + "</span>\n")
	}
	out.WriteString(`</pre>`)
	return out.String()
}
