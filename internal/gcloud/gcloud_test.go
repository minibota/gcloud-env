package gcloud

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndRestoreADC(t *testing.T) {
	m := &Manager{ConfigDir: t.TempDir()}
	original := []byte(`{"type":"authorized_user","refresh_token":"test-placeholder"}`)
	if err := os.WriteFile(m.ADCPath(), original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.SaveCurrentADC("demo"); err != nil {
		t.Fatal(err)
	}
	if !m.HasSavedADC("demo") {
		t.Fatal("saved ADC not detected")
	}
	if err := os.WriteFile(m.ADCPath(), []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.RestoreADC("demo"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(m.ADCPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("restored ADC = %s", got)
	}
	for _, path := range []string{m.ADCPath(), m.SavedADCPath("demo")} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("%s permissions = %o", path, info.Mode().Perm())
		}
	}
}

func TestInvalidADCDoesNotReplaceDestination(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source")
	dst := filepath.Join(dir, "destination")
	if err := os.WriteFile(src, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte(`{"existing":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := copyCredentialFile(src, dst); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"existing":true}` {
		t.Fatal("existing ADC modified")
	}
}
