package models

type PlateRequirement struct {
	FilamentID int     `yaml:"filament_id,omitempty" json:"filament_id,omitempty"`
	Name       string  `yaml:"name,omitempty" json:"name,omitempty"`
	Material   string  `yaml:"material,omitempty" json:"material,omitempty"`
	Color      string  `yaml:"color,omitempty" json:"color,omitempty"`
	Amount     float64 `yaml:"amount" json:"amount"`
}

type Plate struct {
	Name              string             `yaml:"name"`
	Status            string             `yaml:"status"`                        // "todo", "in-progress", "completed"
	Printer           string             `yaml:"printer,omitempty"`             // printer name when in-progress
	StartedAt         string             `yaml:"started_at,omitempty"`          // RFC3339 timestamp when printing started
	EstimatedDuration string             `yaml:"estimated_duration,omitempty"`  // e.g. "6h25m"
	Needs             []PlateRequirement `yaml:"needs"`
}

func (p *Plate) DefaultStatus() {
	if p.Status == "" {
		p.Status = "todo"
	}
}

type Project struct {
	Name   string  `yaml:"name"`
	Status string  `yaml:"status"` // "todo", "in-progress", "completed"
	Plates []Plate `yaml:"plates"`
}

func (p *Project) DefaultStatus() {
	if p.Status == "" {
		p.Status = "todo"
	}
	for i := range p.Plates {
		p.Plates[i].DefaultStatus()
	}
}

type PlanFile struct {
	Assembly string    `yaml:"assembly,omitempty"`
	Projects []Project `yaml:"projects"`
}

func (p *PlanFile) DefaultStatus() {
	for i := range p.Projects {
		p.Projects[i].DefaultStatus()
	}
}
