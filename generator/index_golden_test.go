package generator

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jrsearles/caddy-dev-local/discovery"
)

var updateIndexGolden = flag.Bool("update-index-golden", false, "rewrite tests/Fixtures/golden/index from Go output")

const indexGoldenDir = "../tests/Fixtures/golden/index"

type indexFixture struct {
	Name            string
	Slug            string
	TLD             string
	Containers      []*discovery.ContainerInfo
	ConfigJSON      string
	DiscoveryError  string
	LastRefreshUnix int64
}

func loadIndexFixtures(t *testing.T) []indexFixture {
	t.Helper()
	content, err := os.ReadFile("../tests/Fixtures/index_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []indexFixture
	if err := json.Unmarshal(content, &cases); err != nil {
		t.Fatal(err)
	}
	return cases
}

func TestIndexGoldenFiles(t *testing.T) {
	if *updateIndexGolden {
		if err := os.MkdirAll(indexGoldenDir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, fixture := range loadIndexFixtures(t) {
		t.Run(fixture.Slug, func(t *testing.T) {
			page := GenerateIndexPage(fixture.TLD, fixture.Containers, fixture.ConfigJSON, fixture.DiscoveryError, fixture.LastRefreshUnix)
			path := filepath.Join(indexGoldenDir, fixture.Slug+".html")
			if *updateIndexGolden {
				if err := os.WriteFile(path, []byte(normalizeNewlines(page)), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if got := normalizeNewlines(page); got != normalizeNewlines(string(want)) {
				t.Errorf("page differs from %s, rerun with -update-index-golden", path)
			}
		})
	}
}

func normalizeNewlines(text string) string {
	return strings.ReplaceAll(text, "\r\n", "\n")
}
