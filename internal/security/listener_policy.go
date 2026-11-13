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

// ValidateContainerListener accepts only explicit wildcard listeners for the
// opt-in container profile. Docker forwards a published host port to the
// container's network interface, not to its loopback, so binding 127.0.0.1
// inside the container would make the published port unreachable. Host
// publication must be bound to 127.0.0.1 in compose.yaml; that binding is
// checked by the Compose publication test. A concrete non-loopback IP is
// rejected so the setting cannot silently widen exposure on a bare host.
func ValidateContainerListener(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("listen address must be host:port: %w", err)
	}
	if port == "" {
		return fmt.Errorf("listen address must include a port")
	}
	host = strings.Trim(host, "[]")
	if host == "0.0.0.0" || host == "::" {
		return nil
	}
	return fmt.Errorf("container listener must use a wildcard address (0.0.0.0 or [::]), got %q", host)
}
