package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jfoltran/pgmanager/internal/metrics"
)

// Client talks to the daemon's HTTP API.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient creates an API client pointing at the given base URL.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

// Ping checks if the daemon is reachable.
func (c *Client) Ping() error {
	resp, err := c.http.Get(c.baseURL + "/api/v1/status")
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// Status fetches the current metrics snapshot.
func (c *Client) Status() (*metrics.Snapshot, error) {
	resp, err := c.http.Get(c.baseURL + "/api/v1/status")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var snap metrics.Snapshot
	if err := json.Unmarshal(body, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

// Logs fetches recent log entries.
func (c *Client) Logs() ([]metrics.LogEntry, error) {
	resp, err := c.http.Get(c.baseURL + "/api/v1/logs")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var entries []metrics.LogEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// SubmitClone submits a clone job to the daemon.
func (c *Client) SubmitClone(payload ClonePayload) (*JobResponse, error) {
	return c.postJob("/api/v1/jobs/clone", payload)
}

// SubmitFollow submits a follow job to the daemon.
func (c *Client) SubmitFollow(payload FollowPayload) (*JobResponse, error) {
	return c.postJob("/api/v1/jobs/follow", payload)
}

// SubmitSwitchover submits a switchover job to the daemon.
func (c *Client) SubmitSwitchover(payload SwitchoverPayload) (*JobResponse, error) {
	return c.postJob("/api/v1/jobs/switchover", payload)
}

// StopJob requests the daemon to stop the current job.
func (c *Client) StopJob() (*JobResponse, error) {
	return c.postJob("/api/v1/jobs/stop", nil)
}

// JobStatus fetches the current job status.
func (c *Client) JobStatus() (map[string]any, error) {
	resp, err := c.http.Get(c.baseURL + "/api/v1/jobs/status")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) postJob(path string, payload any) (*JobResponse, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(data)
	} else {
		body = bytes.NewReader([]byte("{}"))
	}

	resp, err := c.http.Post(c.baseURL+path, "application/json", body)
	if err != nil {
		return nil, fmt.Errorf("cannot reach daemon at %s: %w", c.baseURL, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var jr JobResponse
	if err := json.Unmarshal(respBody, &jr); err != nil {
		return nil, fmt.Errorf("unexpected response: %s", string(respBody))
	}
	return &jr, nil
}
