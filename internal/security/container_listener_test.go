package security

import "testing"

func TestValidateContainerListenerAcceptsOnlyWildcards(t *testing.T) {
	cases := []struct {
		address string
		wantErr bool
	}{
		{"0.0.0.0:8080", false},
		{"[::]:8080", false},
		{"127.0.0.1:8080", true},
		{"192.168.1.10:8080", true},
		{"localhost:8080", true},
		{":8080", true},
		{"0.0.0.0", true},
		{"not-an-address", true},
	}
	for _, tt := range cases {
		t.Run(tt.address, func(t *testing.T) {
			err := ValidateContainerListener(tt.address)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateContainerListener(%q) error = %v, wantErr %t", tt.address, err, tt.wantErr)
			}
		})
	}
}
