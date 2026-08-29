// Package cache stores the last successful fetch on disk so the TUI can paint
// real content on the first frame instead of an empty loading screen. Entries
// are a convenience, never a source of truth: a network refresh always follows
// and overwrites whatever was read here.
package cache

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// Dir returns the cache directory, honouring the platform convention.
func Dir() string {
	if p := os.Getenv("TOOLBOX_TUI_CACHE"); p != "" {
		return p
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "toolbox-tui")
	}
	return filepath.Join(base, "toolbox-tui")
}

func path(name string) string { return filepath.Join(Dir(), name+".json") }

// ErrMissing reports that nothing has been cached under this name yet.
var ErrMissing = errors.New("cache: no entry")

// Load decodes the named entry into v and reports when it was written. A
// missing or corrupt entry returns ErrMissing, since either way there is
// nothing usable and the caller's response is the same: fetch.
func Load(name string, v any) (time.Time, error) {
	file := path(name)
	info, err := os.Stat(file)
	if err != nil {
		return time.Time{}, ErrMissing
	}
	data, err := os.ReadFile(file)
	if err != nil {
		return time.Time{}, ErrMissing
	}
	if err := json.Unmarshal(data, v); err != nil {
		return time.Time{}, ErrMissing
	}
	return info.ModTime(), nil
}

// Save writes v under the given name. It writes to a temporary file and renames
// so a crash or a second process cannot leave a half-written entry behind.
func Save(name string, v any) error {
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	file := path(name)
	// The cache can hold issue titles and calendar summaries, so keep it 0600.
	tmp, err := os.CreateTemp(Dir(), "."+name+"-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), file)
}

// Clear removes every cached entry.
func Clear() error {
	err := os.RemoveAll(Dir())
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
