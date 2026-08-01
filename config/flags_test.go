package config

import (
	"testing"
	"time"

	"github.com/spf13/pflag"
)

func newSharedFlags(t *testing.T, args ...string) *pflag.FlagSet {
	t.Helper()
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	RegisterSharedFlags(fs)
	if err := fs.Parse(args); err != nil {
		t.Fatalf("parsing flags %v: %v", args, err)
	}
	return fs
}

func TestRegisterSharedFlags(t *testing.T) {
	fs := pflag.NewFlagSet("test", pflag.ContinueOnError)
	RegisterSharedFlags(fs)
	for _, name := range []string{"ingress-network", "tld", "stale-ttl", "probe-timeout", "poll-interval"} {
		if fs.Lookup(name) == nil {
			t.Errorf("expected shared flag %q to be registered", name)
		}
	}
}

func TestApplySharedFlagsExplicit(t *testing.T) {
	cfg := DefaultConfig()
	fs := newSharedFlags(t,
		"--ingress-network=lan",
		"--tld=test.local",
		"--stale-ttl=5m",
		"--probe-timeout=500ms",
		"--poll-interval=15s",
	)
	ApplySharedFlags(cfg, fs)

	if cfg.IngressNetwork != "lan" {
		t.Errorf("IngressNetwork = %q, want lan", cfg.IngressNetwork)
	}
	if cfg.TLD != "test.local" {
		t.Errorf("TLD = %q, want test.local", cfg.TLD)
	}
	if cfg.StaleTTL != 5*time.Minute {
		t.Errorf("StaleTTL = %v, want 5m", cfg.StaleTTL)
	}
	if cfg.ProbeTimeout != 500*time.Millisecond {
		t.Errorf("ProbeTimeout = %v, want 500ms", cfg.ProbeTimeout)
	}
	if cfg.PollInterval != 15*time.Second {
		t.Errorf("PollInterval = %v, want 15s", cfg.PollInterval)
	}
}

func TestApplySharedFlagsDefaultsPreserved(t *testing.T) {
	cfg := &Config{
		IngressNetwork: "lan",
		TLD:            "test.local",
		StaleTTL:       5 * time.Minute,
		ProbeTimeout:   500 * time.Millisecond,
		PollInterval:   15 * time.Second,
	}
	ApplySharedFlags(cfg, newSharedFlags(t))

	if cfg.IngressNetwork != "lan" {
		t.Error("unset flags must not override env-configured values")
	}
	if cfg.TLD != "test.local" {
		t.Error("unset flags must not override env-configured values")
	}
	if cfg.StaleTTL != 5*time.Minute {
		t.Error("unset flags must not override env-configured values")
	}
	if cfg.PollInterval != 15*time.Second {
		t.Error("unset flags must not override env-configured values")
	}
}

func TestApplySharedFlagsPollIntervalZeroExplicit(t *testing.T) {
	cfg := DefaultConfig()
	ApplySharedFlags(cfg, newSharedFlags(t, "--poll-interval=0"))
	if cfg.PollInterval != 0 {
		t.Errorf("PollInterval = %v, want 0 (explicit --poll-interval=0 disables polling)", cfg.PollInterval)
	}
}

func TestResolveDefaults(t *testing.T) {
	cfg := Resolve(newSharedFlags(t))
	if cfg.IngressNetwork != "devlocal" {
		t.Errorf("IngressNetwork = %q, want devlocal", cfg.IngressNetwork)
	}
	if cfg.TLD != "dev.local" {
		t.Errorf("TLD = %q, want dev.local", cfg.TLD)
	}
	if cfg.StaleTTL != time.Hour {
		t.Errorf("StaleTTL = %v, want 1h", cfg.StaleTTL)
	}
	if cfg.ProbeTimeout != 2*time.Second {
		t.Errorf("ProbeTimeout = %v, want 2s", cfg.ProbeTimeout)
	}
	if cfg.PollInterval != 30*time.Second {
		t.Errorf("PollInterval = %v, want 30s", cfg.PollInterval)
	}
}

func TestResolveStandaloneExplicitFlag(t *testing.T) {
	t.Setenv("DEVLOCAL_INGRESS_NETWORK", "")
	fs := newSharedFlags(t, "--ingress-network=lan")
	cfg := &Config{}
	ResolveStandalone(cfg, fs)
	if cfg.Standalone {
		t.Error("explicit --ingress-network must disable standalone detection")
	}
}

func TestResolveStandaloneExplicitEnv(t *testing.T) {
	t.Setenv("DEVLOCAL_INGRESS_NETWORK", "lan")
	fs := newSharedFlags(t)
	cfg := &Config{}
	ResolveStandalone(cfg, fs)
	if cfg.Standalone {
		t.Error("DEVLOCAL_INGRESS_NETWORK env must disable standalone detection")
	}
}

func TestResolveStandaloneAutoDetect(t *testing.T) {
	t.Setenv("DEVLOCAL_INGRESS_NETWORK", "")
	fs := newSharedFlags(t)
	cfg := &Config{}
	ResolveStandalone(cfg, fs)
	if want := DetectStandalone(); cfg.Standalone != want {
		t.Errorf("Standalone = %v, want %v", cfg.Standalone, want)
	}
}
