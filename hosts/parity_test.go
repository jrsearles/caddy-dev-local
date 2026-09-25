package hosts

import (
	"encoding/json"
	"os"
	"testing"
)

func TestHostsParityFixtures(t *testing.T) {
	content, err := os.ReadFile("../tests/Fixtures/hosts_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name          string
		TLD           string
		Domains       []string
		ExpectedBlock string
	}
	if err := json.Unmarshal(content, &cases); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range cases {
		t.Run(fixture.Name, func(t *testing.T) {
			if got := buildBlock(fixture.TLD, fixture.Domains); got != fixture.ExpectedBlock {
				t.Errorf("block: got %q, want %q", got, fixture.ExpectedBlock)
			}
		})
	}
}
