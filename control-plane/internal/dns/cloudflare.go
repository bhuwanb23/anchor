package dns

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

const cloudflareAPIBase = "https://api.cloudflare.com/client/v4"

// Client manages DNS records via the Cloudflare API.
type Client struct {
	token    string
	zoneID   string
	client   *http.Client
	apiBase  string // override for testing
}

// NewClient creates a new Cloudflare DNS client.
func NewClient(token, zoneID string) *Client {
	return &Client{
		token:   token,
		zoneID:  zoneID,
		client:  &http.Client{Timeout: 30 * time.Second},
		apiBase: cloudflareAPIBase,
	}
}

// NewClientWithBase creates a client with a custom API base URL (for testing).
func NewClientWithBase(token, zoneID, apiBase string) *Client {
	return &Client{
		token:   token,
		zoneID:  zoneID,
		client:  &http.Client{Timeout: 30 * time.Second},
		apiBase: apiBase,
	}
}

// Record represents a Cloudflare DNS record.
type Record struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
}

type cfResponse struct {
	Success  bool           `json:"success"`
	Errors   []cfError      `json:"errors"`
	Result   json.RawMessage `json:"result"`
	ResultInfo struct {
		TotalCount int `json:"total_count"`
	} `json:"result_info"`
}

type cfError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// UpsertWildcard creates or updates a wildcard A record:
// *.{subdomain} → IP address
func (c *Client) UpsertWildcard(ctx context.Context, subdomain, ip string) error {
	name := "*." + subdomain
	slog.Info("upserting DNS wildcard record", "name", name, "content", ip)

	// Try to find existing record
	existing, err := c.findRecord(ctx, name, "A")
	if err != nil {
		return fmt.Errorf("find existing record: %w", err)
	}

	if existing != nil {
		return c.updateRecord(ctx, existing.ID, ip)
	}
	return c.createRecord(ctx, name, "A", ip)
}

// DeleteWildcard removes a wildcard DNS record for the given subdomain.
func (c *Client) DeleteWildcard(ctx context.Context, subdomain string) error {
	name := "*." + subdomain
	slog.Info("deleting DNS wildcard record", "name", name)

	record, err := c.findRecord(ctx, name, "A")
	if err != nil {
		return fmt.Errorf("find record to delete: %w", err)
	}
	if record == nil {
		slog.Debug("wildcard record not found, nothing to delete", "name", name)
		return nil
	}

	url := fmt.Sprintf("%s/zones/%s/dns_records/%s", c.apiBase, c.zoneID, record.ID)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return fmt.Errorf("create delete request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("delete DNS record: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("cloudflare delete rejected (%d): %s", resp.StatusCode, string(body))
	}

	return nil
}

// GetRecord returns a DNS record by name and type, or nil if not found.
func (c *Client) GetRecord(ctx context.Context, name, recordType string) (*Record, error) {
	return c.findRecord(ctx, name, recordType)
}

func (c *Client) findRecord(ctx context.Context, name, recordType string) (*Record, error) {
	url := fmt.Sprintf("%s/zones/%s/dns_records?name=%s&type=%s", c.apiBase, c.zoneID, name, recordType)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query DNS records: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("cloudflare query rejected (%d): %s", resp.StatusCode, string(body))
	}

	var cfResp cfResponse
	if err := json.NewDecoder(resp.Body).Decode(&cfResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	if !cfResp.Success {
		return nil, fmt.Errorf("cloudflare API error: %v", cfResp.Errors)
	}

	var records []Record
	if err := json.Unmarshal(cfResp.Result, &records); err != nil {
		return nil, fmt.Errorf("decode records: %w", err)
	}

	if len(records) == 0 {
		return nil, nil
	}
	return &records[0], nil
}

func (c *Client) createRecord(ctx context.Context, name, recordType, content string) error {
	body := map[string]interface{}{
		"type":    recordType,
		"name":    name,
		"content": content,
		"ttl":     1, // auto
		"proxied": false,
	}

	data, _ := json.Marshal(body)
	url := fmt.Sprintf("%s/zones/%s/dns_records", c.apiBase, c.zoneID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("create DNS record: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("cloudflare create rejected (%d): %s", resp.StatusCode, string(body))
	}

	var cfResp cfResponse
	if err := json.NewDecoder(resp.Body).Decode(&cfResp); err != nil {
		return fmt.Errorf("decode create response: %w", err)
	}
	if !cfResp.Success {
		return fmt.Errorf("cloudflare create error: %v", cfResp.Errors)
	}

	return nil
}

func (c *Client) updateRecord(ctx context.Context, recordID, content string) error {
	body := map[string]interface{}{
		"content": content,
		"ttl":     1,
		"proxied": false,
	}

	data, _ := json.Marshal(body)
	url := fmt.Sprintf("%s/zones/%s/dns_records/%s", c.apiBase, c.zoneID, recordID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, url, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("update DNS record: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("cloudflare update rejected (%d): %s", resp.StatusCode, string(body))
	}

	var cfResp cfResponse
	if err := json.NewDecoder(resp.Body).Decode(&cfResp); err != nil {
		return fmt.Errorf("decode update response: %w", err)
	}
	if !cfResp.Success {
		return fmt.Errorf("cloudflare update error: %v", cfResp.Errors)
	}

	return nil
}
