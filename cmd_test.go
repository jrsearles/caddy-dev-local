package caddydevlocal

import (
	"testing"
	"time"

	caddycmd "github.com/caddyserver/caddy/v2/cmd"
	"github.com/spf13/pflag"

	"github.com/jsearles/caddy-dev-local/config"
)

func newDevlocalFlags(t *testing.T, args ...string) caddycmd.Flags {
	t.Helper()
	fs := pflag.NewFlagSet("devlocal", pflag.ContinueOnError)
	fs.String("ingress-network", "", "")
	fs.String("tld", "", "")
	fs.Duration("stale-ttl", 0, "")
	fs.Duration("probe-timeout", 0, "")
	fs.Bool("hosts-file", true, "")
	if err := fs.Parse(args); err != nil {
		t.Fatalf("parsing flags %v: %v", args, err)
	}
	return caddycmd.Flags{FlagSet: fs}
}

func TestApplyCommandFlagsDefaultsPreserved(t *testing.T) {
	cfg := &config.Config{
		IngressNetwork: "devlocal",
		TLD:            "dev.local",
		StaleTTL:       time.Hour,
		ProbeTimeout:   2 * time.Second,
		HostsFile:      false,
	}

	applyCommandFlags(cfg, newDevlocalFlags(t))

	if cfg.HostsFile {
		t.Error("unset --hosts-file flag must not override an env-configured HostsFile value")
	}
	if cfg.IngressNetwork != "devlocal" || cfg.TLD != "dev.local" {
		t.Errorf("defaults were clobbered: %+v", cfg)
	}
}

func TestApplyCommandFlagsExplicitOverrides(t *testing.T) {
	cfg := config.DefaultConfig()

	applyCommandFlags(cfg, newDevlocalFlags(t,
		"--ingress-network=lan",
		"--tld=test.local",
		"--stale-ttl=5m",
		"--probe-timeout=500ms",
		"--hosts-file=false",
	))

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
	if cfg.HostsFile {
		t.Error("HostsFile should be false when --hosts-file=false is passed")
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
