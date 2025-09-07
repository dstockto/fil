package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/dstockto/fil/models"
)

var ErrSpoolNotFound = fmt.Errorf("no spool found")

type Client struct {
	base       string // base API endpoint
	httpClient http.Client
}

type SpoolFilter func(models.FindSpool) bool

func (c Client) FindSpoolsByName(name string, filter SpoolFilter, query map[string]string) ([]models.FindSpool, error) {
	endpoint := c.base + "/api/v1/spool"
	sort := "location:asc,remaining_weight:asc,filament.name:asc,id:desc"
	trimmedName := strings.TrimSpace(name)

	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid base url: %w", err)
	}
	q := u.Query()
	q.Set("sort", sort)
	q.Set("limit", "1000")

	for k, v := range query {
		switch k {
		case "manufacturer":
			q.Set("filament.vendor.name", v)
		case "allow_archived":
			q.Set("allow_archived", "true")
		case "location":
			q.Set("location", v)
		default:
			fmt.Printf("unknown query param: %s\n", k)
		}
	}

	// Only filter by name if it's not a wildcard
	if trimmedName != "*" {
		q.Set("filament.name", trimmedName)
	}
	u.RawQuery = q.Encode()

	resp, err := c.httpClient.Get(u.String())
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() {
		closeErr := resp.Body.Close()
		if closeErr != nil {
			fmt.Printf("failed to close response body: %v\n", closeErr)
		}
	}()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("api error: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var out []models.FindSpool
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if filter != nil {
		out = c.filterSpools(out, filter)
	}
	return out, nil
}

func (c Client) filterSpools(spools []models.FindSpool, filter SpoolFilter) []models.FindSpool {
	var filtered []models.FindSpool
	for _, s := range spools {
		if filter(s) {
			filtered = append(filtered, s)
		}
	}
	spools = filtered
	return spools
}

func (c Client) FindSpoolsById(id int) (*models.FindSpool, error) {
	endpoint := c.base + "/api/v1/spool/%d"
	endpoint = fmt.Sprintf(endpoint, id)

	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid base url: %w", err)
	}

	resp, err := c.httpClient.Do(&http.Request{
		Method: http.MethodGet,
		URL:    u,
	})
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() {
		closeErr := resp.Body.Close()
		if closeErr != nil {
			fmt.Printf("failed to close response body: %v\n", closeErr)
		}
	}()

	if resp.StatusCode == http.StatusNotFound {
		// No spools found, but don't return an error
		return nil, ErrSpoolNotFound
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("api error: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var out models.FindSpool
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &out, nil
}

func (c Client) UseFilament(spoolId int, amount float64) error {
	endpoint := c.base + "/api/v1/spool/%d/use"
	body := map[string]any{
		"use_weight": amount,
	}
	endpoint = fmt.Sprintf(endpoint, spoolId)

	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("invalid base url: %w", err)
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("failed to marshal body: %w", err)
	}
	bytesReader := strings.NewReader(string(jsonBody))

	// send the PUT request
	req, err := http.NewRequest(http.MethodPut, u.String(), bytesReader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() {
		closeErr := resp.Body.Close()
		if closeErr != nil {
			fmt.Printf("failed to close response body: %v\n", closeErr)
		}
	}()
	if resp.StatusCode == http.StatusNotFound {
		return ErrSpoolNotFound
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("api error: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	return nil
}

func (c Client) MoveSpool(spoolId int, to string) error {
	if to == "<empty>" {
		to = ""
	}

	body := map[string]any{
		"location": to,
	}

	return c.PatchSpool(spoolId, body)
}

func (c Client) PatchSpool(spoolId int, updates map[string]any) error {
	endpoint := c.base + "/api/v1/spool/%d"
	endpoint = fmt.Sprintf(endpoint, spoolId)
	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("invalid base url: %w", err)
	}

	jsonBody, err := json.Marshal(updates)
	if err != nil {
		return fmt.Errorf("failed to marshal body: %w", err)
	}
	bodyBuffer := bytes.NewBuffer(jsonBody)
	req, err := http.NewRequest(http.MethodPatch, u.String(), bodyBuffer)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() {
		closeErr := resp.Body.Close()
		if closeErr != nil {
			fmt.Printf("failed to close response body: %v\n", closeErr)
		}
	}()

	if resp.StatusCode == http.StatusNotFound {
		return ErrSpoolNotFound
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("api error: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	return nil
}

func (c Client) ArchiveSpool(spoolId int) error {
	body := map[string]any{
		"archived": true,
		"location": "",
	}
	return c.PatchSpool(spoolId, body)
}

func NewClient(base string) *Client {
	return &Client{
		base:       base,
		httpClient: http.Client{},
	}
}
