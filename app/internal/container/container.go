// Package container manages container lifecycle: create, start, stop, exec, remove.
package container

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/you/lildocker/internal/image"
)

// State represents the lifecycle state of a container
type State string

const (
	StateCreated State = "created"
	StateRunning State = "running"
	StateStopped State = "stopped"
	StateExited  State = "exited"
)

// Config holds all container configuration
type Config struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	ImageID     string            `json:"image_id"`
	ImageName   string            `json:"image_name"`
	Cmd         []string          `json:"cmd"`
	Env         []string          `json:"env"`
	WorkDir     string            `json:"workdir"`
	Hostname    string            `json:"hostname"`
	State       State             `json:"state"`
	PID         int               `json:"pid"`
	CreatedAt   time.Time         `json:"created_at"`
	StartedAt   *time.Time        `json:"started_at,omitempty"`
	FinishedAt  *time.Time        `json:"finished_at,omitempty"`
	ExitCode    int               `json:"exit_code"`
	Ports       []PortMapping     `json:"ports"`
	Volumes     []VolumeMount     `json:"volumes"`
	NetworkID   string            `json:"network_id"`
	IPAddress   string            `json:"ip_address"`
	MemLimit    int64             `json:"mem_limit"`   // bytes, 0 = unlimited
	CPUShares   int64             `json:"cpu_shares"`  // relative weight
	Labels      map[string]string `json:"labels"`
}

// PortMapping maps a host port to a container port
type PortMapping struct {
	HostPort      int    `json:"host_port"`
	ContainerPort int    `json:"container_port"`
	Protocol      string `json:"protocol"` // tcp or udp
}

// VolumeMount maps a host path into the container
type VolumeMount struct {
	HostPath      string `json:"host_path"`
	ContainerPath string `json:"container_path"`
	ReadOnly      bool   `json:"read_only"`
}

// RunOptions passed by the user via `lildocker run`
type RunOptions struct {
	Name      string
	Detach    bool
	Ports     []PortMapping
	Volumes   []VolumeMount
	Env       []string
	Cmd       []string
	MemLimit  int64
	CPUShares int64
	NetworkID string
	Hostname  string
}

// Manager manages container state on disk and in-process
type Manager struct {
	store *image.Store
}

func NewManager(store *image.Store) *Manager {
	return &Manager{store: store}
}

func (m *Manager) ContainerDir(id string) string {
	return m.store.ContainerDir(id)
}

// Create sets up the container's directory structure and config without starting it
func (m *Manager) Create(manifest *image.Manifest, opts RunOptions) (*Config, error) {
	id := newID()

	name := opts.Name
	if name == "" {
		name = id[:8]
	}
	hostname := opts.Hostname
	if hostname == "" {
		hostname = name
	}

	// Merge env: image defaults first, then user overrides
	env := append(manifest.Env, opts.Env...)

	// Determine the command to run
	cmd := opts.Cmd
	if len(cmd) == 0 {
		cmd = manifest.Cmd
	}
	if len(cmd) == 0 {
		return nil, fmt.Errorf("no command specified and image has no default CMD")
	}

	cfg := &Config{
		ID:        id,
		Name:      name,
		ImageID:   manifest.ID,
		ImageName: manifest.Name + ":" + manifest.Tag,
		Cmd:       cmd,
		Env:       env,
		WorkDir:   manifest.WorkDir,
		Hostname:  hostname,
		State:     StateCreated,
		CreatedAt: time.Now(),
		Ports:     opts.Ports,
		Volumes:   opts.Volumes,
		MemLimit:  opts.MemLimit,
		CPUShares: opts.CPUShares,
		NetworkID: opts.NetworkID,
		Labels:    manifest.Labels,
	}

	// Create container directory layout
	cDir := m.ContainerDir(id)
	for _, sub := range []string{"upper", "work", "merged", "logs"} {
		if err := os.MkdirAll(filepath.Join(cDir, sub), 0755); err != nil {
			return nil, fmt.Errorf("create container dir: %w", err)
		}
	}

	// Write the config
	if err := m.saveConfig(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Start mounts the overlay filesystem and launches the container process
func (m *Manager) Start(cfg *Config, manifest *image.Manifest) error {
	cDir := m.ContainerDir(cfg.ID)
	mergedDir := filepath.Join(cDir, "merged")
	upperDir := filepath.Join(cDir, "upper")
	workDir := filepath.Join(cDir, "work")

	// Build list of read-only lower layers from the image
	var lowerDirs []string
	imgDir := m.store.ImageDir(manifest.ID)
	for _, layerID := range manifest.Layers {
		lowerDirs = append(lowerDirs, filepath.Join(imgDir, "layers", layerID))
	}

	// Mount overlayfs
	if err := m.mountOverlay(lowerDirs, upperDir, workDir, mergedDir); err != nil {
		return fmt.Errorf("mount overlay: %w", err)
	}

	// Bind-mount host volumes into the merged view
	for _, vol := range cfg.Volumes {
		dst := filepath.Join(mergedDir, vol.ContainerPath)
		if err := os.MkdirAll(dst, 0755); err != nil {
			return err
		}
		flags := uintptr(syscall.MS_BIND)
		if vol.ReadOnly {
			flags |= syscall.MS_RDONLY
		}
		if err := syscall.Mount(vol.HostPath, dst, "", flags, ""); err != nil {
			return fmt.Errorf("bind mount %s: %w", vol.HostPath, err)
		}
	}

	// Re-exec ourselves as the container child process
	self, err := os.Executable()
	if err != nil {
		return err
	}

	// Serialize config for the child process
	cfgJSON, _ := json.Marshal(cfg)
	logFile := filepath.Join(cDir, "logs", "stdout.log")

	args := append([]string{"_child", string(cfgJSON), mergedDir, logFile}, cfg.Cmd...)
	cmd := exec.Command(self, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// New namespaces for isolation
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUTS |
			syscall.CLONE_NEWPID |
			syscall.CLONE_NEWNS |
			syscall.CLONE_NEWNET |
			syscall.CLONE_NEWIPC,
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start container: %w", err)
	}

	cfg.PID = cmd.Process.Pid
	cfg.State = StateRunning
	now := time.Now()
	cfg.StartedAt = &now

	if err := m.saveConfig(cfg); err != nil {
		return err
	}

	// Set up cgroups for resource limits
	if err := m.setupCgroups(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "warning: cgroup setup failed: %v\n", err)
	}

	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			cfg.ExitCode = exitErr.ExitCode()
		}
	}

	// Cleanup
	cfg.State = StateExited
	finished := time.Now()
	cfg.FinishedAt = &finished
	m.saveConfig(cfg)

	// Unmount overlay
	syscall.Unmount(mergedDir, 0)

	return nil
}

// Child is called from the re-exec inside the new namespaces.
// It sets up the container environment and exec's the user command.
func Child(cfgJSON, rootDir, logFile string, cmd []string) error {
	var cfg Config
	if err := json.Unmarshal([]byte(cfgJSON), &cfg); err != nil {
		return fmt.Errorf("parse container config: %w", err)
	}

	// Set hostname
	if err := syscall.Sethostname([]byte(cfg.Hostname)); err != nil {
		return fmt.Errorf("sethostname: %w", err)
	}

	// Mount /proc inside the container
	if err := syscall.Mount("proc", filepath.Join(rootDir, "proc"), "proc", 0, ""); err != nil {
		fmt.Fprintf(os.Stderr, "warning: mount proc: %v\n", err)
	}

	// Mount /dev/pts
	if err := os.MkdirAll(filepath.Join(rootDir, "dev", "pts"), 0755); err == nil {
		syscall.Mount("devpts", filepath.Join(rootDir, "dev", "pts"), "devpts", 0, "")
	}

	// chroot into the container rootfs
	if err := syscall.Chroot(rootDir); err != nil {
		return fmt.Errorf("chroot: %w", err)
	}
	if err := os.Chdir("/"); err != nil {
		return fmt.Errorf("chdir: %w", err)
	}

	// Set working directory
	if cfg.WorkDir != "" {
		if err := os.MkdirAll(cfg.WorkDir, 0755); err == nil {
			os.Chdir(cfg.WorkDir)
		}
	}

	// Set up log file (tee stdout/stderr to it)
	if logFile != "" {
		// We write logs from the parent side; inside the container we just exec normally
	}

	// Build env
	env := cfg.Env

	// Exec the command (replaces this process)
	binary, err := exec.LookPath(cmd[0])
	if err != nil {
		// Try absolute path
		binary = cmd[0]
	}

	return syscall.Exec(binary, cmd, env)
}

// Stop sends SIGTERM (then SIGKILL after timeout) to the container process
func (m *Manager) Stop(id string) error {
	cfg, err := m.LoadConfig(id)
	if err != nil {
		return err
	}
	if cfg.State != StateRunning {
		return fmt.Errorf("container %s is not running", id[:8])
	}

	proc, err := os.FindProcess(cfg.PID)
	if err != nil {
		return err
	}

	// SIGTERM first
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return err
	}

	// Give it 10 seconds, then SIGKILL
	done := make(chan error, 1)
	go func() {
		_, err := proc.Wait()
		done <- err
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		proc.Kill()
	}

	cfg.State = StateStopped
	finished := time.Now()
	cfg.FinishedAt = &finished
	return m.saveConfig(cfg)
}

// Exec runs a command in an already-running container by entering its namespaces
func (m *Manager) Exec(id string, cmd []string) error {
	cfg, err := m.LoadConfig(id)
	if err != nil {
		return err
	}
	if cfg.State != StateRunning {
		return fmt.Errorf("container %s is not running (state: %s)", id[:8], cfg.State)
	}

	// nsenter enters the namespaces of the running container process
	nsenterArgs := []string{
		fmt.Sprintf("--target=%d", cfg.PID),
		"--mount", "--uts", "--ipc", "--net", "--pid",
		"--",
	}
	nsenterArgs = append(nsenterArgs, cmd...)

	c := exec.Command("nsenter", nsenterArgs...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

// Remove deletes a stopped container's data
func (m *Manager) Remove(id string) error {
	cfg, err := m.LoadConfig(id)
	if err != nil {
		return err
	}
	if cfg.State == StateRunning {
		return fmt.Errorf("cannot remove running container %s; stop it first", id[:8])
	}
	return os.RemoveAll(m.ContainerDir(id))
}

// ListContainers returns all containers
func (m *Manager) ListContainers() ([]*Config, error) {
	entries, err := os.ReadDir(m.store.ContainersDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var configs []*Config
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		cfg, err := m.LoadConfig(e.Name())
		if err != nil {
			continue
		}
		configs = append(configs, cfg)
	}
	return configs, nil
}

// Logs tails the container's log file
func (m *Manager) Logs(id string) error {
	cfg, err := m.LoadConfig(id)
	if err != nil {
		return err
	}
	logFile := filepath.Join(m.ContainerDir(cfg.ID), "logs", "stdout.log")
	data, err := os.ReadFile(logFile)
	if err != nil {
		return fmt.Errorf("no logs for container %s", id[:8])
	}
	fmt.Print(string(data))
	return nil
}

// --- cgroups ---

func (m *Manager) setupCgroups(cfg *Config) error {
	cgroupBase := fmt.Sprintf("/sys/fs/cgroup/lildocker_%s", cfg.ID[:8])

	// Memory limit
	if cfg.MemLimit > 0 {
		memPath := cgroupBase + "_mem"
		os.MkdirAll(memPath, 0755)
		os.WriteFile(filepath.Join(memPath, "memory.limit_in_bytes"),
			[]byte(strconv.FormatInt(cfg.MemLimit, 10)), 0644)
		os.WriteFile(filepath.Join(memPath, "cgroup.procs"),
			[]byte(strconv.Itoa(cfg.PID)), 0644)
	}

	// CPU shares
	if cfg.CPUShares > 0 {
		cpuPath := cgroupBase + "_cpu"
		os.MkdirAll(cpuPath, 0755)
		os.WriteFile(filepath.Join(cpuPath, "cpu.shares"),
			[]byte(strconv.FormatInt(cfg.CPUShares, 10)), 0644)
		os.WriteFile(filepath.Join(cpuPath, "cgroup.procs"),
			[]byte(strconv.Itoa(cfg.PID)), 0644)
	}

	return nil
}

// --- overlayfs ---

func (m *Manager) mountOverlay(lowerDirs []string, upperDir, workDir, mergedDir string) error {
	// overlayfs needs at least one lower dir
	if len(lowerDirs) == 0 {
		// If no lower layers (scratch image), just use an empty tmpfs
		return syscall.Mount("tmpfs", mergedDir, "tmpfs", 0, "")
	}

	opts := image.OverlayOptions(lowerDirs, upperDir, workDir)
	return syscall.Mount("overlay", mergedDir, "overlay", 0, opts)
}

// --- config persistence ---

func (m *Manager) saveConfig(cfg *Config) error {
	cDir := m.ContainerDir(cfg.ID)
	if err := os.MkdirAll(cDir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(cDir, "config.json"), data, 0644)
}

func (m *Manager) LoadConfig(id string) (*Config, error) {
	// Support short IDs: scan all containers and match prefix
	full, err := m.resolveID(id)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(m.ContainerDir(full), "config.json"))
	if err != nil {
		return nil, fmt.Errorf("container %s not found", id)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (m *Manager) resolveID(id string) (string, error) {
	entries, _ := os.ReadDir(m.store.ContainersDir())
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), id) {
			return e.Name(), nil
		}
	}
	return "", fmt.Errorf("no container matching %q", id)
}

// --- utilities ---

func newID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
