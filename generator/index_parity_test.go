package generator

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/jrsearles/caddy-dev-local/discovery"
)

func TestIndexParityFixtures(t *testing.T) {
	content, err := os.ReadFile("../tests/Fixtures/index_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name             string
		TLD              string
		Containers       []*discovery.ContainerInfo
		ConfigJSON       string
		DiscoveryError   string
		ExpectedContains []string
		ExcludedContains []string
	}
	if err := json.Unmarshal(content, &cases); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range cases {
		t.Run(fixture.Name, func(t *testing.T) {
			page := GenerateIndexPage(fixture.TLD, fixture.Containers, fixture.ConfigJSON, fixture.DiscoveryError, 0)
			for _, expected := range fixture.ExpectedContains {
				if !strings.Contains(page, expected) {
					t.Errorf("page missing %q", expected)
				}
			}
			for _, excluded := range fixture.ExcludedContains {
				if strings.Contains(page, excluded) {
					t.Errorf("page unexpectedly contains %q", excluded)
				}
			}
		})
	}
}
