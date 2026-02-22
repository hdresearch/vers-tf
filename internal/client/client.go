// Package client provides a Go HTTP client for the Vers API (vers.sh).
// It covers VM lifecycle (create, list, delete, branch, commit, restore, state)
// and SSH key retrieval.
package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const DefaultBaseURL = "https://api.vers.sh/api/v1"

// Client is a Vers API client.
type Client struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

// New creates a new Vers API client.
func New(apiKey, baseURL string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	return &Client{
		BaseURL: baseURL,
		APIKey:  apiKey,
		HTTPClient: &http.Client{
			Timeout: 5 * time.Minute, // some operations (create, commit) are slow
		},
	}
}

// VM represents a Vers virtual machine.
type VM struct {
	VMID      string `json:"vm_id"`
	OwnerID   string `json:"owner_id,omitempty"`
	State     string `json:"state"`
	CreatedAt string `json:"created_at"`
}

// NewVMResponse is returned when creating/branching/restoring a VM.
type NewVMResponse struct {
	VMID string `json:"vm_id"`
}

// BranchResponse handles both API response shapes.
type BranchResponse struct {
	VMID string        `json:"vm_id,omitempty"`
	VMs  []NewVMResponse `json:"vms,omitempty"`
}

// CommitResponse is returned when committing a VM.
type CommitResponse struct {
	CommitID string `json:"commit_id"`
}

// SSHKeyResponse is returned when fetching SSH credentials.
type SSHKeyResponse struct {
	SSHPort       int    `json:"ssh_port"`
	SSHPrivateKey string `json:"ssh_private_key"`
}

// VMConfig is the configuration for creating a new VM.
type VMConfig struct {
	VCPUCount  *int `json:"vcpu_count,omitempty"`
	MemSizeMiB *int `json:"mem_size_mib,omitempty"`
	FSSizeMiB  *int `json:"fs_size_mib,omitempty"`
}

// request is a generic HTTP helper.
func (c *Client) request(method, path string, body interface{}) ([]byte, error) {
	url := c.BaseURL + path

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Vers API %s %s returned %d: %s", method, path, resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// ListVMs returns all VMs owned by the authenticated user.
func (c *Client) ListVMs() ([]VM, error) {
	data, err := c.request("GET", "/vms", nil)
	if err != nil {
		return nil, err
	}
	var vms []VM
	if err := json.Unmarshal(data, &vms); err != nil {
		return nil, fmt.Errorf("decode VMs: %w", err)
	}
	return vms, nil
}

// GetVM returns a specific VM by ID, or nil if not found.
func (c *Client) GetVM(vmID string) (*VM, error) {
	vms, err := c.ListVMs()
	if err != nil {
		return nil, err
	}
	for _, vm := range vms {
		if vm.VMID == vmID {
			return &vm, nil
		}
	}
	return nil, nil
}

// CreateVM creates a new root VM.
func (c *Client) CreateVM(config VMConfig, waitBoot bool) (*NewVMResponse, error) {
	path := "/vm/new_root"
	if waitBoot {
		path += "?wait_boot=true"
	}
	body := map[string]interface{}{
		"vm_config": config,
	}
	data, err := c.request("POST", path, body)
	if err != nil {
		return nil, err
	}
	var resp NewVMResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("decode create response: %w", err)
	}
	return &resp, nil
}

// DeleteVM deletes a VM.
func (c *Client) DeleteVM(vmID string) error {
	_, err := c.request("DELETE", fmt.Sprintf("/vm/%s", vmID), nil)
	return err
}

// BranchVM clones a VM. Returns the new VM ID.
func (c *Client) BranchVM(vmID string) (string, error) {
	data, err := c.request("POST", fmt.Sprintf("/vm/%s/branch", vmID), nil)
	if err != nil {
		return "", err
	}
	var resp BranchResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return "", fmt.Errorf("decode branch response: %w", err)
	}
	// Handle both { vm_id } and { vms: [{ vm_id }] }
	if len(resp.VMs) > 0 {
		return resp.VMs[0].VMID, nil
	}
	if resp.VMID != "" {
		return resp.VMID, nil
	}
	return "", fmt.Errorf("unexpected branch response: %s", string(data))
}

// CommitVM creates a snapshot of a VM.
func (c *Client) CommitVM(vmID string, keepPaused bool) (*CommitResponse, error) {
	path := fmt.Sprintf("/vm/%s/commit", vmID)
	if keepPaused {
		path += "?keep_paused=true"
	}
	data, err := c.request("POST", path, nil)
	if err != nil {
		return nil, err
	}
	var resp CommitResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("decode commit response: %w", err)
	}
	return &resp, nil
}

// RestoreVM restores a VM from a commit.
func (c *Client) RestoreVM(commitID string) (*NewVMResponse, error) {
	body := map[string]string{
		"commit_id": commitID,
	}
	data, err := c.request("POST", "/vm/from_commit", body)
	if err != nil {
		return nil, err
	}
	var resp NewVMResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("decode restore response: %w", err)
	}
	return &resp, nil
}

// UpdateVMState pauses or resumes a VM.
func (c *Client) UpdateVMState(vmID, state string) error {
	body := map[string]string{
		"state": state,
	}
	_, err := c.request("PATCH", fmt.Sprintf("/vm/%s/state", vmID), body)
	return err
}

// GetSSHKey retrieves SSH credentials for a VM.
func (c *Client) GetSSHKey(vmID string) (*SSHKeyResponse, error) {
	data, err := c.request("GET", fmt.Sprintf("/vm/%s/ssh_key", vmID), nil)
	if err != nil {
		return nil, err
	}
	var resp SSHKeyResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("decode SSH key response: %w", err)
	}
	return &resp, nil
}

// WaitForBoot polls until a VM reaches "running" state, with timeout.
func (c *Client) WaitForBoot(vmID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		vm, err := c.GetVM(vmID)
		if err != nil {
			return err
		}
		if vm != nil && vm.State == "running" {
			return nil
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("VM %s did not reach running state within %s", vmID, timeout)
}
