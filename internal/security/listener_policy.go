package security

import (
	"fmt"
	"net"
	"strings"
)

// ValidateListener rejects non-loopback listeners unless authenticated mode is enabled.
func ValidateListener(address string, authenticated bool) error {
	if authenticated {
		if _, _, err := net.SplitHostPort(address); err != nil {
			return fmt.Errorf("listen address must be host:port: %w", err)
		}
		return nil
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("listen address must be host:port: %w", err)
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	ip := net.ParseIP(host)
	if ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("non-loopback listener requires authenticated mode")
}
