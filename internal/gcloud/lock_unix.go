//go:build unix

package gcloud

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func (m *Manager) withLock(fn func() error) error {
	dir, err := m.resolvedConfigDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	lockPath := filepath.Join(dir, "gcloud-env.lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("open config lock: %w", err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock config directory: %w", err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:errcheck
	return fn()
}

func (m *Manager) resolvedConfigDir() (string, error) {
	dir := m.ConfigDir
	if dir == "" {
		return "", fmt.Errorf("config directory is empty")
	}
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return resolved, nil
	}
	return dir, nil
}
