package generator

import (
	_ "embed"
	"html/template"
	"strconv"
	"strings"
)

//go:embed index.html.tmpl
var indexTemplateHTML string
var indexTemplate = template.Must(template.New("index").Parse(indexTemplateHTML))

type indexRow struct {
	Domain          string
	LocalhostDomain string
	ContainerName   string
	IPAddress       string
	ComposeProject  string
	ComposeService  string
	IsRunning       bool
	StartedAt       string
	StoppedAt       string
	PortStr         string
}

type indexData struct {
	TLD        string
	Containers []indexRow
}

func GenerateIndexPage(tld string, standalone bool, containers []*ContainerInfo) string {
	rows := make([]indexRow, 0, len(containers))
	for _, info := range containers {
		composeProject := ""
		composeService := ""
		if info.IsCompose {
			composeProject = info.Project
			composeService = info.Service
		}

		stoppedAt := ""
		if !info.IsRunning && !info.LastStopped.IsZero() {
			stoppedAt = info.LastStopped.Format("2006-01-02 15:04:05")
		}

		if len(info.CustomDomains) > 0 {
			for _, cd := range info.CustomDomains {
				ipAddress := ""
				if !standalone {
					ipAddress = info.IPAddress
				}
				rows = append(rows, indexRow{
					Domain:         cd.Domain,
					ContainerName:  info.ContainerName,
					IPAddress:      ipAddress,
					ComposeProject: composeProject,
					ComposeService: composeService,
					IsRunning:      info.IsRunning,
					StartedAt:      info.Created.Format("2006-01-02 15:04:05"),
					StoppedAt:      stoppedAt,
					PortStr:        strconv.FormatUint(uint64(cd.Port), 10),
				})
			}
			continue
		}

		var domain string
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

		portStr := "-"
		if info.SelectedPort > 0 {
			portStr = strconv.FormatUint(uint64(info.SelectedPort), 10)
		}

		ipAddress := ""
		if !standalone {
			ipAddress = info.IPAddress
		}

		rows = append(rows, indexRow{
			Domain:          domain,
			LocalhostDomain: localhostDomain,
			ContainerName:   info.ContainerName,
			IPAddress:       ipAddress,
			ComposeProject:  composeProject,
			ComposeService:  composeService,
			IsRunning:       info.IsRunning,
			StartedAt:       info.Created.Format("2006-01-02 15:04:05"),
			StoppedAt:       stoppedAt,
			PortStr:         portStr,
		})
	}

	data := indexData{
		TLD:        tld,
		Containers: rows,
	}

	var sb strings.Builder
	if err := indexTemplate.Execute(&sb, data); err != nil {
		return "template error: " + err.Error()
	}
	return sb.String()
}
