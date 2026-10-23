package probe

import (
	"encoding/json"
	"os"
	"time"

	"d75scope/ifsource"
	"d75scope/radio"
)

// SchemaVersion is the probe-report.json schema version
// (schema/probe-report.schema.json).
const SchemaVersion = 1

// Report is probe-report.json.
type Report struct {
	SchemaVersion int               `json:"schema_version"`
	Tool          Tool              `json:"tool"`
	Started       time.Time         `json:"started"`
	Finished      time.Time         `json:"finished"`
	Mode          string            `json:"mode"` // "read-only" or "allow-writes"
	Mock          bool              `json:"mock"`
	Settings      Settings          `json:"settings"`
	Notes         map[string]string `json:"notes"`

	Audio  AudioSection  `json:"audio"`
	Serial SerialSection `json:"serial"`
	CAT    CATSection    `json:"cat"`

	PreviousSession *PreviousSession     `json:"previous_session,omitempty"`
	Snapshot        *radio.Snapshot      `json:"snapshot,omitempty"`
	Steps           []Step               `json:"steps"`
	Recordings      []Recording          `json:"recordings"`
	Inversion       []Inversion          `json:"inversion"`
	Offsets         []Offset             `json:"offsets"`
	AITest          *AITest              `json:"ai_test,omitempty"`
	Restore         *radio.RestoreReport `json:"restore,omitempty"`

	Answers    []Answer             `json:"answers"`
	Warnings   []string             `json:"warnings"`
	Errors     []string             `json:"errors"`
	Transcript []radio.TrafficEntry `json:"transcript"`
}

// Tool identifies the probe build and host.
type Tool struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	GoVersion string `json:"go_version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

// Settings echoes the options the probe ran with.
type Settings struct {
	RecordSeconds   float64 `json:"record_seconds"`
	Port            string  `json:"port,omitempty"`
	Baud            int     `json:"baud"`
	AudioDevice     string  `json:"audio_device,omitempty"`
	IFCentreHz      float64 `json:"if_centre_hz"`
	RefCarrierHz    int64   `json:"ref_carrier_hz,omitempty"`
	TuneStepHz      int64   `json:"tune_step_hz"`
	Interactive     bool    `json:"interactive"`
	RestorePrevious bool    `json:"restore_previous"`
}

// AudioSection lists audio devices and the one chosen.
type AudioSection struct {
	Devices     []ifsource.Device `json:"devices"`
	Radio       *ifsource.Device  `json:"radio,omitempty"`
	MatchReason string            `json:"match_reason,omitempty"`
	Format      *ifsource.Format  `json:"capture_format,omitempty"`
	Error       string            `json:"error,omitempty"`
}

// SerialSection lists serial ports and the one chosen.
type SerialSection struct {
	Ports []radio.Port `json:"ports"`
	Port  string       `json:"port,omitempty"`
	Match string       `json:"match,omitempty"`
	Baud  int          `json:"baud,omitempty"`
	// ExclusiveLock is "verified" (a second open was refused), "not
	// enforced" (a second open succeeded) or "untested".
	ExclusiveLock string `json:"exclusive_lock"`
	Error         string `json:"error,omitempty"`
}

// CATSection records every read and what the port carried.
type CATSection struct {
	Responding bool            `json:"responding"`
	Identity   radio.Identity  `json:"identity"`
	Reads      []CommandResult `json:"reads"`
	Hygiene    radio.PortStats `json:"hygiene"`
	Foreign    []string        `json:"foreign_samples,omitempty"` // first few NMEA/APRS lines seen
	State      map[string]any  `json:"state,omitempty"`           // decoded values from the reads
}

// CommandResult is one CAT exchange.
type CommandResult struct {
	Command   string  `json:"command"`
	Reply     string  `json:"reply,omitempty"`
	Error     string  `json:"error,omitempty"`
	ElapsedMs float64 `json:"elapsed_ms"`
}

// PreviousSession describes a dirty snapshot found at start.
type PreviousSession struct {
	Snapshot *radio.Snapshot      `json:"snapshot"`
	Restored bool                 `json:"restored"`
	Restore  *radio.RestoreReport `json:"restore,omitempty"`
	Error    string               `json:"error,omitempty"`
}

// Step is one action in the write sequence.
type Step struct {
	Time   time.Time `json:"t"`
	Action string    `json:"action"`
	Result string    `json:"result"`
	Error  string    `json:"error,omitempty"`
}

// Recording is one WAV file and its analysis.
type Recording struct {
	Name        string          `json:"name"`
	File        string          `json:"file"`
	Purpose     string          `json:"purpose"`
	Mode        string          `json:"mode"`
	Output      string          `json:"output"` // IO value at the time
	DialHz      int64           `json:"dial_hz"`
	DialAfterHz int64           `json:"dial_after_hz"`
	SquelchOpen *bool           `json:"squelch_open,omitempty"`
	Started     time.Time       `json:"started"`
	Seconds     float64         `json:"seconds"`
	Format      ifsource.Format `json:"format"`
	Overruns    uint64          `json:"overruns"`
	Analysis    Analysis        `json:"analysis"`
}

// AITest records unsolicited lines seen while the owner turned the dial.
type AITest struct {
	Ran   bool     `json:"ran"`
	Lines []string `json:"lines"`
	Note  string   `json:"note,omitempty"`
}

// Answer is the probe's answer to one entry of HARDWARE_QUESTIONS.md.
type Answer struct {
	ID       string `json:"id"`
	Question string `json:"question"`
	Answer   string `json:"answer"`
	// Basis is "measured", "inferred", "owner" (from the owner's notes) or
	// "unknown".
	Basis    string `json:"basis"`
	Evidence string `json:"evidence,omitempty"`
}

// WriteJSON writes the report.
func (r *Report) WriteJSON(path string) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
