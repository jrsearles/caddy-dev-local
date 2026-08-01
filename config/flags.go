package config

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/spf13/pflag"
)

const (
	flagIngressNetwork = "ingress-network"
	flagTLD            = "tld"
	flagStaleTTL       = "stale-ttl"
	flagProbeTimeout   = "probe-timeout"
	flagPollInterval   = "poll-interval"
)

type sharedFlagSpec struct {
	name  string
	def   string
	usage string
}

var sharedFlagSpecs = []sharedFlagSpec{
	{flagIngressNetwork, "", "Docker network name (env: DEVLOCAL_INGRESS_NETWORK)"},
	{flagTLD, "", "Top-level domain (env: DEVLOCAL_TLD)"},
	{flagStaleTTL, "0", "Keep config for stopped containers (env: DEVLOCAL_STALE_TTL)"},
	{flagProbeTimeout, "0", "HTTP probe timeout (env: DEVLOCAL_PROBE_TIMEOUT)"},
	{flagPollInterval, "0", "Periodic full refresh as a safety net for missed events (env: DEVLOCAL_POLL_INTERVAL, default 30s, 0 disables)"},
}

func isSharedDurationFlag(name string) bool {
	switch name {
	case flagStaleTTL, flagProbeTimeout, flagPollInterval:
		return true
	}
	return false
}

func mustParseDuration(name, def string) time.Duration {
	d, err := time.ParseDuration(def)
	if err != nil {
		panic(fmt.Sprintf("invalid default duration for flag %s: %v", name, err))
	}
	return d
}

// RegisterSharedFlags registers the shared discovery flags on a pflag flag set.
func RegisterSharedFlags(fs *pflag.FlagSet) {
	for _, s := range sharedFlagSpecs {
		if isSharedDurationFlag(s.name) {
			fs.Duration(s.name, mustParseDuration(s.name, s.def), s.usage)
		} else {
			fs.String(s.name, s.def, s.usage)
		}
	}
}

// RegisterSharedGoFlags registers the shared discovery flags on a standard
// library flag set (used by Caddy's command flag registration).
func RegisterSharedGoFlags(fs *flag.FlagSet) {
	for _, s := range sharedFlagSpecs {
		if isSharedDurationFlag(s.name) {
			fs.Duration(s.name, mustParseDuration(s.name, s.def), s.usage)
		} else {
			fs.String(s.name, s.def, s.usage)
		}
	}
}

// SharedOverrides translates explicitly-set shared flags into overrides.
func SharedOverrides(fs *pflag.FlagSet) FlagOverrides {
	var o FlagOverrides
	if fs.Changed(flagIngressNetwork) {
		v, _ := fs.GetString(flagIngressNetwork)
		o.IngressNetwork = &v
	}
	if fs.Changed(flagTLD) {
		v, _ := fs.GetString(flagTLD)
		o.TLD = &v
	}
	if fs.Changed(flagStaleTTL) {
		v, _ := fs.GetDuration(flagStaleTTL)
		o.StaleTTL = &v
	}
	if fs.Changed(flagProbeTimeout) {
		v, _ := fs.GetDuration(flagProbeTimeout)
		o.ProbeTimeout = &v
	}
	if fs.Changed(flagPollInterval) {
		v, _ := fs.GetDuration(flagPollInterval)
		o.PollInterval = &v
	}
	return o
}

// ApplySharedFlags applies explicitly-set shared flags to cfg.
func ApplySharedFlags(cfg *Config, fs *pflag.FlagSet) {
	cfg.ApplyFlags(SharedOverrides(fs))
}

// ResolveStandalone detects standalone mode unless the ingress network was
// explicitly configured via flag or environment variable.
func ResolveStandalone(cfg *Config, fs *pflag.FlagSet) {
	if !fs.Changed(flagIngressNetwork) && os.Getenv("DEVLOCAL_INGRESS_NETWORK") == "" {
		cfg.Standalone = DetectStandalone()
	}
}

// Resolve builds a Config from defaults, explicitly-set shared flags, and
// standalone detection.
func Resolve(fs *pflag.FlagSet) *Config {
	cfg := DefaultConfig()
	ApplySharedFlags(cfg, fs)
	ResolveStandalone(cfg, fs)
	return cfg
}
