//go:build windows

// axonhub-service is a small Windows Service Control Manager host. It keeps
// the application processes outside of the service process so the existing
// AxonHub binary does not need to know about Windows service callbacks.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
)

const serviceName = "AxonHub"

type serviceConfig struct {
	Backend         string            `json:"backend"`
	BackendArgs     []string          `json:"backendArgs,omitempty"`
	WorkDir         string            `json:"workDir"`
	LogDir          string            `json:"logDir"`
	Frontend        string            `json:"frontend,omitempty"`
	FrontendArgs    []string          `json:"frontendArgs,omitempty"`
	FrontendDir     string            `json:"frontendDir,omitempty"`
	Environment     map[string]string `json:"environment,omitempty"`
	RestartDelaySec int               `json:"restartDelaySec,omitempty"`
}

type managedProcess struct {
	name string
	cmd  *exec.Cmd
	done chan struct{}
}

type processExit struct {
	process *managedProcess
	err     error
}

type service struct {
	config serviceConfig
	log    *log.Logger
	logFile *os.File

	mu       sync.Mutex
	children map[string]*managedProcess
	exits    chan processExit
	stopOnce sync.Once
	stopCh   chan struct{}
	stopping bool
}

func main() {
	config, err := loadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "axonhub-service:", err)
		os.Exit(1)
	}

	manager, err := newService(config)
	if err != nil {
		fmt.Fprintln(os.Stderr, "axonhub-service:", err)
		os.Exit(1)
	}
	defer manager.closeLog()

	isService, err := svc.IsWindowsService()
	if err != nil {
		manager.log.Printf("cannot detect service mode: %v", err)
		os.Exit(1)
	}

	if isService {
		err = svc.Run(serviceName, manager)
	} else {
		err = manager.runConsole()
	}
	if err != nil {
		manager.log.Printf("service exited with error: %v", err)
		os.Exit(1)
	}
}

func loadConfig() (serviceConfig, error) {
	configPath := flag.String("config", "", "path to the service JSON configuration")
	flag.Parse()

	if *configPath == "" {
		exe, err := os.Executable()
		if err != nil {
			return serviceConfig{}, fmt.Errorf("resolve executable path: %w", err)
		}
		*configPath = filepath.Join(filepath.Dir(exe), "axonhub-service.json")
	}

	data, err := os.ReadFile(*configPath)
	if err != nil {
		return serviceConfig{}, fmt.Errorf("read config %q: %w", *configPath, err)
	}

	var config serviceConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return serviceConfig{}, fmt.Errorf("parse config %q: %w", *configPath, err)
	}

	configBase := filepath.Dir(*configPath)
	config.Backend = resolvePath(configBase, config.Backend)
	config.WorkDir = resolvePath(configBase, config.WorkDir)
	config.LogDir = resolvePath(configBase, config.LogDir)
	config.Frontend = resolvePath(configBase, config.Frontend)
	config.FrontendDir = resolvePath(configBase, config.FrontendDir)

	if config.Backend == "" {
		return serviceConfig{}, errors.New("backend path is required")
	}
	if config.WorkDir == "" {
		config.WorkDir = filepath.Dir(config.Backend)
	}
	if config.LogDir == "" {
		config.LogDir = filepath.Join(configBase, "logs")
	}
	if config.RestartDelaySec <= 0 {
		config.RestartDelaySec = 5
	}

	return config, nil
}

func resolvePath(base, value string) string {
	if value == "" {
		return ""
	}
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Clean(filepath.Join(base, value))
}

func newService(config serviceConfig) (*service, error) {
	if err := os.MkdirAll(config.LogDir, 0o755); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}

	logPath := filepath.Join(config.LogDir, "service.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open service log: %w", err)
	}

	return &service{
		config:   config,
		log:      log.New(logFile, "", log.LstdFlags|log.Lmicroseconds),
		logFile:  logFile,
		children: make(map[string]*managedProcess),
		exits:    make(chan processExit, 4),
		stopCh:   make(chan struct{}),
	}, nil
}

func (s *service) closeLog() {
	if s.logFile != nil {
		_ = s.logFile.Close()
	}
}

func (s *service) Execute(_ []string, requests <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	changes <- svc.Status{State: svc.StartPending, WaitHint: 10_000}

	if err := s.startAll(); err != nil {
		s.log.Printf("startup failed: %v", err)
		return false, 1
	}

	accepted := svc.AcceptStop | svc.AcceptShutdown
	changes <- svc.Status{State: svc.Running, Accepts: accepted}
	s.log.Printf("%s service is running", serviceName)

	for {
		select {
		case request, ok := <-requests:
			if !ok {
				s.stopAll()
				return false, 0
			}

			switch request.Cmd {
			case svc.Interrogate:
				changes <- request.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending, WaitHint: 15_000}
				s.stopAll()
				s.log.Printf("%s service stopped", serviceName)
				return false, 0
			}
		case exit := <-s.exits:
			if s.isStopping() {
				continue
			}
			s.log.Printf("%s process exited: %v", exit.process.name, exit.err)
			if err := s.restartAfter(exit.process); err != nil {
				s.log.Printf("restart %s failed: %v", exit.process.name, err)
			}
		}
	}
}

func (s *service) runConsole() error {
	if err := s.startAll(); err != nil {
		return err
	}

	s.log.Printf("running in console mode; press Ctrl+C to stop")
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	<-signals
	s.stopAll()
	return nil
}

func (s *service) startAll() error {
	if err := s.startChild("backend", s.config.Backend, s.config.WorkDir, s.config.BackendArgs, false); err != nil {
		return err
	}

	if s.config.Frontend != "" {
		if err := s.startChild("frontend", s.config.Frontend, s.config.FrontendDir, s.config.FrontendArgs, true); err != nil {
			s.stopAll()
			return err
		}
	}

	return nil
}

func (s *service) startChild(name, executable, workDir string, args []string, batchFile bool) error {
	if _, err := os.Stat(executable); err != nil {
		return fmt.Errorf("%s executable %q is unavailable: %w", name, executable, err)
	}
	if workDir == "" {
		workDir = filepath.Dir(executable)
	}

	var command *exec.Cmd
	if batchFile {
		commandLine := quoteWindowsArg(executable)
		for _, arg := range args {
			commandLine += " " + quoteWindowsArg(arg)
		}
		command = exec.Command("cmd.exe", "/d", "/s", "/c", commandLine)
	} else {
		command = exec.Command(executable, args...)
	}
	command.Dir = workDir
	command.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
	command.Env = withEnvironment(os.Environ(), s.config.Environment)

	stdoutPath := filepath.Join(s.config.LogDir, name+".stdout.log")
	stderrPath := filepath.Join(s.config.LogDir, name+".stderr.log")
	stdout, err := os.OpenFile(stdoutPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return fmt.Errorf("open %s stdout log: %w", name, err)
	}
	stderr, err := os.OpenFile(stderrPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		_ = stdout.Close()
		return fmt.Errorf("open %s stderr log: %w", name, err)
	}
	command.Stdout = stdout
	command.Stderr = stderr

	if err := command.Start(); err != nil {
		_ = stdout.Close()
		_ = stderr.Close()
		return fmt.Errorf("start %s: %w", name, err)
	}

	process := &managedProcess{name: name, cmd: command, done: make(chan struct{})}
	s.mu.Lock()
	s.children[name] = process
	s.mu.Unlock()
	s.log.Printf("started %s (PID %d)", name, command.Process.Pid)

	go func() {
		err := command.Wait()
		_ = stdout.Close()
		_ = stderr.Close()
		close(process.done)
		s.exits <- processExit{process: process, err: err}
	}()

	return nil
}

func (s *service) restartAfter(previous *managedProcess) error {
	s.mu.Lock()
	if current := s.children[previous.name]; current == previous {
		delete(s.children, previous.name)
	}
	s.mu.Unlock()

	timer := time.NewTimer(time.Duration(s.config.RestartDelaySec) * time.Second)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-s.stopCh:
		return nil
	}

	if previous.name == "backend" {
		return s.startChild("backend", s.config.Backend, s.config.WorkDir, s.config.BackendArgs, false)
	}
	return s.startChild("frontend", s.config.Frontend, s.config.FrontendDir, s.config.FrontendArgs, true)
}

func (s *service) stopAll() {
	s.stopOnce.Do(func() {
		close(s.stopCh)
		s.mu.Lock()
		s.stopping = true
		children := make([]*managedProcess, 0, len(s.children))
		for _, child := range s.children {
			children = append(children, child)
		}
		s.mu.Unlock()

		for _, child := range children {
			s.stopChild(child)
		}
	})
}

func (s *service) stopChild(child *managedProcess) {
	if child == nil || child.cmd == nil || child.cmd.Process == nil {
		return
	}

	s.log.Printf("stopping %s (PID %d)", child.name, child.cmd.Process.Pid)
	if err := killProcessTree(child.cmd.Process.Pid); err != nil {
		s.log.Printf("taskkill %s failed: %v; killing process directly", child.name, err)
		_ = child.cmd.Process.Kill()
	}
	select {
	case <-child.done:
	case <-time.After(15 * time.Second):
		s.log.Printf("%s did not stop within 15 seconds", child.name)
	}
}

func killProcessTree(pid int) error {
	return exec.Command("taskkill.exe", "/PID", strconv.Itoa(pid), "/T", "/F").Run()
}

func (s *service) isStopping() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopping
}

func withEnvironment(base []string, overrides map[string]string) []string {
	if len(overrides) == 0 {
		return base
	}

	result := append([]string(nil), base...)
	for key, value := range overrides {
		prefix := key + "="
		replaced := false
		for i, item := range result {
			if strings.HasPrefix(item, prefix) {
				result[i] = prefix + value
				replaced = true
				break
			}
		}
		if !replaced {
			result = append(result, prefix+value)
		}
	}
	return result
}

func quoteWindowsArg(value string) string {
	if value != "" && !strings.ContainsAny(value, " \t\"") {
		return value
	}
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}
