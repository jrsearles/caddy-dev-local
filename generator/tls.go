package generator

import "crypto/tls"

var skipTLSVerify = &tls.Config{
	InsecureSkipVerify: true,
}
