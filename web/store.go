package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// docStore holds the *.sbd.json documents. The server uses a directory on
// disk; the in-browser demo build uses memory.
type docStore interface {
	List() ([]storedDoc, error)
	Read(name string) ([]byte, time.Time, error)
	Write(name string, data []byte) error // atomic replace
	Exists(name string) bool
	Location() string // shown to the user
}

type storedDoc struct {
	Name     string
	Modified time.Time
}

// dirStore keeps documents as files in one directory.
type dirStore struct{ dir string }

func (d dirStore) Location() string { return d.dir }

func (d dirStore) List() ([]storedDoc, error) {
	entries, err := os.ReadDir(d.dir)
	if err != nil {
		return nil, err
	}
	var docs []storedDoc
	for _, e := range entries {
		if e.IsDir() || !validName(e.Name()) {
			continue
		}
		doc := storedDoc{Name: e.Name()}
		if info, err := e.Info(); err == nil {
			doc.Modified = info.ModTime()
		}
		docs = append(docs, doc)
	}
	return docs, nil
}

func (d dirStore) Read(name string) ([]byte, time.Time, error) {
	p := filepath.Join(d.dir, name)
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, time.Time{}, err
	}
	var mod time.Time
	if info, err := os.Stat(p); err == nil {
		mod = info.ModTime()
	}
	return data, mod, nil
}

// Write goes through a temp file + rename so a crash never leaves a torn file.
func (d dirStore) Write(name string, data []byte) error {
	if err := os.MkdirAll(d.dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(d.dir, ".sbd-*.tmp")
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
	return os.Rename(tmp.Name(), filepath.Join(d.dir, name))
}

func (d dirStore) Exists(name string) bool {
	_, err := os.Stat(filepath.Join(d.dir, name))
	return !errors.Is(err, fs.ErrNotExist)
}

// memStore keeps documents in memory.
type memStore struct {
	mu      sync.Mutex
	docs    map[string]storedFile
	onWrite func(name string, data []byte) // optional persistence hook
}

type storedFile struct {
	data []byte
	mod  time.Time
}

func newMemStore() *memStore { return &memStore{docs: map[string]storedFile{}} }

func (m *memStore) Location() string { return "(in this browser tab)" }

func (m *memStore) List() ([]storedDoc, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var docs []storedDoc
	for name, f := range m.docs {
		docs = append(docs, storedDoc{Name: name, Modified: f.mod})
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].Name < docs[j].Name })
	return docs, nil
}

func (m *memStore) Read(name string) ([]byte, time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f, ok := m.docs[name]
	if !ok {
		return nil, time.Time{}, fs.ErrNotExist
	}
	return f.data, f.mod, nil
}

func (m *memStore) Write(name string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.docs[name] = storedFile{data: append([]byte(nil), data...), mod: time.Now()}
	if m.onWrite != nil {
		m.onWrite(name, data)
	}
	return nil
}

func (m *memStore) Exists(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.docs[name]
	return ok
}
