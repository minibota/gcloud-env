package gcloud

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type Configuration struct {
	Name    string
	Active  bool
	Account string
	Project string
	HasADC  bool
}

type rawListConfig struct {
	Name       string `json:"name"`
	IsActive   bool   `json:"is_active"`
	Properties struct {
		Core struct {
			Account string `json:"account"`
			Project string `json:"project"`
		} `json:"core"`
	} `json:"properties"`
}

type Manager struct {
	GcloudPath string
	ConfigDir  string
}

func NewManager() (*Manager, error) {
	path, err := exec.LookPath("gcloud")
	if err != nil {
		return nil, errors.New("gcloud is not installed or is not in PATH")
	}

	configDir := os.Getenv("CLOUDSDK_CONFIG")
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("get home directory: %w", err)
		}
		configDir = filepath.Join(home, ".config", "gcloud")
	}

	return &Manager{GcloudPath: path, ConfigDir: configDir}, nil
}

func (m *Manager) gcloudCmd(args ...string) *exec.Cmd {
	cmd := exec.Command(m.GcloudPath, args...)
	if m.ConfigDir != "" {
		cmd.Env = append(os.Environ(), "CLOUDSDK_CONFIG="+m.ConfigDir)
	}
	return cmd
}

func (m *Manager) ListConfigurations() ([]Configuration, error) {
	cmd := m.gcloudCmd("config", "configurations", "list", "--format=json")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("list configurations: %w\n%s", err, strings.TrimSpace(string(out)))
	}

	var raw []rawListConfig
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("read gcloud output: %w", err)
	}

	configs := make([]Configuration, 0, len(raw))
	for _, r := range raw {
		cfg := Configuration{
			Name:    r.Name,
			Active:  r.IsActive,
			Account: r.Properties.Core.Account,
			Project: r.Properties.Core.Project,
		}
		if cfg.Account == "" || cfg.Project == "" {
			account, project := m.describeCore(cfg.Name)
			if cfg.Account == "" {
				cfg.Account = account
			}
			if cfg.Project == "" {
				cfg.Project = project
			}
		}
		cfg.HasADC = m.HasSavedADC(cfg.Name)
		configs = append(configs, cfg)
	}

	sort.SliceStable(configs, func(i, j int) bool {
		if configs[i].Active != configs[j].Active {
			return configs[i].Active
		}
		return configs[i].Name < configs[j].Name
	})
	return configs, nil
}

func (m *Manager) describeCore(name string) (string, string) {
	cmd := m.gcloudCmd("config", "configurations", "describe", name, "--format=json")
	out, err := cmd.Output()
	if err != nil {
		return "", ""
	}
	var data struct {
		Properties struct {
			Core struct {
				Account string `json:"account"`
				Project string `json:"project"`
			} `json:"core"`
		} `json:"properties"`
	}
	if json.Unmarshal(out, &data) != nil {
		return "", ""
	}
	return data.Properties.Core.Account, data.Properties.Core.Project
}

func (m *Manager) Activate(name string) error {
	cmd := m.gcloudCmd("config", "configurations", "activate", name, "--quiet")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("activate %q: %w\n%s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (m *Manager) Switch(name string) (bool, error) {
	var restored bool
	err := m.withLock(func() error {
		if err := m.Activate(name); err != nil {
			return err
		}
		if !m.HasSavedADC(name) {
			return nil
		}
		if err := m.RestoreADC(name); err != nil {
			return err
		}
		restored = true
		return nil
	})
	return restored, err
}

func (m *Manager) ADCPath() string {
	return filepath.Join(m.ConfigDir, "application_default_credentials.json")
}

func (m *Manager) savedADCDir() string {
	return filepath.Join(m.ConfigDir, "gcloud-env", "adc")
}

func safeName(name string) string {
	replacer := strings.NewReplacer("/", "_", "\\", "_", "..", "_")
	return replacer.Replace(name)
}

func (m *Manager) SavedADCPath(name string) string {
	return filepath.Join(m.savedADCDir(), safeName(name)+".json")
}

func (m *Manager) HasSavedADC(name string) bool {
	st, err := os.Stat(m.SavedADCPath(name))
	return err == nil && st.Mode().IsRegular() && st.Size() > 0
}

func (m *Manager) SaveCurrentADC(name string) error {
	return copyCredentialFile(m.ADCPath(), m.SavedADCPath(name))
}

func (m *Manager) RestoreADC(name string) error {
	return copyCredentialFile(m.SavedADCPath(name), m.ADCPath())
}

func copyCredentialFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read ADC %s: %w", src, err)
	}
	if !json.Valid(data) {
		return fmt.Errorf("ADC file %s does not contain valid JSON", src)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return fmt.Errorf("create ADC directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".adc-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary ADC: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("set temporary ADC permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temporary ADC: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temporary ADC: %w", err)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		return fmt.Errorf("install ADC: %w", err)
	}
	return os.Chmod(dst, 0o600)
}

func (m *Manager) LoginADC(cfg Configuration) error {
	return m.withLock(func() error {
		if err := m.Activate(cfg.Name); err != nil {
			return err
		}
		args := []string{"auth", "application-default", "login"}
		if cfg.Account != "" {
			args = append(args, cfg.Account)
		}
		if cfg.Project != "" {
			args = append(args, "--project="+cfg.Project)
		}

		cmd := m.gcloudCmd(args...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("ADC login: %w", err)
		}
		return m.SaveCurrentADC(cfg.Name)
	})
}

func (m *Manager) ActiveConfiguration() (string, error) {
	cmd := m.gcloudCmd("config", "configurations", "list", "--filter=is_active:true", "--format=value(name)")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("get active configuration: %w\n%s", err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}
