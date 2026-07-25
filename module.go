package caddydevlocal

import "github.com/caddyserver/caddy/v2"

func init() {
	caddy.RegisterModule(CaddyDevLocal{})
}

type CaddyDevLocal struct{}

func (CaddyDevLocal) CaddyModule() caddy.ModuleInfo {
	return caddy.ModuleInfo{
		ID:  "dev_local",
		New: func() caddy.Module { return new(CaddyDevLocal) },
	}
}
