package generator

import "crypto/tls"

var skipTLSVerify = &tls.Config{
	InsecureSkipVerify: true, //nolint:gosec // TLS verification intentionally skipped for local HTTP port probing
}
