package generator

import (
	"cmp"
	_ "embed"
	"encoding/json"
	"html/template"
	"slices"
	"strconv"
	"strings"
)

//go:embed index.html.tmpl
var indexTemplateHTML string
var indexTemplate = template.Must(template.New("index").Parse(indexTemplateHTML))

type portChip struct {
	PortStr  string
	CopyText string
	IsHTTP   bool
}

type domainEntry struct {
	Domain string
	Chips  []portChip
	URL    string
}

type publishedPort struct {
	Private string
	Public  string
}

type indexRow struct {
	Domains            []domainEntry
	ContainerName      string
	ContainerIDShort   string
	Image              string
	Icon               string
	IPAddress          string
	ComposeProject     string
	ComposeService     string
	IsRunning          bool
	NoPorts            bool
	StartedUnix        int64
	StartedAbs         string
	StoppedUnix        int64
	StoppedAbs         string
	Health             string
	Networks           []string
	PublishedPortPairs []publishedPort
	LabelsJSON         template.JS
	DomainsFlat        string
}

type displayGroup struct {
	Project            string
	Rows               []indexRow
	RunningCount       int
	StoppedCount       int
	DockerDesktopURL   template.URL
	DockerDesktopLabel string
}

func projectDesktopURL(project string) template.URL {
	return template.URL("docker-desktop://dashboard/apps/" + project) // #nosec G203
}

func containersDesktopURL() template.URL {
	return template.URL("docker-desktop://dashboard/open") // #nosec G203
}

func filteredLabels(labels map[string]string) map[string]string {
	prefixes := []string{
		"dev.local.",
		"com.docker.compose.",
		"org.opencontainers.image.",
		"com.docker.extension.",
	}
	out := make(map[string]string)
	for k, v := range labels {
		for _, p := range prefixes {
			if strings.HasPrefix(k, p) {
				out[k] = v
				break
			}
		}
	}
	return out
}

type indexData struct {
	TLD                        string
	DisplayRows                []indexRow
	Groups                     []displayGroup
	RunningCount               int
	StoppedCount               int
	ConfigJSON                 string
	StandaloneDockerDesktopURL template.URL
	StandaloneDockerLabel      string
	DiscoveryError             string
	LastRefreshUnix            int64
}

func buildDomainEntry(domain string, portStrs []string, url string) domainEntry {
	chips := make([]portChip, 0, len(portStrs))
	for _, p := range portStrs {
		chips = append(chips, portChip{PortStr: p, CopyText: domain + ":" + p})
	}
	if len(chips) == 0 {
		chips = []portChip{{PortStr: "-"}}
	}
	return domainEntry{Domain: domain, Chips: chips, URL: url}
}

func sortDomainEntries(entries []domainEntry, httpPortStr string) {
	portNum := func(s string) int {
		n, err := strconv.Atoi(s)
		if err != nil {
			return 1 << 30
		}
		return n
	}
	for i := range entries {
		slices.SortStableFunc(entries[i].Chips, func(a, b portChip) int {
			aHTTP := httpPortStr != "" && a.PortStr == httpPortStr
			bHTTP := httpPortStr != "" && b.PortStr == httpPortStr
			if aHTTP != bHTTP {
				if aHTTP {
					return -1
				}
				return 1
			}
			return cmp.Compare(portNum(a.PortStr), portNum(b.PortStr))
		})
		for j := range entries[i].Chips {
			if httpPortStr != "" && entries[i].URL != "" {
				entries[i].Chips[j].IsHTTP = entries[i].Chips[j].PortStr == httpPortStr
			}
		}
	}
}

func GenerateIndexPage(tld string, standalone bool, containers []*ContainerInfo, configJSON string, discoveryError string, lastRefreshUnix int64) string {
	rows := make([]indexRow, 0, len(containers))

	for _, info := range containers {
		composeProject := ""
		composeService := ""
		if info.IsCompose {
			composeProject = info.Project
			composeService = info.Service
		}

		stoppedAt := ""
		var stoppedUnix int64
		if !info.IsRunning && !info.LastStopped.IsZero() {
			stoppedAt = info.LastStopped.Format("2006-01-02 15:04")
			stoppedUnix = info.LastStopped.Unix()
		}

		ipAddress := ""
		if !standalone {
			ipAddress = info.IPAddress
		}

		httpPortStr := ""
		if info.SelectedPort > 0 {
			httpPortStr = strconv.FormatUint(uint64(info.SelectedPort), 10)
		}

		var domains []domainEntry

		if len(info.CustomDomains) > 0 {
			for _, cd := range info.CustomDomains {
				port := effectivePort(info, cd.Port)
				url := ""
				if info.IsRunning {
					url = "https://" + cd.Domain
				}
				portStr := strconv.FormatUint(uint64(port), 10)
				domains = append(domains, domainEntry{
					Domain: cd.Domain,
					Chips:  []portChip{{PortStr: portStr, CopyText: cd.Domain + ":" + portStr}},
					URL:    url,
				})
			}
		} else if len(info.Ports) > 0 {
			var domain string
			if info.IsCompose {
				domain = info.Project + "." + info.Service + "." + tld
			} else {
				domain = info.ContainerName + "." + tld
			}

			portStrs := make([]string, 0, len(info.Ports))
			for _, p := range info.Ports {
				portStrs = append(portStrs, strconv.FormatUint(uint64(effectivePort(info, p)), 10))
			}

			url := ""
			if info.IsRunning && info.SelectedPort > 0 {
				url = "https://" + domain
			}

			domains = append(domains, buildDomainEntry(domain, portStrs, url))

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
			domains = append(domains, buildDomainEntry(localhostDomain, portStrs, localhostURL))
		}

		sortDomainEntries(domains, httpPortStr)

		domainNames := make([]string, 0, len(domains))
		for _, d := range domains {
			domainNames = append(domainNames, d.Domain)
		}

		pubPairs := make([]publishedPort, 0, len(info.PublishedPorts))
		privKeys := make([]uint16, 0, len(info.PublishedPorts))
		for priv := range info.PublishedPorts {
			privKeys = append(privKeys, priv)
		}
		slices.Sort(privKeys)
		for _, priv := range privKeys {
			pubPairs = append(pubPairs, publishedPort{
				Private: strconv.Itoa(int(priv)),
				Public:  strconv.Itoa(int(info.PublishedPorts[priv])),
			})
		}

		fl := filteredLabels(info.Labels)
		labelsJSON := template.JS("{}")
		if len(fl) > 0 {
			if b, err := json.Marshal(fl); err == nil {
				labelsJSON = template.JS(b) // #nosec G203
			}
		}

		shortID := info.ContainerID
		if len(shortID) > 12 {
			shortID = shortID[:12]
		}

		rows = append(rows, indexRow{
			Domains:            domains,
			ContainerName:      info.ContainerName,
			ContainerIDShort:   shortID,
			Image:              info.Image,
			Icon:               iconForContainer(info.Image, info.Labels),
			IPAddress:          ipAddress,
			ComposeProject:     composeProject,
			ComposeService:     composeService,
			IsRunning:          info.IsRunning,
			NoPorts:            len(info.Ports) == 0 && len(info.CustomDomains) == 0,
			StartedUnix:        info.Created.Unix(),
			StartedAbs:         info.Created.Format("2006-01-02 15:04"),
			StoppedUnix:        stoppedUnix,
			StoppedAbs:         stoppedAt,
			Health:             info.Health,
			Networks:           info.Networks,
			PublishedPortPairs: pubPairs,
			LabelsJSON:         labelsJSON,
			DomainsFlat:        strings.Join(domainNames, " "),
		})
	}

	var top []indexRow
	var projectRows []indexRow
	for i := range rows {
		if rows[i].ComposeProject == "" {
			top = append(top, rows[i])
		} else {
			projectRows = append(projectRows, rows[i])
		}
	}
	slices.SortStableFunc(top, func(a, b indexRow) int {
		if a.IsRunning != b.IsRunning {
			if a.IsRunning {
				return -1
			}
			return 1
		}
		return 0
	})

	projectGroups := make(map[string][]indexRow)
	var projectNames []string
	for i := range projectRows {
		r := &projectRows[i]
		if _, ok := projectGroups[r.ComposeProject]; !ok {
			projectNames = append(projectNames, r.ComposeProject)
		}
		projectGroups[r.ComposeProject] = append(projectGroups[r.ComposeProject], *r)
	}
	slices.Sort(projectNames)

	for _, name := range projectNames {
		slices.SortStableFunc(projectGroups[name], func(a, b indexRow) int {
			if a.IsRunning != b.IsRunning {
				if a.IsRunning {
					return -1
				}
				return 1
			}
			return 0
		})
	}

	grouped := make([]displayGroup, 0, len(projectNames))
	for _, name := range projectNames {
		gr, gs := 0, 0
		for i := range projectGroups[name] {
			if projectGroups[name][i].IsRunning {
				gr++
			} else {
				gs++
			}
		}
		grouped = append(grouped, displayGroup{
			Project:            name,
			Rows:               projectGroups[name],
			RunningCount:       gr,
			StoppedCount:       gs,
			DockerDesktopURL:   projectDesktopURL(name),
			DockerDesktopLabel: "Open " + name + " in Docker Desktop",
		})
	}

	running, stopped := 0, 0
	for i := range top {
		if top[i].IsRunning {
			running++
		} else {
			stopped++
		}
	}
	for gi := range grouped {
		for i := range grouped[gi].Rows {
			if grouped[gi].Rows[i].IsRunning {
				running++
			} else {
				stopped++
			}
		}
	}

	data := indexData{
		TLD:             tld,
		DisplayRows:     top,
		Groups:          grouped,
		RunningCount:    running,
		StoppedCount:    stopped,
		ConfigJSON:      configJSON,
		DiscoveryError:  discoveryError,
		LastRefreshUnix: lastRefreshUnix,
	}
	if len(top) > 0 {
		data.StandaloneDockerDesktopURL = containersDesktopURL()
		data.StandaloneDockerLabel = "Open Docker Desktop"
	}

	var sb strings.Builder
	if err := indexTemplate.Execute(&sb, data); err != nil {
		return "template error: " + err.Error()
	}
	return sb.String()
}
