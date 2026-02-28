package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	DirName  = ".migrator"
	PIDFile  = "migrator.pid"
	LogFile  = "migrator.log"
)

// DataDir returns ~/.migrator, creating it if needed.
func DataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, DirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// PIDPath returns the path to the PID file.
func PIDPath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, PIDFile), nil
}

// LogPath returns the path to the log file.
func LogPath() (string, error) {
	dir, err := DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, LogFile), nil
}

// WritePID writes the current process PID to the PID file.
func WritePID() error {
	path, err := PIDPath()
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o644)
}

// RemovePID removes the PID file.
func RemovePID() {
	path, err := PIDPath()
	if err != nil {
		return
	}
	os.Remove(path) //nolint:errcheck
}

// ReadPID reads the PID from the PID file. Returns 0 if not found.
func ReadPID() (int, error) {
	path, err := PIDPath()
	if err != nil {
		return 0, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("corrupt PID file: %w", err)
	}
	return pid, nil
}

// IsRunning checks if the daemon process is alive.
func IsRunning() (int, bool) {
	pid, err := ReadPID()
	if err != nil || pid == 0 {
		return 0, false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return pid, false
	}
	// Signal 0 checks if process exists without sending a signal.
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		RemovePID()
		return pid, false
	}
	return pid, true
}

// Background re-execs the current binary with _MIGRATOR_DAEMON=1 set,
// detaching stdin/stdout/stderr so the parent can exit.
func Background(args []string) (int, error) {
	exe, err := os.Executable()
	if err != nil {
		return 0, fmt.Errorf("resolve executable: %w", err)
	}

	logPath, err := LogPath()
	if err != nil {
		return 0, err
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, fmt.Errorf("open log file: %w", err)
	}

	cmd := exec.Command(exe, args...)
	cmd.Env = append(os.Environ(), "_MIGRATOR_DAEMON=1")
	cmd.Stdin = nil
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return 0, fmt.Errorf("start daemon: %w", err)
	}
	logFile.Close()

	return cmd.Process.Pid, nil
}

// IsDaemonProcess returns true if running as the backgrounded daemon child.
func IsDaemonProcess() bool {
	return os.Getenv("_MIGRATOR_DAEMON") == "1"
}

// Stop sends SIGTERM to the daemon and waits for it to exit.
func Stop() error {
	pid, alive := IsRunning()
	if !alive {
		return fmt.Errorf("daemon is not running")
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("send SIGTERM: %w", err)
	}
	// Wait up to 30s for exit.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			RemovePID()
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	// Force kill.
	_ = proc.Signal(syscall.SIGKILL)
	RemovePID()
	return nil
}

// Status describes the current daemon state.
type Status struct {
	Running bool   `json:"running"`
	PID     int    `json:"pid,omitempty"`
	APIAddr string `json:"api_addr,omitempty"`
}

// StatusInfo returns the daemon's current status and reads config if available.
func StatusInfo(port int) Status {
	pid, alive := IsRunning()
	if !alive {
		return Status{Running: false}
	}
	return Status{
		Running: true,
		PID:     pid,
		APIAddr: fmt.Sprintf("http://localhost:%d", port),
	}
}

// JobRequest is sent by CLI commands to the daemon to start a pipeline operation.
type JobRequest struct {
	Action  string          `json:"action"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// ClonePayload holds parameters for a clone job.
type ClonePayload struct {
	SourceURI   string `json:"source_uri"`
	DestURI     string `json:"dest_uri"`
	Follow      bool   `json:"follow"`
	Resume      bool   `json:"resume"`
	SlotName    string `json:"slot_name,omitempty"`
	Publication string `json:"publication,omitempty"`
	Workers     int    `json:"workers,omitempty"`
}

// FollowPayload holds parameters for a follow job.
type FollowPayload struct {
	SourceURI   string `json:"source_uri"`
	DestURI     string `json:"dest_uri"`
	StartLSN    string `json:"start_lsn,omitempty"`
	SlotName    string `json:"slot_name,omitempty"`
	Publication string `json:"publication,omitempty"`
}

// SwitchoverPayload holds parameters for a switchover job.
type SwitchoverPayload struct {
	SourceURI   string `json:"source_uri"`
	DestURI     string `json:"dest_uri"`
	SlotName    string `json:"slot_name,omitempty"`
	Publication string `json:"publication,omitempty"`
	TimeoutSec  int    `json:"timeout_sec,omitempty"`
}

// JobResponse is returned after submitting a job.
type JobResponse struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}
