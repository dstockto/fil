package models

import "time"

type FindSpool struct {
	Id         int       `json:"id"`
	Registered time.Time `json:"registered"`
	FirstUsed  time.Time `json:"first_used"`
	LastUsed   time.Time `json:"last_used"`
	Filament   struct {
		Id         int       `json:"id"`
		Registered time.Time `json:"registered"`
		Name       string    `json:"name"`
		Vendor     struct {
			Id         int       `json:"id"`
			Registered time.Time `json:"registered"`
			Name       string    `json:"name"`
			Extra      struct {
			} `json:"extra"`
		} `json:"vendor"`
		Material            string  `json:"material"`
		Price               float64 `json:"price"`
		Density             float64 `json:"density"`
		Diameter            float64 `json:"diameter"`
		Weight              float64 `json:"weight"`
		SpoolWeight         float64 `json:"spool_weight"`
		ColorHex            string  `json:"color_hex"`
		MultiColorHexes     string  `json:"multi_color_hexes"`
		MultiColorDirection string  `json:"multi_color_direction"`
		Extra               struct {
		} `json:"extra"`
	} `json:"filament"`
	RemainingWeight float64 `json:"remaining_weight"`
	InitialWeight   float64 `json:"initial_weight"`
	SpoolWeight     float64 `json:"spool_weight"`
	UsedWeight      float64 `json:"used_weight"`
	RemainingLength float64 `json:"remaining_length"`
	UsedLength      float64 `json:"used_length"`
	Location        string  `json:"location"`
	Comment         string  `json:"comment"`
	Archived        bool    `json:"archived"`
	Extra           struct {
	} `json:"extra"`
}
