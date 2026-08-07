package hosts

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"
	"strings"
)

const (
	beginMarker = "# dev-local:BEGIN"
	endMarker   = "# dev-local:END"
)

func FilePath() string {
	if runtime.GOOS == "windows" {
		return `C:\Windows\System32\drivers\etc\hosts`
	}
	return "/etc/hosts"
}

func CanWrite() bool {
	f, err := os.OpenFile(FilePath(), os.O_WRONLY, 0)
	if err != nil {
		return false
	}
	f.Close()
	return true
}

func Sync(tld string, domains []string) error {
	return syncToFile(FilePath(), tld, domains)
}

func Remove() error {
	return removeFromFile(FilePath())
}

func syncToFile(path string, tld string, domains []string) error {
	expected := buildBlock(tld, domains)

	existing, err := readBlock(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading hosts file: %w", err)
	}

	if expected == existing {
		return nil
	}

	content, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading hosts file: %w", err)
	}

	var newContent string
	if existing != "" {
		newContent = replaceBlock(string(content), expected)
	} else {
		trimmed := strings.TrimRight(string(content), "\n\r")
		if trimmed != "" {
			newContent = trimmed + "\n" + expected + "\n"
		} else {
			newContent = expected + "\n"
		}
	}

	return writeHostsFile(path, newContent)
}

func removeFromFile(path string) error {
	existing, err := readBlock(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading hosts file: %w", err)
	}

	if existing == "" {
		return nil
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading hosts file: %w", err)
	}

	newContent := replaceBlock(string(content), "")
	return writeHostsFile(path, newContent)
}

func buildBlock(tld string, domains []string) string {
	sorted := make([]string, len(domains))
	copy(sorted, domains)
	sort.Strings(sorted)

	var sb strings.Builder
	sb.WriteString(beginMarker)
	sb.WriteString("\n")
	sb.WriteString("# Managed by caddy-dev-local — do not edit.\n")
	fmt.Fprintf(&sb, "127.0.0.1    %s\n", tld)
	for _, d := range sorted {
		if d == tld {
			continue
		}
		fmt.Fprintf(&sb, "127.0.0.1    %s\n", d)
	}
	sb.WriteString(endMarker)
	sb.WriteString("\n")
	return sb.String()
}

func readBlock(path string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	lines := strings.Split(string(content), "\n")
	var block []string
	inBlock := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == beginMarker {
			inBlock = true
			block = append(block, line)
			continue
		}
		if trimmed == endMarker {
			inBlock = false
			block = append(block, line)
			continue
		}
		if inBlock {
			block = append(block, line)
		}
	}

	if len(block) == 0 {
		return "", nil
	}
	return strings.Join(block, "\n") + "\n", nil
}

func replaceBlock(content, newBlock string) string {
	lines := strings.Split(content, "\n")
	var result []string
	inBlock := false

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == beginMarker {
			inBlock = true
			if newBlock != "" {
				result = append(result, strings.TrimRight(newBlock, "\n"))
			}
			continue
		}
		if trimmed == endMarker {
			inBlock = false
			continue
		}
		if !inBlock {
			result = append(result, line)
		}
	}

	output := strings.Join(result, "\n")
	output = strings.TrimRight(output, "\n")
	if output != "" {
		output += "\n"
	}
	return output
}

func writeHostsFile(path, content string) error {
	if runtime.GOOS == "windows" {
		return os.WriteFile(path, []byte(content), 0644) //nolint:gosec // 0644 matches standard /etc/hosts permissions
	}

	tmp := path + ".devlocal.tmp"
	if err := os.WriteFile(tmp, []byte(content), 0644); err != nil { //nolint:gosec // 0644 matches standard /etc/hosts permissions
		return fmt.Errorf("writing temp hosts file: %w", err)
	}
	defer os.Remove(tmp)

	if err := os.Rename(tmp, path); err == nil {
		return nil
	}

	return writeInPlace(path, tmp)
}

func writeInPlace(path, tmp string) error {
	src, err := os.Open(tmp)
	if err != nil {
		return fmt.Errorf("opening temp hosts file: %w", err)
	}
	defer src.Close()

	dst, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("opening hosts file: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("writing hosts file: %w", err)
	}
	return nil
}
