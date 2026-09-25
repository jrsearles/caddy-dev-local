package caddy

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/jrsearles/caddy-dev-local/config"
)

func TestCaddyParityFixtures(t *testing.T) {
	content, err := os.ReadFile("../../tests/Fixtures/caddy_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name             string
		Tracing          bool
		Targets          map[string][]string
		ExpectedRoutes   map[string]json.RawMessage
		ExpectedPolicies map[string]json.RawMessage
	}
	if err := json.Unmarshal(content, &cases); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range cases {
		t.Run(fixture.Name, func(t *testing.T) {
			routes, policies, err := buildConfig(&config.Config{Tracing: fixture.Tracing}, fixture.Targets)
			if err != nil {
				t.Fatal(err)
			}
			checkCaddyParity(t, "routes", routes, fixture.ExpectedRoutes)
			checkCaddyParity(t, "policies", policies, fixture.ExpectedPolicies)
		})
	}
}

func checkCaddyParity(t *testing.T, name string, actual, expected map[string]json.RawMessage) {
	t.Helper()
	actualJSON, err := json.Marshal(actual)
	if err != nil {
		t.Fatal(err)
	}
	expectedJSON, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	var actualValue, expectedValue any
	if err := json.Unmarshal(actualJSON, &actualValue); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(expectedJSON, &expectedValue); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actualValue, expectedValue) {
		t.Errorf("%s: got %s, want %s", name, actualJSON, expectedJSON)
	}
}
