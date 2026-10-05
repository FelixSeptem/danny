package security

import "testing"

func TestListenerPolicyClassifiesAddresses(t *testing.T) {
	tests := []struct {
		name          string
		address       string
		authenticated bool
		wantErr       bool
	}{
		{name: "ipv4 loopback", address: "127.0.0.1:8080"},
		{name: "ipv6 loopback", address: "[::1]:8080"},
		{name: "localhost", address: "localhost:8080"},
		{name: "wildcard", address: "0.0.0.0:8080", wantErr: true},
		{name: "lan", address: "192.168.1.10:8080", wantErr: true},
		{name: "public", address: "203.0.113.10:8080", wantErr: true},
		{name: "wildcard authenticated", address: "0.0.0.0:8080", authenticated: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateListener(tt.address, tt.authenticated)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateListener(%q, %t) error = %v, wantErr %t", tt.address, tt.authenticated, err, tt.wantErr)
			}
		})
	}
}

func TestListenerPolicyRejectsMalformedAddress(t *testing.T) {
	if err := ValidateListener("not-an-address", false); err == nil {
		t.Fatal("ValidateListener() error = nil, want malformed address error")
	}
}
