package model

import "testing"

func TestChannelAllowsPassthrough(t *testing.T) {
	if !(&Channel{}).AllowsPassthrough() {
		t.Fatal("zero-value channel must preserve the legacy automatic passthrough behavior")
	}
	if (&Channel{PassthroughMode: ChannelPassthroughModeOff}).AllowsPassthrough() {
		t.Fatal("off mode must disable optional passthrough")
	}
}
