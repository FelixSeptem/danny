package security

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestComposeHostPublicationsStayLoopbackOnly is the executable half of the
// container listener trade-off: ValidateContainerListener permits a wildcard
// in-container bind, so the Compose host publication is the real exposure
// boundary. Every published port must be bound to 127.0.0.1.
func TestComposeHostPublicationsStayLoopbackOnly(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "compose.yaml"))
	if err != nil {
		t.Fatalf("read compose.yaml: %v", err)
	}
	inPorts := false
	publications := 0
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "ports:") {
			inPorts = true
			continue
		}
		if !inPorts {
			continue
		}
		if !strings.HasPrefix(trimmed, "-") {
			inPorts = false
			continue
		}
		publications++
		entry := strings.Trim(strings.TrimSpace(trimmed[1:]), "\"'")
		if !strings.HasPrefix(entry, "127.0.0.1:") {
			t.Fatalf("host port publication %q is not bound to 127.0.0.1", entry)
		}
	}
	if publications == 0 {
		t.Fatal("compose.yaml declares no host port publication; the loopback check would be vacuous")
	}

	pg, err := os.ReadFile(filepath.Join("..", "..", "compose.postgres.yaml"))
	if err != nil {
		t.Fatalf("read compose.postgres.yaml: %v", err)
	}
	if strings.Contains(string(pg), "ports:") {
		t.Fatal("the PostgreSQL override must not publish any host port")
	}
}
