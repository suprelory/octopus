package model

import "testing"

func TestGroupModeNormalizeRemovesLegacyRandomMode(t *testing.T) {
	tests := []struct {
		name string
		mode GroupMode
		want GroupMode
	}{
		{name: "round robin", mode: GroupModeRoundRobin, want: GroupModeRoundRobin},
		{name: "failover", mode: GroupModeFailover, want: GroupModeFailover},
		{name: "weighted", mode: GroupModeWeighted, want: GroupModeWeighted},
		{name: "legacy random", mode: GroupMode(2), want: GroupModeRoundRobin},
		{name: "unset", mode: GroupMode(0), want: GroupModeRoundRobin},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.mode.Normalize(); got != tt.want {
				t.Fatalf("Normalize() = %d, want %d", got, tt.want)
			}
		})
	}
}
