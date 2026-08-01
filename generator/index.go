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
	URL     string
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
	URL            string
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
		if info.SelectedPort == 0 && len(info.CustomDomains) == 0 {
			if standalone {
				if len(info.PublishedPorts) == 0 {
					continue
				}
			} else if len(info.Ports) == 0 {
				continue
			}
		}

		composeProject := ""
		composeService := ""
		if info.IsCompose {
			composeProject = info.Project
			composeService = info.Service
		}

		stoppedAt := ""
		if !info.IsRunning && !info.LastStopped.IsZero() {
			stoppedAt = info.LastStopped.Format("2006-01-02 15:04")
		}

		ipAddress := ""
		if !standalone {
			ipAddress = info.IPAddress
		}

		var domains []domainEntry

		if len(info.CustomDomains) > 0 {
			for _, cd := range info.CustomDomains {
				url := ""
				if info.IsRunning {
					url = "https://" + cd.Domain
				}
				domains = append(domains, domainEntry{
					Domain:  cd.Domain,
					PortStr: strconv.FormatUint(uint64(cd.Port), 10),
					URL:     url,
				})
			}
		} else {
			var domain string
			if info.IsCompose {
				domain = info.Project + "." + info.Service + "." + tld
			} else {
				domain = info.ContainerName + "." + tld
			}

			portStrs := make([]string, 0, len(info.Ports))
			for _, p := range info.Ports {
				port := p
				if standalone {
					if pub, ok := info.PublishedPorts[p]; ok {
						port = pub
					}
				}
				portStrs = append(portStrs, strconv.FormatUint(uint64(port), 10))
			}
			portStr := "-"
			if len(portStrs) > 0 {
				portStr = strings.Join(portStrs, ", ")
			}

			url := ""
			if info.IsRunning && info.SelectedPort > 0 {
				url = "https://" + domain
			}

			domains = append(domains, domainEntry{
				Domain:  domain,
				PortStr: portStr,
				URL:     url,
			})

			if standalone {
				var localhostDomain string
				if info.IsCompose {
					localhostDomain = info.Project + "." + info.Service + ".localhost"
				} else {
					localhostDomain = info.ContainerName + ".localhost"
				}
				localhostURL := ""
				if info.IsRunning && info.SelectedPort > 0 {
					localhostURL = "https://" + localhostDomain
				}
				domains = append(domains, domainEntry{
					Domain:  localhostDomain,
					PortStr: portStr,
					URL:     localhostURL,
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
			StartedAt:      info.Created.Format("2006-01-02 15:04"),
			StoppedAt:      stoppedAt,
		})
	}

	var rows []displayRow
	for gi := range groups {
		for i, d := range groups[gi].Domains {
			rows = append(rows, displayRow{
				Domain:         d.Domain,
				PortStr:        d.PortStr,
				URL:            d.URL,
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
