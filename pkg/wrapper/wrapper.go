package wrapper

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/getlantern/systray"
	"github.com/kardianos/service"
)

type Config struct {
	Name        string
	DisplayName string
	Description string
	Executable  string
	Args        []string
	Env         map[string]string
	BrowserURL  string
}

type program struct {
	config Config
	cmd    *exec.Cmd
}

type standaloneState struct {
	PID         int    `json:"pid"`
	CommandHint string `json:"command_hint"`
	RecordedAt  int64  `json:"recorded_at"`
}

const defaultBrowserURL = "http://localhost:8080"

func (p *program) Start(s service.Service) error {
	go p.run()
	return nil
}

func (p *program) run() {
	p.cmd = exec.Command(p.config.Executable, p.config.Args...)
	p.cmd.Stdout = os.Stdout
	p.cmd.Stderr = os.Stderr

	p.cmd.Env = os.Environ()
	for k, v := range p.config.Env {
		p.cmd.Env = append(p.cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	if err := p.cmd.Start(); err != nil {
		log.Fatalf("Critical: Failed to start child process: %v", err)
	}

	if err := p.cmd.Wait(); err != nil {
		log.Printf("Child process exited: %v. Safe exit.", err)
		os.Exit(1)
	}
}

func (p *program) startDetached() error {
	p.cmd = exec.Command(p.config.Executable, p.config.Args...)
	p.cmd.Stdout = os.Stdout
	p.cmd.Stderr = os.Stderr
	p.cmd.Env = os.Environ()
	for k, v := range p.config.Env {
		p.cmd.Env = append(p.cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}
	if err := p.cmd.Start(); err != nil {
		return err
	}
	if err := writeStandaloneState(p.config.Name, p.cmd.Process.Pid, defaultCommandHint(p.config.Executable)); err != nil {
		log.Printf("Warning: failed to write PID file: %v", err)
	}
	return nil
}

func (p *program) Stop(s service.Service) error {
	if p.cmd != nil && p.cmd.Process != nil {
		return p.cmd.Process.Signal(syscall.SIGTERM)
	}
	return nil
}

func OpenBrowser(url string) {
	var err error
	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		err = exec.Command("open", url).Start()
	default:
		err = fmt.Errorf("unsupported platform")
	}
	if err != nil {
		log.Printf("Failed to open browser: %v", err)
	}
}

func RunAppliance(cfg Config, isLocal bool) {
	browserURL := effectiveBrowserURL(cfg.BrowserURL)

	if isStandaloneProcessAlive(cfg.Name) {
		fmt.Println("Standalone process already running. Opening browser...")
		OpenBrowser(browserURL)
		return
	}

	if isURLReachable(browserURL) {
		fmt.Println("Application appears to be already running. Opening browser...")
		OpenBrowser(browserURL)
		return
	}

	svcConfig := &service.Config{
		Name:        cfg.Name,
		DisplayName: cfg.DisplayName,
		Description: cfg.Description,
	}

	prg := &program{config: cfg}
	s, err := service.New(prg, svcConfig)
	if err != nil {
		log.Fatal(err)
	}

	if isLocal {
		fmt.Println("Running in local standalone mode...")
		if err := prg.startDetached(); err != nil {
			log.Fatalf("Critical: Failed to start child process: %v", err)
		}
		go openBrowserWhenReady(browserURL, 10*time.Second)
		runTray(cfg, browserURL, func() error {
			return StopAppliance(cfg)
		}, func() bool {
			return isStandaloneProcessAlive(cfg.Name)
		})
		return
	}

	status, err := s.Status()
	if err != nil || status == service.StatusUnknown {
		fmt.Println("Application not installed as service. Attempting to install...")
		if err := s.Install(); err != nil {
			log.Printf("Service install failed (%v). Falling back to standalone mode.", err)
			runStandaloneWithTray(prg, cfg, browserURL)
			return
		}
		if err := s.Start(); err != nil {
			log.Printf("Service start failed (%v). Falling back to standalone mode.", err)
			runStandaloneWithTray(prg, cfg, browserURL)
			return
		}
		fmt.Println("Service installed and started.")
		go openBrowserWhenReady(browserURL, 10*time.Second)
		runServiceTray(cfg, browserURL, s)
		return
	}

	if status == service.StatusRunning {
		fmt.Println("Service is already running. Launching browser...")
		OpenBrowser(browserURL)
		runServiceTray(cfg, browserURL, s)
		return
	}

	if status == service.StatusStopped {
		fmt.Println("Service is installed but stopped. Attempting to start...")
		if err := s.Start(); err != nil {
			log.Printf("Failed to start installed service (%v). Falling back to standalone mode.", err)
			runStandaloneWithTray(prg, cfg, browserURL)
			return
		}
		go openBrowserWhenReady(browserURL, 10*time.Second)
		runServiceTray(cfg, browserURL, s)
		return
	}

	// Default run as service.
	err = s.Run()
	if err != nil {
		log.Printf("Service run error: %v", err)
	}
}

func StopAppliance(cfg Config) error {
	standaloneStopped, standaloneErr := stopStandaloneByPID(cfg.Name)
	if standaloneErr != nil {
		log.Printf("Standalone stop error: %v", standaloneErr)
	}

	svcConfig := &service.Config{
		Name:        cfg.Name,
		DisplayName: cfg.DisplayName,
		Description: cfg.Description,
	}
	s, svcErr := service.New(&program{config: cfg}, svcConfig)
	serviceStopped := false
	if svcErr == nil {
		status, err := s.Status()
		if err == nil && status == service.StatusRunning {
			if err := s.Stop(); err != nil {
				return fmt.Errorf("failed to stop service: %w", err)
			}
			serviceStopped = true
		}
	}

	if standaloneStopped || serviceStopped {
		if standaloneStopped {
			fmt.Println("Stopped standalone process.")
		}
		if serviceStopped {
			fmt.Println("Stopped service process.")
		}
		return nil
	}

	if standaloneErr != nil {
		return standaloneErr
	}
	return fmt.Errorf("no running instance found")
}

func runStandaloneWithTray(prg *program, cfg Config, browserURL string) {
	fmt.Println("Running in standalone mode...")
	if err := prg.startDetached(); err != nil {
		log.Fatalf("Critical: Failed to start child process: %v", err)
	}
	go openBrowserWhenReady(browserURL, 10*time.Second)
	runTray(cfg, browserURL, func() error {
		return StopAppliance(cfg)
	}, func() bool {
		return isStandaloneProcessAlive(cfg.Name)
	})
}

func runServiceTray(cfg Config, browserURL string, s service.Service) {
	runTray(cfg, browserURL, func() error {
		return StopAppliance(cfg)
	}, func() bool {
		status, err := s.Status()
		if err != nil {
			return false
		}
		return status == service.StatusRunning
	})
}

func runTray(cfg Config, browserURL string, stopFn func() error, runningFn func() bool) {
	display := strings.TrimSpace(cfg.DisplayName)
	if display == "" {
		display = strings.TrimSpace(cfg.Name)
	}
	if display == "" {
		display = "Go-Deploy App"
	}
	tooltip := strings.TrimSpace(cfg.Description)
	if tooltip == "" {
		tooltip = display
	}

	systray.Run(func() {
		systray.SetTitle(display)
		systray.SetTooltip(tooltip)

		openItem := systray.AddMenuItem("Open", "Open application in browser")
		stopItem := systray.AddMenuItem("Stop", "Stop the application")
		quitItem := systray.AddMenuItem("Quit Tray", "Close tray icon")

		go func() {
			for {
				select {
				case <-openItem.ClickedCh:
					OpenBrowser(browserURL)
				case <-stopItem.ClickedCh:
					if err := stopFn(); err != nil {
						log.Printf("Stop failed: %v", err)
					}
					systray.Quit()
					return
				case <-quitItem.ClickedCh:
					systray.Quit()
					return
				}
			}
		}()

		go func() {
			for {
				time.Sleep(2 * time.Second)
				if !runningFn() {
					systray.Quit()
					return
				}
			}
		}()
	}, func() {})
}

func effectiveBrowserURL(input string) string {
	if strings.TrimSpace(input) == "" {
		return defaultBrowserURL
	}
	return input
}

func isURLReachable(url string) bool {
	client := &http.Client{Timeout: 1200 * time.Millisecond}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return true
}

func openBrowserWhenReady(url string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if isURLReachable(url) {
			OpenBrowser(url)
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	// Fall back to opening anyway even if readiness probe failed.
	OpenBrowser(url)
}

func appStateDir(appName string) (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(configDir, "go-deploy", sanitizeFileName(appName))
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return dir, nil
}

func pidFilePath(appName string) (string, error) {
	dir, err := appStateDir(appName)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "standalone.pid"), nil
}

func writeStandaloneState(appName string, pid int, commandHint string) error {
	path, err := pidFilePath(appName)
	if err != nil {
		return err
	}
	state := standaloneState{
		PID:         pid,
		CommandHint: commandHint,
		RecordedAt:  time.Now().Unix(),
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0644)
}

func stopStandaloneByPID(appName string) (bool, error) {
	state, path, err := loadStandaloneState(appName)
	if err != nil {
		return false, err
	}
	if state == nil {
		return false, nil
	}

	proc, err := os.FindProcess(state.PID)
	if err != nil {
		_ = os.Remove(path)
		return false, nil
	}
	if !isProcessAlive(state.PID) {
		_ = os.Remove(path)
		return false, nil
	}
	if !matchesCommandHint(state.PID, state.CommandHint) {
		_ = os.Remove(path)
		return false, fmt.Errorf("refusing to stop PID %d: process identity mismatch", state.PID)
	}

	if err := proc.Kill(); err != nil {
		_ = os.Remove(path)
		return false, err
	}
	_ = os.Remove(path)
	return true, nil
}

func isStandaloneProcessAlive(appName string) bool {
	state, path, err := loadStandaloneState(appName)
	if err != nil {
		return false
	}
	if state == nil {
		return false
	}
	if !isProcessAlive(state.PID) {
		_ = os.Remove(path)
		return false
	}
	if !matchesCommandHint(state.PID, state.CommandHint) {
		_ = os.Remove(path)
		return false
	}
	return true
}

func loadStandaloneState(appName string) (*standaloneState, string, error) {
	path, err := pidFilePath(appName)
	if err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, path, nil
		}
		return nil, path, err
	}
	raw := strings.TrimSpace(string(data))
	if raw == "" {
		_ = os.Remove(path)
		return nil, path, nil
	}

	var state standaloneState
	if err := json.Unmarshal(data, &state); err == nil && state.PID > 0 {
		if strings.TrimSpace(state.CommandHint) == "" {
			state.CommandHint = "appliance_bin_"
		}
		return &state, path, nil
	}

	// Backward compatibility with old PID-only file format.
	var pid int
	if _, err := fmt.Sscanf(raw, "%d", &pid); err == nil && pid > 0 {
		return &standaloneState{PID: pid, CommandHint: "appliance_bin_"}, path, nil
	}

	_ = os.Remove(path)
	return nil, path, nil
}

func isProcessAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		out, err := exec.Command("tasklist", "/FI", fmt.Sprintf("PID eq %d", pid)).CombinedOutput()
		if err != nil {
			return false
		}
		return strings.Contains(string(out), fmt.Sprintf(" %d ", pid)) || strings.Contains(string(out), fmt.Sprintf(" %d\r\n", pid))
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

func matchesCommandHint(pid int, hint string) bool {
	trimmed := strings.TrimSpace(hint)
	if trimmed == "" {
		return false
	}
	cmdline, err := processCommandLine(pid)
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(cmdline), strings.ToLower(trimmed))
}

func processCommandLine(pid int) (string, error) {
	if runtime.GOOS == "windows" {
		out, err := exec.Command("wmic", "process", "where", fmt.Sprintf("ProcessId=%d", pid), "get", "CommandLine", "/value").CombinedOutput()
		if err != nil {
			return "", err
		}
		return string(out), nil
	}

	out, err := exec.Command("ps", "-o", "command=", "-p", fmt.Sprintf("%d", pid)).CombinedOutput()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func defaultCommandHint(executablePath string) string {
	base := strings.ToLower(filepath.Base(strings.TrimSpace(executablePath)))
	if strings.HasPrefix(base, "appliance_bin_") {
		return "appliance_bin_"
	}
	if base == "" {
		return "appliance_bin_"
	}
	return base
}

func sanitizeFileName(name string) string {
	re := regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
	s := strings.TrimSpace(name)
	s = re.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-.")
	if s == "" {
		return "app"
	}
	return s
}
