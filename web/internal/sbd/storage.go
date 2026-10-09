package sbd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// FileSuffix is the extension of Sideboarder documents.
const FileSuffix = ".sbd.json"

// LoadDocument reads and parses a document file.
func LoadDocument(path string) (*Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseDocument(data)
}

// Marshal serializes a document the way the TUI does (2-space indent).
func Marshal(doc *Document) ([]byte, error) {
	doc.Normalize()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// SaveDocument writes a document to path (creating parent dirs). The write
// goes through a temp file + rename so a crash never leaves a torn file.
func SaveDocument(doc *Document, path string) error {
	data, err := Marshal(doc)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".sbd-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// DefaultFilename is a filesystem-friendly default filename for a deck.
func DefaultFilename(deckName string) string {
	var b strings.Builder
	for _, c := range deckName {
		if unicode.IsLetter(c) || unicode.IsDigit(c) || c == ' ' || c == '-' || c == '_' {
			b.WriteRune(c)
		}
	}
	safe := strings.ReplaceAll(strings.TrimSpace(b.String()), " ", "_")
	if safe == "" {
		safe = "deck"
	}
	return safe + FileSuffix
}
