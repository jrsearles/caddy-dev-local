package config

import (
	"os"
	"time"
)

type Config struct {
	TLD          string
	StaleTTL     time.Duration
	ProbeTimeout time.Duration
	Standalone   bool
	HostsFile    bool
	Tracing      bool
	PollInterval time.Duration
}

func DetectStandalone() bool {
	_, err := os.Stat("/.dockerenv")
	return os.IsNotExist(err)
}

func DefaultConfig() *Config {
	return &Config{
		TLD:          getEnvOrDefault("DEVLOCAL_TLD", "dev.local"),
		StaleTTL:     getDurationOrDefault("DEVLOCAL_STALE_TTL", time.Hour),
		ProbeTimeout: getDurationOrDefault("DEVLOCAL_PROBE_TIMEOUT", 2*time.Second),
		HostsFile:    getBoolOrDefault("DEVLOCAL_HOSTS_FILE", true),
		Tracing:      getBoolOrDefault("DEVLOCAL_TRACING", true),
		PollInterval: getDurationOrDefault("DEVLOCAL_POLL_INTERVAL", 30*time.Second),
	}
}

type FlagOverrides struct {
	TLD          *string
	StaleTTL     *time.Duration
	ProbeTimeout *time.Duration
	HostsFile    *bool
	Tracing      *bool
	PollInterval *time.Duration
}

func (c *Config) ApplyFlags(o FlagOverrides) {
	if o.TLD != nil {
		c.TLD = *o.TLD
	}
	if o.StaleTTL != nil {
		c.StaleTTL = *o.StaleTTL
	}
	if o.ProbeTimeout != nil {
		c.ProbeTimeout = *o.ProbeTimeout
	}
	if o.HostsFile != nil {
		c.HostsFile = *o.HostsFile
	}
	if o.Tracing != nil {
		c.Tracing = *o.Tracing
	}
	if o.PollInterval != nil {
		c.PollInterval = *o.PollInterval
	}
}

func getEnvOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func getBoolOrDefault(key string, defaultVal bool) bool {
	if v := os.Getenv(key); v != "" {
		switch v {
		case "true", "1", "yes":
			return true
		case "false", "0", "no":
			return false
		}
	}
	return defaultVal
}

func getDurationOrDefault(key string, defaultVal time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return defaultVal
}
