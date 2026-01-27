package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/dstockto/fil/models"
)

var ErrSpoolNotFound = errors.New("no spool found")

type SpoolmanAPI interface {
	FindSpoolsByName(name string, filter SpoolFilter, query map[string]string) ([]models.FindSpool, error)
	GetFilamentById(id int) (*models.FindSpool, error)
	FindSpoolsById(id int) (*models.FindSpool, error)
	UseFilament(spoolId int, amount float64) error
	MoveSpool(spoolId int, to string) error
	PatchSpool(spoolId int, updates map[string]any) error
	ArchiveSpool(spoolId int) error
	GetSettings() (map[string]SettingEntry, error)
	PatchSettings(fields map[string]any) error
	PostSettingObject(key string, obj any) error
}

type Client struct {
	base       string // base API endpoint
	httpClient http.Client
}

type SpoolFilter func(models.FindSpool) bool

func NewClient(base string) *Client {
	return &Client{
		base:       base,
		httpClient: http.Client{},
	}
}

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
		case "material":
			q.Set("filament.material", v)
		case "color":
			q.Set("filament.color_hex", v)
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

func (c Client) GetFilamentById(id int) (*models.FindSpool, error) {
	endpoint := c.base + "/api/v1/filament/%d"
	endpoint = fmt.Sprintf(endpoint, id)

	findUrl, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid base url: %w", err)
	}

	resp, err := c.httpClient.Do(&http.Request{
		Method: http.MethodGet,
		URL:    findUrl,
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
		return nil, errors.New("filament not found")
	}

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("api error: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	// The Spoolman API for /filament/{id} returns a filament object.
	// Our models.FindSpool has a Filament field which matches this structure.
	// We can wrap it or just decode into a struct that matches.
	var out struct {
		Id       int    `json:"id"`
		Name     string `json:"name"`
		Material string `json:"material"`
	}

	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Return a dummy FindSpool with the filament info populated
	res := &models.FindSpool{}
	res.Filament.Id = out.Id
	res.Filament.Name = out.Name
	res.Filament.Material = out.Material

	return res, nil
}

func (c Client) FindSpoolsById(id int) (*models.FindSpool, error) {
	endpoint := c.base + "/api/v1/spool/%d"
	endpoint = fmt.Sprintf(endpoint, id)

	findUrl, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid base url: %w", err)
	}

	resp, err := c.httpClient.Do(&http.Request{
		Method: http.MethodGet,
		URL:    findUrl,
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

// Settings API models and methods added for cleaning locations_spoolorders
// These are appended after existing methods.

// SettingEntry represents a settings value as returned by /api/v1/setting/
// Note: The API returns Value as a JSON string for complex types (arrays/objects),
// so callers may need to json.Unmarshal twice: first into a string, then that
// string into the concrete type.
type SettingEntry struct {
	Value json.RawMessage `json:"value"`
	IsSet bool            `json:"is_set"`
	Type  string          `json:"type"`
}

// GetSettings fetches all settings from the server at /api/v1/setting/
func (c Client) GetSettings() (map[string]SettingEntry, error) {
	endpoint := c.base + "/api/v1/setting/"

	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid base url: %w", err)
	}

	resp, err := c.httpClient.Get(u.String())
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("api error: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var out map[string]SettingEntry
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&out); err != nil {
		return nil, fmt.Errorf("failed to decode settings response: %w", err)
	}

	return out, nil
}

// PatchSettings sends a PATCH to /api/v1/setting/ with the provided fields.
// The body should be a flat object of key -> value, where value matches what the server expects.
// For complex settings that are represented as JSON strings in the API (e.g., arrays/objects),
// pass a Go value (map/slice) and this method will marshal it into a JSON string so that the
// server receives a string containing JSON (to match current behavior of the endpoint).
func (c Client) PatchSettings(fields map[string]any) error {
	endpoint := c.base + "/api/v1/setting/"

	// Marshal fields, converting maps/slices to JSON strings where necessary.
	payload := map[string]any{}
	for k, v := range fields {
		switch vv := v.(type) {
		case string, bool, float64, int, int64, nil:
			payload[k] = vv
		default:
			// For maps/slices/structs: marshal to JSON, then embed as string
			b, err := json.Marshal(v)
			if err != nil {
				return fmt.Errorf("failed to marshal field %s: %w", k, err)
			}
			payload[k] = string(b)
		}
	}

	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal settings patch body: %w", err)
	}

	req, err := http.NewRequest(http.MethodPatch, endpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("api error: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	return nil
}

// PostSettingObject updates a single setting key using POST /api/v1/setting/{key}
// For object/array settings, the API expects the "value" field to be a JSON string
// containing the serialized object, alongside is_set=true and an appropriate type.
// This helper marshals obj to JSON and wraps it as a string in the request body with type "object".
func (c Client) PostSettingObject(key string, obj any) error {
	endpoint := c.base + "/api/v1/setting/" + key

	b, err := json.Marshal(obj)
	if err != nil {
		return fmt.Errorf("failed to marshal setting %s: %w", key, err)
	}

	// First attempt: send wrapper object {value,is_set,type}
	payload := map[string]any{
		"value":  string(b),
		"is_set": true,
		"type":   "object",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal setting payload for %s: %w", key, err)
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	// We need body text possibly for retry decision; don't defer close until after reading
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated {
		_ = resp.Body.Close()
		return nil
	}
	// Read error body and decide whether to retry with raw string
	errBody, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()

	status := resp.StatusCode
	errText := strings.TrimSpace(string(errBody))

	// If server expects a plain string body, it may return 422 with a message like
	// "Input should be a valid string". In that case, retry by sending the JSON string itself.
	if status == http.StatusUnprocessableEntity || strings.Contains(strings.ToLower(errText), "valid string") || strings.Contains(strings.ToLower(errText), "string_type") {
		// Second attempt: send the JSON string as the whole body (as a JSON string literal)
		jsonStringBody, mErr := json.Marshal(string(b))
		if mErr != nil {
			return fmt.Errorf("failed to marshal raw string payload for %s: %w", key, mErr)
		}

		req2, rErr := http.NewRequest(http.MethodPost, endpoint, bytes.NewReader(jsonStringBody))
		if rErr != nil {
			return fmt.Errorf("failed to create request: %w", rErr)
		}
		req2.Header.Set("Content-Type", "application/json")

		resp2, dErr := c.httpClient.Do(req2)
		if dErr != nil {
			return fmt.Errorf("request failed: %w", dErr)
		}
		defer func() { _ = resp2.Body.Close() }()

		if resp2.StatusCode == http.StatusOK || resp2.StatusCode == http.StatusCreated {
			return nil
		}

		b2, _ := io.ReadAll(resp2.Body)
		return fmt.Errorf("api error: status %d: %s", resp2.StatusCode, strings.TrimSpace(string(b2)))
	}

	// Otherwise, return the original error
	return fmt.Errorf("api error: status %d: %s", status, errText)
}
