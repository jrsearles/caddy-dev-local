package generator

import (
	_ "embed"
	"html/template"
	"strconv"
	"strings"
	"sync"
)

//go:embed index.html.tmpl
var indexTemplateHTML string

var (
	indexTemplateOnce sync.Once
	indexTemplate     *template.Template
)

type indexRow struct {
	Domain          string
	LocalhostDomain string
	ContainerName   string
	TypeBadge       template.HTML
	StatusClass     string
	StatusText      string
	PortStr         string
}

type indexData struct {
	TLD        string
	Containers []indexRow
}

func getIndexTemplate() *template.Template {
	indexTemplateOnce.Do(func() {
		indexTemplate = template.Must(template.New("index").Parse(indexTemplateHTML))
	})
	return indexTemplate
}

func GenerateIndexPage(tld string, standalone bool, containers []*ContainerInfo) string {
	var rows []indexRow
	for _, info := range containers {
		if !info.IsRunning {
			continue
		}

		domain := ""
		if info.IsCompose {
			domain = info.Project + "." + info.Service + "." + tld
		} else {
			domain = info.ContainerName + "." + tld
		}

		localhostDomain := ""
		if standalone {
			if info.IsCompose {
				localhostDomain = info.Project + "." + info.Service + ".localhost"
			} else {
				localhostDomain = info.ContainerName + ".localhost"
			}
		}

		typeBadge := template.HTML(`<span class="badge badge-standalone">standalone</span>`)
		if info.IsCompose {
			typeBadge = `<span class="badge badge-compose">compose</span>`
		}

		portStr := "-"
		if info.SelectedPort > 0 {
			portStr = strconv.FormatUint(uint64(info.SelectedPort), 10)
		}

		rows = append(rows, indexRow{
			Domain:          domain,
			LocalhostDomain: localhostDomain,
			ContainerName:   info.ContainerName,
			TypeBadge:       typeBadge,
			StatusClass:     "status-running",
			StatusText:      "running",
			PortStr:         portStr,
		})
	}

	data := indexData{
		TLD:        tld,
		Containers: rows,
	}

	var sb strings.Builder
	if err := getIndexTemplate().Execute(&sb, data); err != nil {
		return "template error: " + err.Error()
	}
	return sb.String()
}
