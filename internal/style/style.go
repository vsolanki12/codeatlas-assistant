package style

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var filePathPattern = regexp.MustCompile(`[a-zA-Z0-9_/.-]+\.go:\d+`)

func LoadReference(styleFile, atlasData, graphPath string) string {
	if styleFile != "" {
		return readFile(styleFile)
	}

	repoRoot := DetectRepoRoot(graphPath)
	if repoRoot == "" {
		return ""
	}

	goFile := extractGoFilePath(atlasData)
	if goFile == "" {
		return ""
	}

	fullPath := filepath.Join(repoRoot, goFile)
	return readFile(fullPath)
}

func DetectRepoRoot(graphPath string) string {
	f, err := os.Open(graphPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	buf := make([]byte, 4096)
	n, _ := f.Read(buf)
	header := string(buf[:n])

	for _, prefix := range []string{`"repository":"`, `"repository": "`} {
		idx := strings.Index(header, prefix)
		if idx == -1 {
			continue
		}
		start := idx + len(prefix)
		end := strings.Index(header[start:], `"`)
		if end == -1 {
			continue
		}
		repoPath := header[start : start+end]
		if _, err := os.Stat(repoPath); err == nil {
			return repoPath
		}
	}
	return ""
}

func extractGoFilePath(atlasData string) string {
	matches := filePathPattern.FindAllString(atlasData, -1)
	for _, m := range matches {
		parts := strings.SplitN(m, ":", 2)
		path := parts[0]
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		if strings.Contains(path, "controller") || strings.Contains(path, "reconcil") {
			return path
		}
	}
	if len(matches) > 0 {
		return strings.SplitN(matches[0], ":", 2)[0]
	}
	return ""
}

func readFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "style file: %v (skipping)\n", err)
		return ""
	}

	content := string(data)

	lines := strings.Split(content, "\n")
	if len(lines) > 150 {
		lines = lines[:150]
		content = strings.Join(lines, "\n") + "\n// ... (truncated at 150 lines)\n"
	}

	fmt.Fprintf(os.Stderr, "style reference: %s (%d lines)\n", path, len(lines))
	return content
}
