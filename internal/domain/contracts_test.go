package domain_test

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/PsyChaos/ai-team-harness/internal/domain"
)

// Compare complete JSON trees, so a dropped field, null, empty list, or empty
// digest fails even when decoding into a Go struct would otherwise succeed.
func TestRecordedContracts(t *testing.T) {
	cases := []struct {
		name  string
		value any
	}{
		{"project-item", &domain.ProjectItem{}},
		{"snapshot", &domain.Snapshot{}},
		{"bootstrap", &domain.BootstrapEnvelope{}},
		{"routing-task", &domain.RoutingResult{}},
		{"routing-retry", &domain.RoutingResult{}},
		{"routing-action", &domain.RoutingResult{}},
		{"routing-judgment", &domain.RoutingResult{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			original, err := os.ReadFile("testdata/" + tc.name + ".json")
			if err != nil {
				t.Fatal(err)
			}
			decoder := json.NewDecoder(bytes.NewReader(original))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(tc.value); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(tc.value)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(jsonTree(t, original), jsonTree(t, encoded)) {
				t.Fatalf("contract lost data\noriginal: %s\nencoded: %s", original, encoded)
			}
		})
	}
}

func jsonTree(t *testing.T, data []byte) any {
	t.Helper()
	var value any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err)
	}
	return value
}
