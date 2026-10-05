package store

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// readDoc reads a Markdown file with YAML frontmatter into meta and returns its body.
func readDoc(path string, meta any) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	s := string(b)
	if !strings.HasPrefix(s, "---\n") {
		return "", fmt.Errorf("%s: no YAML frontmatter", path)
	}
	head, body, ok := strings.Cut(s[4:], "\n---\n")
	if !ok {
		if strings.HasSuffix(s, "\n---") {
			head, body = strings.TrimSuffix(s[4:], "\n---"), ""
		} else {
			return "", fmt.Errorf("%s: unterminated frontmatter", path)
		}
	}
	if err := yaml.Unmarshal([]byte(head), meta); err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	return strings.TrimSpace(body), nil
}

// writeDoc writes the file atomically: a temporary file, then a rename.
func writeDoc(path string, meta any, body string) error {
	var buf bytes.Buffer
	buf.WriteString("---\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(meta); err != nil {
		return err
	}
	enc.Close()
	buf.WriteString("---\n")
	if body = strings.TrimSpace(body); body != "" {
		buf.WriteString("\n" + body + "\n")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), path)
}
