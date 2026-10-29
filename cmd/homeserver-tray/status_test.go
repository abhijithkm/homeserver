package main

import (
	"bytes"
	"image/png"
	"testing"

	"golang.org/x/sys/windows"
)

func TestDecide(t *testing.T) {
	yes := func() bool { return true }
	no := func() bool { return false }
	cases := []struct {
		name  string
		svc   svcInfo
		probe func() bool
		want  state
	}{
		{"running and answering", svcInfo{true, windows.SERVICE_RUNNING}, yes, stRunning},
		{"running but silent", svcInfo{true, windows.SERVICE_RUNNING}, no, stProblem},
		{"starting", svcInfo{true, windows.SERVICE_START_PENDING}, no, stBusy},
		{"stopping", svcInfo{true, windows.SERVICE_STOP_PENDING}, yes, stBusy},
		{"stopped", svcInfo{true, windows.SERVICE_STOPPED}, no, stStopped},
		{"not installed, nothing answers", svcInfo{}, no, stMissing},
		{"not installed, run by hand", svcInfo{}, yes, stRunning},
	}
	for _, c := range cases {
		if got := decide(c.svc, c.probe); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
}

func TestDecideSkipsProbeWhenServiceStateIsEnough(t *testing.T) {
	probed := false
	p := func() bool { probed = true; return true }
	decide(svcInfo{true, windows.SERVICE_STOPPED}, p)
	decide(svcInfo{true, windows.SERVICE_START_PENDING}, p)
	if probed {
		t.Fatal("probe ran although the service state decides the result")
	}
}

func TestEveryStateHasItsOwnValidIcon(t *testing.T) {
	all := []state{stChecking, stRunning, stBusy, stStopped, stProblem, stMissing}
	seen := map[string]state{}
	for s, b := range icons() {
		if len(b) < 23 || b[0] != 0 || b[2] != 1 {
			t.Fatalf("state %d: not an ICO", s)
		}
		if _, err := png.Decode(bytes.NewReader(b[22:])); err != nil {
			t.Fatalf("state %d: embedded PNG invalid: %v", s, err)
		}
		if other, dup := seen[string(b)]; dup {
			t.Fatalf("states %d and %d share an icon", s, other)
		}
		seen[string(b)] = s
	}
	if len(seen) != len(all) {
		t.Fatalf("got %d icons, want %d", len(seen), len(all))
	}
	for _, s := range all {
		if s.label("") == "" {
			t.Errorf("state %d has no label", s)
		}
	}
}
