package probe

import (
	"encoding/json"
	"os"
	"testing"
)

// checkSchema checks that a report's top-level keys are exactly the
// schema's properties and that required keys are present. (A full JSON
// Schema validator would be a dependency; this catches drift.)
func checkSchema(t *testing.T, reportPath string) {
	t.Helper()
	var schema struct {
		Required   []string                   `json:"required"`
		Properties map[string]json.RawMessage `json:"properties"`
	}
	b, err := os.ReadFile("../schema/probe-report.schema.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &schema); err != nil {
		t.Fatalf("schema: %v", err)
	}
	var rep map[string]json.RawMessage
	b, _ = os.ReadFile(reportPath)
	if err := json.Unmarshal(b, &rep); err != nil {
		t.Fatal(err)
	}
	for k := range rep {
		if _, ok := schema.Properties[k]; !ok {
			t.Errorf("report key %q is not in the schema", k)
		}
	}
	for _, k := range schema.Required {
		if _, ok := rep[k]; !ok {
			t.Errorf("required key %q missing from the report", k)
		}
	}
}
