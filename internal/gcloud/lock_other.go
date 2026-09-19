//go:build !unix

package gcloud

import "fmt"

func (m *Manager) withLock(fn func() error) error {
	return fmt.Errorf("config directory locking is only supported on Linux and macOS")
}
