package client

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// SSHClient handles SSH-over-TLS connections to Vers VMs.
// Vers VMs are reachable via SSH tunneled through TLS using
// `openssl s_client` as a ProxyCommand.
type SSHClient struct {
	KeyPath string
	VMID    string
	Host    string
}

// NewSSHClient creates a new SSH client for a VM.
// It writes the private key to a temp file.
func NewSSHClient(vmID, privateKey string) (*SSHClient, error) {
	keyDir := filepath.Join(os.TempDir(), "vers-tf-ssh-keys")
	if err := os.MkdirAll(keyDir, 0o700); err != nil {
		return nil, fmt.Errorf("create key directory: %w", err)
	}

	keyPath := filepath.Join(keyDir, fmt.Sprintf("vers-%s.pem", vmID[:min(12, len(vmID))]))
	if err := os.WriteFile(keyPath, []byte(privateKey), 0o600); err != nil {
		return nil, fmt.Errorf("write SSH key: %w", err)
	}

	return &SSHClient{
		KeyPath: keyPath,
		VMID:    vmID,
		Host:    fmt.Sprintf("%s.vm.vers.sh", vmID),
	}, nil
}

// sshBaseArgs returns the base SSH arguments for connecting to the VM.
func (s *SSHClient) sshBaseArgs() []string {
	return []string{
		"-i", s.KeyPath,
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"-o", "ConnectTimeout=30",
		"-o", "ServerAliveInterval=15",
		"-o", "ServerAliveCountMax=4",
		"-o", fmt.Sprintf("ProxyCommand=openssl s_client -connect %s:443 -servername %s -quiet 2>/dev/null", s.Host, s.Host),
		fmt.Sprintf("root@%s", s.Host),
	}
}

// Exec runs a command on the VM and returns stdout.
func (s *SSHClient) Exec(command string) (string, error) {
	args := append(s.sshBaseArgs(), command)
	cmd := exec.Command("ssh", args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("SSH exec failed (exit %d): %s\nstderr: %s",
			cmd.ProcessState.ExitCode(), err, stderr.String())
	}

	return stdout.String(), nil
}

// ExecWithTimeout runs a command on the VM with a timeout.
func (s *SSHClient) ExecWithTimeout(command string, timeout time.Duration) (string, error) {
	args := append(s.sshBaseArgs(), command)
	cmd := exec.Command("ssh", args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("SSH start failed: %w", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err != nil {
			return stdout.String(), fmt.Errorf("SSH exec failed: %s\nstderr: %s", err, stderr.String())
		}
		return stdout.String(), nil
	case <-time.After(timeout):
		cmd.Process.Kill()
		return stdout.String(), fmt.Errorf("SSH command timed out after %s", timeout)
	}
}

// WriteFile writes content to a file on the VM using base64 encoding
// to safely transport arbitrary content.
func (s *SSHClient) WriteFile(remotePath, content string) error {
	// Ensure parent directory exists
	dir := filepath.Dir(remotePath)
	if dir != "." && dir != "/" {
		if _, err := s.Exec(fmt.Sprintf("mkdir -p '%s'", shellEscape(dir))); err != nil {
			return fmt.Errorf("mkdir on VM: %w", err)
		}
	}

	// Use base64 to safely transfer arbitrary content
	encoded := base64.StdEncoding.EncodeToString([]byte(content))
	cmd := fmt.Sprintf("echo '%s' | base64 -d > '%s'", encoded, shellEscape(remotePath))
	if _, err := s.Exec(cmd); err != nil {
		return fmt.Errorf("write file %s on VM: %w", remotePath, err)
	}
	return nil
}

// UploadFile copies a local file to the VM via SSH stdin pipe.
func (s *SSHClient) UploadFile(localPath, remotePath string) error {
	content, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("read local file %s: %w", localPath, err)
	}
	return s.WriteFile(remotePath, string(content))
}

// ReadFile reads a file from the VM.
func (s *SSHClient) ReadFile(remotePath string) (string, error) {
	return s.Exec(fmt.Sprintf("cat '%s'", shellEscape(remotePath)))
}

// WaitReachable polls until the VM is reachable via SSH.
func (s *SSHClient) WaitReachable(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		out, err := s.ExecWithTimeout("echo ready", 15*time.Second)
		if err == nil && strings.TrimSpace(out) == "ready" {
			return nil
		}
		lastErr = err
		time.Sleep(3 * time.Second)
	}
	if lastErr != nil {
		return fmt.Errorf("VM %s not reachable via SSH after %s: %w", s.VMID, timeout, lastErr)
	}
	return fmt.Errorf("VM %s not reachable via SSH after %s", s.VMID, timeout)
}

// Cleanup removes the temporary key file.
func (s *SSHClient) Cleanup() {
	os.Remove(s.KeyPath)
}

func shellEscape(s string) string {
	return strings.ReplaceAll(s, "'", "'\\''")
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
