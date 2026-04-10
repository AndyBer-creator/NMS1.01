package main

import (
	"testing"
)

func TestTrapListenPort(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want uint16
	}{
		{name: "default", env: "", want: 162},
		{name: "custom", env: "4162", want: 4162},
		{name: "invalid falls back", env: "not-a-port", want: 162},
		{name: "out of uint16 range", env: "999999", want: 162},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.env != "" {
				t.Setenv("TRAP_PORT", tc.env)
			} else {
				t.Setenv("TRAP_PORT", "")
			}
			if got := trapListenPort(); got != tc.want {
				t.Fatalf("got %d want %d", got, tc.want)
			}
		})
	}
}
