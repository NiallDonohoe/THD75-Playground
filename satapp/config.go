package satapp

import (
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"time"
)

// Location is the station's position and where it came from.
type Location struct {
	Lat    float64   `json:"lat"`
	Lon    float64   `json:"lon"`
	AltM   float64   `json:"altM"`
	Source string    `json:"source"` // "browser", "map", "locator", "flags" or "demo"
	Set    time.Time `json:"set"`
}

// Config is what d75sat remembers between runs.
type Config struct {
	Location     *Location       `json:"location,omitempty"` // nil until set
	Alerts       map[string]bool `json:"alerts,omitempty"`   // by catalogue number
	AlertMinutes int             `json:"alertMinutes"`       // how long before AOS
	AlertMinEl   float64         `json:"alertMinEl"`         // only passes peaking at least this high
}

// Defaults for alerts.
const (
	DefaultAlertMinutes = 5
	DefaultAlertMinEl   = 10
)

func (c *Config) fill() {
	if c.Alerts == nil {
		c.Alerts = map[string]bool{}
	}
	if c.AlertMinutes <= 0 {
		c.AlertMinutes = DefaultAlertMinutes
	}
	if c.AlertMinEl == 0 {
		c.AlertMinEl = DefaultAlertMinEl
	}
}

// LoadConfig reads the config file; a missing file is an empty config.
func LoadConfig(path string) (Config, error) {
	var c Config
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return c, err
	}
	if err == nil {
		if err := json.Unmarshal(b, &c); err != nil {
			return c, err
		}
		if c.Location != nil && checkLocation(*c.Location) != nil {
			c.Location = nil
		}
	}
	c.fill()
	return c, nil
}

func saveConfig(path string, c Config) error {
	if path == "" {
		return nil // demo: remembered for this run only
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

var errLocation = errors.New("give a position on Earth: latitude −90 to 90, longitude −180 to 180, height −500 to 9000 m")

func checkLocation(l Location) error {
	for _, v := range []float64{l.Lat, l.Lon, l.AltM} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return errLocation
		}
	}
	if l.Lat < -90 || l.Lat > 90 || l.Lon < -180 || l.Lon > 180 || l.AltM < -500 || l.AltM > 9000 {
		return errLocation
	}
	switch l.Source {
	case "browser", "map", "locator", "flags", "demo":
		return nil
	}
	return errors.New("location source must be browser, map, locator, flags or demo")
}
