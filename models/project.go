package models

type PlateRequirement struct {
	FilamentID int     `yaml:"filament_id,omitempty"`
	Name       string  `yaml:"name,omitempty"`
	Material   string  `yaml:"material,omitempty"`
	Color      string  `yaml:"color,omitempty"`
	Amount     float64 `yaml:"amount"`
}

type Plate struct {
	Name   string             `yaml:"name"`
	Status string             `yaml:"status"` // "todo", "in-progress", "completed"
	Needs  []PlateRequirement `yaml:"needs"`
}

type Project struct {
	Name   string  `yaml:"name"`
	Status string  `yaml:"status"` // "todo", "in-progress", "completed"
	Plates []Plate `yaml:"plates"`
}

type PlanFile struct {
	Projects []Project `yaml:"projects"`
}
