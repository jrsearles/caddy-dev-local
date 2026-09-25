package generator

import (
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"testing"

	"github.com/jrsearles/caddy-dev-local/config"
	"github.com/jrsearles/caddy-dev-local/discovery"
)

func TestDomainParityFixtures(t *testing.T) {
	content, err := os.ReadFile("../tests/Fixtures/domain_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name            string
		TLD             string
		Containers      []*discovery.ContainerInfo
		ExpectedDomains []string
		ExpectedTargets map[string][]string
	}
	if err := json.Unmarshal(content, &cases); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range cases {
		t.Run(fixture.Name, func(t *testing.T) {
			cfg := &config.Config{TLD: fixture.TLD}
			if got := Domains(cfg, fixture.Containers); !slices.Equal(got, fixture.ExpectedDomains) {
				t.Errorf("domains: got %v, want %v", got, fixture.ExpectedDomains)
			}
			if got := DomainTargets(cfg, fixture.Containers); !reflect.DeepEqual(got, fixture.ExpectedTargets) {
				t.Errorf("targets: got %v, want %v", got, fixture.ExpectedTargets)
			}
		})
	}
}
