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
	OriginalLocation string    `yaml:"original_location,omitempty"`
	Projects         []Project `yaml:"projects"`
}

func (p *PlanFile) DefaultStatus() {
	for i := range p.Projects {
		p.Projects[i].DefaultStatus()
	}
}
