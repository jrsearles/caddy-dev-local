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

type domainEntry struct {
	Domain  string
	PortStr string
}

type indexRow struct {
	Domains        []domainEntry
	ContainerName  string
	Image          string
	IPAddress      string
	ComposeProject string
	ComposeService string
	IsRunning      bool
	StartedAt      string
	StoppedAt      string
}

type displayRow struct {
	Domain         string
	PortStr        string
	ContainerName  string
	Image          string
	IPAddress      string
	ComposeProject string
	ComposeService string
	IsRunning      bool
	StartedAt      string
	StoppedAt      string
	RowSpan        int
	IsFirst        bool
}

type indexData struct {
	TLD         string
	DisplayRows []displayRow
}

func GenerateIndexPage(tld string, standalone bool, containers []*ContainerInfo) string {
	groups := make([]indexRow, 0, len(containers))

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

		ipAddress := ""
		if !standalone {
			ipAddress = info.IPAddress
		}

		var domains []domainEntry

		if len(info.CustomDomains) > 0 {
			for _, cd := range info.CustomDomains {
				domains = append(domains, domainEntry{
					Domain:  cd.Domain,
					PortStr: strconv.FormatUint(uint64(cd.Port), 10),
				})
			}
		} else {
			var domain string
			if info.IsCompose {
				domain = info.Project + "." + info.Service + "." + tld
			} else {
				domain = info.ContainerName + "." + tld
			}

			portStr := "-"
			if info.SelectedPort > 0 {
				portStr = strconv.FormatUint(uint64(info.SelectedPort), 10)
			}

			domains = append(domains, domainEntry{
				Domain:  domain,
				PortStr: portStr,
			})

			if standalone {
				var localhostDomain string
				if info.IsCompose {
					localhostDomain = info.Project + "." + info.Service + ".localhost"
				} else {
					localhostDomain = info.ContainerName + ".localhost"
				}
				domains = append(domains, domainEntry{
					Domain:  localhostDomain,
					PortStr: portStr,
				})
			}
		}

		groups = append(groups, indexRow{
			Domains:        domains,
			ContainerName:  info.ContainerName,
			Image:          info.Image,
			IPAddress:      ipAddress,
			ComposeProject: composeProject,
			ComposeService: composeService,
			IsRunning:      info.IsRunning,
			StartedAt:      info.Created.Format("2006-01-02 15:04:05"),
			StoppedAt:      stoppedAt,
		})
	}

	var rows []displayRow
	for gi := range groups {
		for i, d := range groups[gi].Domains {
			rows = append(rows, displayRow{
				Domain:         d.Domain,
				PortStr:        d.PortStr,
				ContainerName:  groups[gi].ContainerName,
				Image:          groups[gi].Image,
				IPAddress:      groups[gi].IPAddress,
				ComposeProject: groups[gi].ComposeProject,
				ComposeService: groups[gi].ComposeService,
				IsRunning:      groups[gi].IsRunning,
				StartedAt:      groups[gi].StartedAt,
				StoppedAt:      groups[gi].StoppedAt,
				RowSpan:        len(groups[gi].Domains),
				IsFirst:        i == 0,
			})
		}
	}

	data := indexData{
		TLD:         tld,
		DisplayRows: rows,
	}

	var sb strings.Builder
	if err := indexTemplate.Execute(&sb, data); err != nil {
		return "template error: " + err.Error()
	}
	return sb.String()
}
