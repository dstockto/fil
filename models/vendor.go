package models

import "time"

// Vendor represents a filament vendor/manufacturer in Spoolman.
type Vendor struct {
	Id         int       `json:"id"`
	Registered time.Time `json:"registered"`
	Name       string    `json:"name"`
}

// CreateFilamentRequest holds the fields needed to create a new filament definition.
type CreateFilamentRequest struct {
	Name        string  `json:"name"`
	VendorId    int     `json:"vendor_id"`
	Material    string  `json:"material"`
	Price       float64 `json:"price,omitempty"`
	Density     float64 `json:"density"`
	Diameter    float64 `json:"diameter"`
	Weight      float64 `json:"weight"`
	SpoolWeight float64 `json:"spool_weight"`
	ColorHex    string  `json:"color_hex"`
}

// FilamentResponse represents the API response after creating a filament.
type FilamentResponse struct {
	Id       int     `json:"id"`
	Name     string  `json:"name"`
	Material string  `json:"material"`
	Vendor   Vendor  `json:"vendor"`
	ColorHex string  `json:"color_hex"`
	Weight   float64 `json:"weight"`
	Density  float64 `json:"density"`
	Diameter float64 `json:"diameter"`
	Price    float64 `json:"price"`
}
