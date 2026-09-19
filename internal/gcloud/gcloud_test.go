package gcloud

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if os.Getenv("GCLOUD_ENV_HELPER") == "switch" {
		os.Exit(runSwitchHelper())
	}
	os.Exit(m.Run())
}

func runSwitchHelper() int {
	m := &Manager{
		GcloudPath: os.Getenv("GCLOUD_ENV_FAKE"),
		ConfigDir:  os.Getenv("CLOUDSDK_CONFIG"),
	}
	name := os.Getenv("GCLOUD_ENV_SWITCH")
	if _, err := m.Switch(name); err != nil {
		os.Stderr.WriteString(err.Error() + "\n")
		return 1
	}
	f, err := os.OpenFile(os.Getenv("GCLOUD_ENV_DONE"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return 1
	}
	defer f.Close()
	if _, err := f.WriteString(name + "\n"); err != nil {
		return 1
	}
	return 0
}

func writeFakeGcloud(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "fake-gcloud")
	script := `#!/bin/sh
set -e
CONFIG="${CLOUDSDK_CONFIG:?}"
STATE="$CONFIG/fake-state"
mkdir -p "$STATE"
if [ "$1" = config ] && [ "$2" = configurations ] && [ "$3" = activate ]; then
  sleep "${GCLOUD_FAKE_SLEEP:-0}"
  printf '%s\n' "$4" > "$STATE/active"
  exit 0
fi
if [ "$1" = config ] && [ "$2" = configurations ] && [ "$3" = list ]; then
  active=$(tr -d '[:space:]' < "$STATE/active" 2>/dev/null || true)
  for arg in "$@"; do
    if [ "$arg" = "--filter=is_active:true" ]; then
      printf '%s\n' "$active"
      exit 0
    fi
  done
  dev_active=false
  prod_active=false
  [ "$active" = dev ] && dev_active=true
  [ "$active" = prod ] && prod_active=true
  printf '[{"name":"dev","is_active":%s,"properties":{"core":{"account":"dev@example.com","project":"dev"}}},{"name":"prod","is_active":%s,"properties":{"core":{"account":"prod@example.com","project":"prod"}}}]\n' "$dev_active" "$prod_active"
  exit 0
fi
if [ "$1" = auth ]; then
  printf '%s\n' '{"type":"authorized_user","refresh_token":"login"}' > "$CONFIG/application_default_credentials.json"
  exit 0
fi
echo "unexpected: $*" >&2
exit 1
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}

func seedADC(t *testing.T, m *Manager, name, token string) {
	t.Helper()
	body := []byte(`{"type":"authorized_user","refresh_token":"` + token + `"}`)
	if err := os.MkdirAll(filepath.Dir(m.SavedADCPath(name)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.SavedADCPath(name), body, 0o600); err != nil {
		t.Fatal(err)
	}
}

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

func TestCopyCredentialFileUsesUniqueTempAnd0600(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "source")
	dst := filepath.Join(dir, "destination")
	body := []byte(`{"type":"authorized_user","refresh_token":"unique-temp"}`)
	if err := os.WriteFile(src, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyCredentialFile(src, dst); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(body) {
		t.Fatalf("dst = %s", got)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions = %o", info.Mode().Perm())
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".adc-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("leftover temps: %v", matches)
	}
}

func TestConcurrentCopiesDoNotShareTemp(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "destination")
	var wg sync.WaitGroup
	errCh := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			src := filepath.Join(dir, "src-"+string(rune('a'+i)))
			body, _ := json.Marshal(map[string]any{"i": i, "type": "authorized_user"})
			if err := os.WriteFile(src, body, 0o600); err != nil {
				errCh <- err
				return
			}
			errCh <- copyCredentialFile(src, dst)
		}(i)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(got) {
		t.Fatalf("destination is not valid JSON: %s", got)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions = %o", info.Mode().Perm())
	}
}

func TestSwitchSerializesProcesses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("flock is unix-only")
	}
	dir := t.TempDir()
	fake := writeFakeGcloud(t, dir)
	m := &Manager{GcloudPath: fake, ConfigDir: dir}
	seedADC(t, m, "dev", "dev-token")
	seedADC(t, m, "prod", "prod-token")
	if err := os.MkdirAll(filepath.Join(dir, "fake-state"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fake-state", "active"), []byte("dev\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	done := filepath.Join(dir, "done")
	run := func(name string) *exec.Cmd {
		cmd := exec.Command(os.Args[0], "-test.run=TestSwitchSerializesProcesses")
		cmd.Env = append(os.Environ(),
			"GCLOUD_ENV_HELPER=switch",
			"GCLOUD_ENV_FAKE="+fake,
			"CLOUDSDK_CONFIG="+dir,
			"GCLOUD_ENV_SWITCH="+name,
			"GCLOUD_ENV_DONE="+done,
			"GCLOUD_FAKE_SLEEP=0.25",
		)
		cmd.Dir = dir
		return cmd
	}
	a := run("dev")
	b := run("prod")
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := b.Start(); err != nil {
		t.Fatal(err)
	}
	if err := a.Wait(); err != nil {
		t.Fatalf("dev switch: %v", err)
	}
	if err := b.Wait(); err != nil {
		t.Fatalf("prod switch: %v", err)
	}

	order, err := os.ReadFile(done)
	if err != nil {
		t.Fatal(err)
	}
	lines := splitNonEmpty(string(order))
	if len(lines) != 2 {
		t.Fatalf("completion order %q", order)
	}
	last := lines[len(lines)-1]
	active, err := os.ReadFile(filepath.Join(dir, "fake-state", "active"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(bytesTrim(active)); got != last {
		t.Fatalf("active = %q, last completed = %q", got, last)
	}
	adc, err := os.ReadFile(m.ADCPath())
	if err != nil {
		t.Fatal(err)
	}
	want := `"refresh_token":"` + last + `-token"`
	if !strings.Contains(string(adc), want) {
		t.Fatalf("adc %s does not match last switch %s", adc, last)
	}
}

func TestLockReleasedAfterFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("flock is unix-only")
	}
	dir := t.TempDir()
	m := &Manager{GcloudPath: filepath.Join(dir, "missing-gcloud"), ConfigDir: dir}
	if _, err := m.Switch("dev"); err == nil {
		t.Fatal("expected activate failure")
	}
	fake := writeFakeGcloud(t, dir)
	m.GcloudPath = fake
	seedADC(t, m, "dev", "dev-token")
	if err := os.MkdirAll(filepath.Join(dir, "fake-state"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Switch("dev"); err != nil {
		t.Fatal(err)
	}
}

func TestIndependentConfigDirsDoNotShareLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("flock is unix-only")
	}
	a := t.TempDir()
	b := t.TempDir()
	fakeA := writeFakeGcloud(t, a)
	fakeB := writeFakeGcloud(t, b)
	ma := &Manager{GcloudPath: fakeA, ConfigDir: a}
	mb := &Manager{GcloudPath: fakeB, ConfigDir: b}
	seedADC(t, ma, "dev", "a-token")
	seedADC(t, mb, "prod", "b-token")
	if err := os.MkdirAll(filepath.Join(a, "fake-state"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(b, "fake-state"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := ma.Switch("dev"); err != nil {
		t.Fatal(err)
	}
	if _, err := mb.Switch("prod"); err != nil {
		t.Fatal(err)
	}
	adcA, _ := os.ReadFile(ma.ADCPath())
	adcB, _ := os.ReadFile(mb.ADCPath())
	if !strings.Contains(string(adcA), "a-token") || !strings.Contains(string(adcB), "b-token") {
		t.Fatalf("a=%s b=%s", adcA, adcB)
	}
}

func TestSymlinkConfigDirSharesLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("flock is unix-only")
	}
	real := t.TempDir()
	parent := t.TempDir()
	link := filepath.Join(parent, "alias")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	fake := writeFakeGcloud(t, real)
	m1 := &Manager{GcloudPath: fake, ConfigDir: real}
	m2 := &Manager{GcloudPath: fake, ConfigDir: link}
	seedADC(t, m1, "dev", "dev-token")
	seedADC(t, m1, "prod", "prod-token")
	if err := os.MkdirAll(filepath.Join(real, "fake-state"), 0o700); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	done := make(chan error, 2)
	go func() {
		close(started)
		_, err := m1.Switch("dev")
		done <- err
	}()
	<-started
	_, err := m2.Switch("prod")
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func splitNonEmpty(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == '\n' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	return out
}

func bytesTrim(b []byte) []byte {
	i, j := 0, len(b)
	for i < j && (b[i] == ' ' || b[i] == '\n' || b[i] == '\t' || b[i] == '\r') {
		i++
	}
	for j > i && (b[j-1] == ' ' || b[j-1] == '\n' || b[j-1] == '\t' || b[j-1] == '\r') {
		j--
	}
	return b[i:j]
}
