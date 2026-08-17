package caddydevlocal

import (
	"testing"
	"time"

	caddycmd "github.com/caddyserver/caddy/v2/cmd"
	"github.com/spf13/pflag"

	"github.com/jrsearles/caddy-dev-local/config"
)

func newDevlocalFlags(t *testing.T, args ...string) caddycmd.Flags {
	t.Helper()
	fs := pflag.NewFlagSet("devlocal", pflag.ContinueOnError)
	fs.String("tld", "", "")
	fs.Duration("stale-ttl", 0, "")
	fs.Duration("probe-timeout", 0, "")
	fs.Bool("hosts-file", true, "")
	fs.Duration("poll-interval", 0, "")
	if err := fs.Parse(args); err != nil {
		t.Fatalf("parsing flags %v: %v", args, err)
	}
	return caddycmd.Flags{FlagSet: fs}
}

func TestApplyCommandFlagsDefaultsPreserved(t *testing.T) {
	cfg := &config.Config{
		TLD:          "dev.local",
		StaleTTL:     time.Hour,
		ProbeTimeout: 2 * time.Second,
		HostsFile:    false,
		PollInterval: 45 * time.Second,
	}

	applyCommandFlags(cfg, newDevlocalFlags(t))

	if cfg.HostsFile {
		t.Error("unset --hosts-file flag must not override an env-configured HostsFile value")
	}
	if cfg.PollInterval != 45*time.Second {
		t.Errorf("PollInterval = %v, want 45s (env-configured value preserved)", cfg.PollInterval)
	}
	if cfg.TLD != "dev.local" {
		t.Errorf("defaults were clobbered: %+v", cfg)
	}
}

func TestApplyCommandFlagsExplicitOverrides(t *testing.T) {
	cfg := config.DefaultConfig()

	applyCommandFlags(cfg, newDevlocalFlags(t,
		"--tld=test.local",
		"--stale-ttl=5m",
		"--probe-timeout=500ms",
		"--hosts-file=false",
		"--poll-interval=15s",
	))

	if cfg.TLD != "test.local" {
		t.Errorf("TLD = %q, want test.local", cfg.TLD)
	}
	if cfg.StaleTTL != 5*time.Minute {
		t.Errorf("StaleTTL = %v, want 5m", cfg.StaleTTL)
	}
	if cfg.ProbeTimeout != 500*time.Millisecond {
		t.Errorf("ProbeTimeout = %v, want 500ms", cfg.ProbeTimeout)
	}
	if cfg.HostsFile {
		t.Error("HostsFile should be false when --hosts-file=false is passed")
	}
	if cfg.PollInterval != 15*time.Second {
		t.Errorf("PollInterval = %v, want 15s", cfg.PollInterval)
	}
}

func TestApplyCommandFlagsHostsFileTrueExplicit(t *testing.T) {
	cfg := &config.Config{
		HostsFile: false,
	}

	applyCommandFlags(cfg, newDevlocalFlags(t, "--hosts-file=true"))

	if !cfg.HostsFile {
		t.Error("explicit --hosts-file=true must override cfg.HostsFile")
	}
}

func TestApplyCommandFlagsPollIntervalZeroExplicit(t *testing.T) {
	cfg := config.DefaultConfig()

	applyCommandFlags(cfg, newDevlocalFlags(t, "--poll-interval=0"))

	if cfg.PollInterval != 0 {
		t.Errorf("PollInterval = %v, want 0 (explicit --poll-interval=0 disables polling)", cfg.PollInterval)
	}
}
