//go:build !windows

package credentials

import "errors"

// errUnsupported is returned on platforms without an implemented OS keyring.
// We fail closed rather than store secrets in plaintext.
var errUnsupported = errors.New("credentials: encrypted credential storage is not supported on this platform")

func protect(data []byte) ([]byte, error) {
	return nil, errUnsupported
}

func unprotect(blob []byte) ([]byte, error) {
	return nil, errUnsupported
}
