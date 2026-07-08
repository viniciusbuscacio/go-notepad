package apiserver

import (
	"net"
	"testing"
)

func TestNormalizeCIDR(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"127.0.0.1", "127.0.0.1/32", true},
		{"192.168.0.10/32", "192.168.0.10/32", true},
		{"192.168.1.7/24", "192.168.1.0/24", true},
		{" 10.0.0.0/8 ", "10.0.0.0/8", true},
		{"", "", false},
		{"nope", "", false},
		{"999.1.1.1/32", "", false},
	}
	for _, tc := range cases {
		got, err := NormalizeCIDR(tc.in)
		if tc.ok && err != nil {
			t.Errorf("NormalizeCIDR(%q) erro inesperado: %v", tc.in, err)
		}
		if !tc.ok && err == nil {
			t.Errorf("NormalizeCIDR(%q) esperava erro", tc.in)
		}
		if tc.ok && got != tc.want {
			t.Errorf("NormalizeCIDR(%q) = %q, quer %q", tc.in, got, tc.want)
		}
	}
}

func TestIPAllowed(t *testing.T) {
	nets, err := parseCIDRs([]string{"127.0.0.1/32", "192.168.1.0/24"})
	if err != nil {
		t.Fatalf("parseCIDRs: %v", err)
	}
	cases := []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", true},
		{"192.168.1.55", true},
		{"192.168.2.1", false},
		{"10.0.0.1", false},
	}
	for _, tc := range cases {
		if got := ipAllowed(net.ParseIP(tc.ip), nets); got != tc.want {
			t.Errorf("ipAllowed(%s) = %v, quer %v", tc.ip, got, tc.want)
		}
	}
}
