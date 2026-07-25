package config

import (
	"os"
	"time"
)

type Config struct {
	IngressNetwork string
	TLD            string
	StaleTTL       time.Duration
	PollInterval   time.Duration
	ProbeTimeout   time.Duration
	Standalone     bool
}

func DetectStandalone() bool {
	_, err := os.Stat("/.dockerenv")
	return os.IsNotExist(err)
}

func DefaultConfig() *Config {
	return &Config{
		IngressNetwork: getEnvOrDefault("DEVLOCAL_INGRESS_NETWORK", "devlocal"),
		TLD:            getEnvOrDefault("DEVLOCAL_TLD", "dev.local"),
		StaleTTL:       getDurationOrDefault("DEVLOCAL_STALE_TTL", time.Hour),
		PollInterval:   getDurationOrDefault("DEVLOCAL_POLL_INTERVAL", 30*time.Second),
		ProbeTimeout:   getDurationOrDefault("DEVLOCAL_PROBE_TIMEOUT", 2*time.Second),
	}
}

func (c *Config) ApplyFlags(ingressNetwork, tld string, staleTTL, pollInterval, probeTimeout time.Duration) {
	if ingressNetwork != "" {
		c.IngressNetwork = ingressNetwork
	}
	if tld != "" {
		c.TLD = tld
	}
	if staleTTL > 0 {
		c.StaleTTL = staleTTL
	}
	if pollInterval > 0 {
		c.PollInterval = pollInterval
	}
	if probeTimeout > 0 {
		c.ProbeTimeout = probeTimeout
	}
}

func getEnvOrDefault(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
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
