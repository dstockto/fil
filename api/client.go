package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/dstockto/fil/models"
)

type Client struct {
	base       string // base API endpoint
	httpClient http.Client
}

type SpoolFilter func(models.FindSpool) bool

func (c Client) FindSpoolsByName(name string, filter SpoolFilter) ([]models.FindSpool, error) {
	endpoint := c.base + "/api/v1/spool"
	sort := "remaining_weight:asc,filament.name:asc,id:desc"
	trimmedName := strings.TrimSpace(name)

	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid base url: %w", err)
	}
	q := u.Query()
	q.Set("sort", sort)
	q.Set("filament.name", trimmedName)
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

func NewClient(base string) *Client {
	return &Client{
		base:       base,
		httpClient: http.Client{},
	}
}
