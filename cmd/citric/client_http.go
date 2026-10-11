package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type critClient struct {
	cfg  *ClientConfig
	http *http.Client
}

func newCritClient() (*critClient, error) {
	cfg, err := loadClientConfig()
	if err != nil {
		return nil, err
	}
	return &critClient{cfg: cfg, http: &http.Client{}}, nil
}

func (c *critClient) checkSetup() error {
	if c.cfg.Server == "" || c.cfg.Token == "" {
		return fmt.Errorf("not configured — run 'citric auth setup' first")
	}
	return nil
}

func (c *critClient) request(method, path string, body interface{}) (*http.Response, error) {
	if err := c.checkSetup(); err != nil {
		return nil, err
	}
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.cfg.Server+path, bodyReader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	return c.http.Do(req)
}

func (c *critClient) get(path string) (*http.Response, error) {
	return c.request("GET", path, nil)
}

func (c *critClient) getAccept(path, accept string) (*http.Response, error) {
	if err := c.checkSetup(); err != nil {
		return nil, err
	}
	req, err := http.NewRequest("GET", c.cfg.Server+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	req.Header.Set("Accept", accept)
	return c.http.Do(req)
}

func (c *critClient) post(path string, body interface{}) (*http.Response, error) {
	return c.request("POST", path, body)
}

// postNoAuth sends a POST without requiring a token (used during auth setup).
func (c *critClient) postNoAuth(path string, body interface{}) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewReader(b)
	}
	req, err := http.NewRequest("POST", c.cfg.Server+path, bodyReader)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.http.Do(req)
}

// decodeOK decodes a successful JSON response. Returns error for non-2xx.
func decodeOK(resp *http.Response, dst interface{}) error {
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server error %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	if dst == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}

// expectOK checks for a success status code with no body to decode.
func expectOK(resp *http.Response) error {
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("server error %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}
