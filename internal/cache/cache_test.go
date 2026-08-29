package cache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

type payload struct {
	Name  string
	When  time.Time
	Maybe *time.Time
	Extra map[string]string
}

func TestSaveLoadRoundTrip(t *testing.T) {
	t.Setenv("TOOLBOX_TUI_CACHE", t.TempDir())
	when := time.Date(2026, 8, 29, 10, 0, 0, 0, time.Local)
	in := payload{Name: "改修", When: when, Maybe: &when, Extra: map[string]string{"k": "v"}}
	if err := Save("thing", in); err != nil {
		t.Fatal(err)
	}
	var out payload
	at, err := Load("thing", &out)
	if err != nil {
		t.Fatal(err)
	}
	if out.Name != in.Name || !out.When.Equal(in.When) || out.Maybe == nil || !out.Maybe.Equal(when) {
		t.Errorf("round trip lost data: %+v", out)
	}
	if out.Extra["k"] != "v" {
		t.Errorf("map lost: %+v", out.Extra)
	}
	if time.Since(at) > time.Minute {
		t.Errorf("mtime looks wrong: %v", at)
	}
}

func TestLoadMissingAndCorrupt(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TOOLBOX_TUI_CACHE", dir)

	var out payload
	if _, err := Load("absent", &out); err != ErrMissing {
		t.Errorf("missing entry: err = %v, want ErrMissing", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load("broken", &out); err != ErrMissing {
		t.Errorf("corrupt entry: err = %v, want ErrMissing", err)
	}
}

func TestSavePermissionsAndAtomicity(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TOOLBOX_TUI_CACHE", dir)
	if err := Save("secret", payload{Name: "x"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "secret.json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("cache file mode = %o, want 600", perm)
	}
	// The temp file used for the atomic rename must not be left behind.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("cache dir holds %v, want just secret.json", names)
	}
}

func TestClear(t *testing.T) {
	t.Setenv("TOOLBOX_TUI_CACHE", t.TempDir())
	if err := Save("a", payload{Name: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := Clear(); err != nil {
		t.Fatal(err)
	}
	var out payload
	if _, err := Load("a", &out); err != ErrMissing {
		t.Errorf("entry survived Clear: %v", err)
	}
	// Clearing an already-absent directory is not an error.
	if err := Clear(); err != nil {
		t.Errorf("second Clear: %v", err)
	}
}
