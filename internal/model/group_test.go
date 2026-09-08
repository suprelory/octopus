package model

import "testing"

func TestGroupModeValid(t *testing.T) {
	tests := []struct {
		name string
		mode GroupMode
		want bool
	}{
		{name: "round robin", mode: GroupModeRoundRobin, want: true},
		{name: "failover", mode: GroupModeFailover, want: true},
		{name: "weighted", mode: GroupModeWeighted, want: true},
		{name: "unknown", mode: GroupMode(99), want: false},
		{name: "unset", mode: GroupMode(0), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.mode.Valid(); got != tt.want {
				t.Fatalf("Valid() = %t, want %t", got, tt.want)
			}
		})
	}
}
