package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	gc "github.com/minibota/gcloud-env/internal/gcloud"
)

const (
	reset  = "\x1b[0m"
	bold   = "\x1b[1m"
	dim    = "\x1b[2m"
	purple = "\x1b[38;5;141m"
	green  = "\x1b[38;5;78m"
	yellow = "\x1b[38;5;214m"
	red    = "\x1b[38;5;203m"
	cyan   = "\x1b[38;5;81m"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

type tui struct {
	mgr      *gc.Manager
	configs  []gc.Configuration
	selected int
	status   string
	isError  bool
	tty      *os.File
	sttyOld  string
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "help", "-h", "--help":
			usage(0)
		case "version", "--version":
			fmt.Printf("gcloud-env %s\n", version)
			return
		}
	}

	mgr, err := gc.NewManager()
	if err != nil {
		fatal(err)
	}

	if len(os.Args) > 1 {
		handleCLI(mgr, os.Args[1:])
		return
	}

	configs, err := mgr.ListConfigurations()
	if err != nil {
		fatal(err)
	}
	if len(configs) == 0 {
		fatal(fmt.Errorf("no gcloud configurations found"))
	}

	app := &tui{mgr: mgr, configs: configs}
	if err := app.run(); err != nil {
		fatal(err)
	}
}

func (t *tui) run() error {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open terminal: %w", err)
	}
	t.tty = tty
	defer tty.Close()

	if err := t.enterRaw(); err != nil {
		return err
	}
	defer t.restoreTerminal()

	fmt.Fprint(t.tty, "\x1b[?25l")
	defer fmt.Fprint(t.tty, "\x1b[?25h\x1b[0m\n")

	reader := bufio.NewReader(t.tty)
	for {
		t.render()
		key, err := readKey(reader)
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		switch key {
		case "up", "k":
			if t.selected > 0 {
				t.selected--
			}
		case "down", "j":
			if t.selected < len(t.configs)-1 {
				t.selected++
			}
		case "enter":
			t.switchSelected()
		case "a":
			t.authenticateSelected()
		case "r":
			t.reload("Configurations reloaded")
		case "q", "ctrl-c", "esc":
			return nil
		}
	}
}

func (t *tui) enterRaw() error {
	cmd := exec.Command("stty", "-g")
	cmd.Stdin = t.tty
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("read terminal state with stty: %w", err)
	}
	t.sttyOld = strings.TrimSpace(string(out))

	cmd = exec.Command("stty", "-echo", "-icanon", "min", "1", "time", "0")
	cmd.Stdin = t.tty
	cmd.Stdout = t.tty
	cmd.Stderr = t.tty
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("enable TUI mode with stty: %w", err)
	}
	return nil
}

func (t *tui) restoreTerminal() {
	if t.sttyOld == "" || t.tty == nil {
		return
	}
	cmd := exec.Command("stty", t.sttyOld)
	cmd.Stdin = t.tty
	cmd.Stdout = t.tty
	cmd.Stderr = t.tty
	_ = cmd.Run()
}

func (t *tui) render() {
	fmt.Fprint(t.tty, "\x1b[2J\x1b[H")
	fmt.Fprintf(t.tty, "%s%s gcloud-env%s\n", bold, purple, reset)
	fmt.Fprintf(t.tty, "%sSwitch gcloud configuration and restore its saved ADC.%s\n\n", dim, reset)

	for i, cfg := range t.configs {
		cursor := "  "
		if i == t.selected {
			cursor = cyan + "› " + reset
		}
		active := " "
		if cfg.Active {
			active = green + "●" + reset
		}
		adc := yellow + "ADC —" + reset
		if cfg.HasADC {
			adc = green + "ADC ✓" + reset
		}
		name := cfg.Name
		if i == t.selected {
			name = bold + name + reset
		}

		fmt.Fprintf(t.tty, "%s%s  %-22s %s\n", cursor, active, name, adc)
		fmt.Fprintf(t.tty, "      %s%-30s%s  %s%s%s\n", dim, fallback(cfg.Account, "no account"), reset, dim, fallback(cfg.Project, "no project"), reset)
	}

	fmt.Fprintln(t.tty)
	if t.status != "" {
		color := green
		if t.isError {
			color = red
		}
		fmt.Fprintf(t.tty, "%s%s%s\n\n", color, t.status, reset)
	}
	fmt.Fprintf(t.tty, "%s↑/↓ or j/k%s move   %sEnter%s switch   %sa%s authenticate ADC   %sr%s reload   %sq%s quit\n",
		dim, reset, bold, reset, bold, reset, bold, reset, bold, reset)
}

func (t *tui) switchSelected() {
	cfg := t.configs[t.selected]
	restored, err := t.mgr.Switch(cfg.Name)
	if err != nil {
		t.setError(err)
		return
	}
	if restored {
		t.setStatus(fmt.Sprintf("✓ %s activated · ADC restored", cfg.Name))
	} else {
		t.setStatus(fmt.Sprintf("✓ %s activated · no saved ADC; press 'a' once to authenticate", cfg.Name))
	}
	t.reload(t.status)
}

func (t *tui) authenticateSelected() {
	cfg := t.configs[t.selected]

	t.restoreTerminal()
	fmt.Fprint(t.tty, "\x1b[?25h\x1b[2J\x1b[H")
	fmt.Fprintf(t.tty, "%sAuthenticating ADC for %s%s\n", bold, cfg.Name, reset)
	fmt.Fprintf(t.tty, "%sAccount: %s · Project: %s%s\n", dim, fallback(cfg.Account, "(gcloud will choose)"), fallback(cfg.Project, "(no project)"), reset)
	fmt.Fprintf(t.tty, "%sWaiting if another gcloud-env is switching this directory…%s\n\n", dim, reset)

	if err := t.mgr.LoginADC(cfg); err != nil {
		t.setError(err)
	} else {
		t.setStatus(fmt.Sprintf("✓ ADC authenticated and saved for %s", cfg.Name))
	}

	if err := t.enterRaw(); err != nil {
		t.setError(err)
		return
	}
	fmt.Fprint(t.tty, "\x1b[?25l")
	t.reload(t.status)
}

func (t *tui) reload(status string) {
	configs, err := t.mgr.ListConfigurations()
	if err != nil {
		t.setError(err)
		return
	}
	selectedName := ""
	if len(t.configs) > 0 && t.selected < len(t.configs) {
		selectedName = t.configs[t.selected].Name
	}
	t.configs = configs
	for i, cfg := range configs {
		if cfg.Name == selectedName {
			t.selected = i
			break
		}
	}
	if t.selected >= len(configs) {
		t.selected = len(configs) - 1
	}
	t.status = status
	t.isError = false
}

func (t *tui) setStatus(s string) {
	t.status = s
	t.isError = false
}

func (t *tui) setError(err error) {
	t.status = "Error: " + err.Error()
	t.isError = true
}

func readKey(r *bufio.Reader) (string, error) {
	b, err := r.ReadByte()
	if err != nil {
		return "", err
	}
	switch b {
	case 3:
		return "ctrl-c", nil
	case '\r', '\n':
		return "enter", nil
	case 27:
		// ESC is treated as the start of a terminal escape sequence.
		// Use q or Ctrl-C to exit the TUI.
		b2, err := r.ReadByte()
		if err != nil {
			return "esc", nil
		}
		if b2 != '[' {
			return "esc", nil
		}
		b3, err := r.ReadByte()
		if err != nil {
			return "esc", nil
		}
		switch b3 {
		case 'A':
			return "up", nil
		case 'B':
			return "down", nil
		}
		return "esc", nil
	default:
		return string(b), nil
	}
}

func handleCLI(mgr *gc.Manager, args []string) {
	switch args[0] {
	case "use":
		if len(args) != 2 {
			usage(2)
		}
		restored, err := mgr.Switch(args[1])
		if err != nil {
			fatal(err)
		}
		if restored {
			fmt.Printf("✓ %s activated; ADC restored\n", args[1])
		} else {
			fmt.Printf("✓ %s activated; no saved ADC (open gcloud-env and press 'a')\n", args[1])
		}
	case "status":
		configs, err := mgr.ListConfigurations()
		if err != nil {
			fatal(err)
		}
		for _, cfg := range configs {
			if cfg.Active {
				adc := "no"
				if cfg.HasADC {
					adc = "yes"
				}
				fmt.Printf("config: %s\naccount: %s\nproject: %s\nsaved adc: %s\n", cfg.Name, cfg.Account, cfg.Project, adc)
				return
			}
		}
		fmt.Println("No active configuration")
	case "version", "--version":
		fmt.Printf("gcloud-env %s\n", version)
	case "help", "-h", "--help":
		usage(0)
	default:
		usage(2)
	}
}

func usage(code int) {
	fmt.Print(`gcloud-env

Usage:
  gcloud-env              open the TUI
  gcloud-env use NAME     activate NAME and restore its saved ADC
  gcloud-env status       show the active context
  gcloud-env --version    show the build version
  gcloud-env help         show this help
`)
	os.Exit(code)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "gcloud-env:", err)
	os.Exit(1)
}

func fallback(v, alt string) string {
	if strings.TrimSpace(v) == "" {
		return alt
	}
	return v
}
