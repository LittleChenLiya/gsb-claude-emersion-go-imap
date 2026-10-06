package imapclient

import (
	"errors"
	"testing"
)

// TestCompleteCommandOnce ensures a command is finished exactly once:
// repeated completions (e.g. BYE followed by connection close) are
// no-ops and keep the original result.
func TestCompleteCommandOnce(t *testing.T) {
	c := new(Client)
	cmd := &Command{}
	cmd.done = make(chan error, 1)

	c.completeCommand(cmd, nil)
	c.completeCommand(cmd, errors.New("duplicate completion")) // must be a no-op

	if err := cmd.Wait(); err != nil {
		t.Fatalf("Wait() = %v, want nil", err)
	}
}

// TestCommandTrackerTagAttribution ensures tagged completions are matched
// to their originating command by exact tag, in any order.
func TestCommandTrackerTagAttribution(t *testing.T) {
	var tr commandTracker
	cmd1 := &Command{}
	cmd1.tag = "T1"
	cmd2 := &Command{}
	cmd2.tag = "T2"
	tr.register(cmd1)
	tr.register(cmd2)

	// Out-of-order completion still matches the exact tag
	if got := tr.deleteByTag("T2"); got != cmd2 {
		t.Fatalf("deleteByTag(T2) = %v, want cmd2", got)
	}
	// An unknown tag matches nothing
	if got := tr.deleteByTag("T9"); got != nil {
		t.Fatalf("deleteByTag(T9) = %v, want nil", got)
	}
	if got := tr.deleteByTag("T1"); got != cmd1 {
		t.Fatalf("deleteByTag(T1) = %v, want cmd1", got)
	}
	if len(tr.pending) != 0 || len(tr.byTag) != 0 {
		t.Fatalf("tracker not empty after draining: pending=%v byTag=%v", tr.pending, tr.byTag)
	}
}

// TestCommandTrackerDrain ensures drain terminates all undecided commands
// while keeping the ones selected for survival (e.g. LOGOUT on BYE).
func TestCommandTrackerDrain(t *testing.T) {
	var tr commandTracker
	cmd1 := &Command{}
	cmd1.tag = "T1"
	cmd2 := &logoutCommand{}
	cmd2.tag = "T2"
	tr.register(cmd1)
	tr.register(cmd2)

	drained := tr.drain(func(cmd command) bool {
		_, isLogout := cmd.(*logoutCommand)
		return isLogout
	})
	if len(drained) != 1 || drained[0] != cmd1 {
		t.Fatalf("drain() = %v, want [cmd1]", drained)
	}
	if got := tr.deleteByTag("T2"); got != cmd2 {
		t.Fatalf("deleteByTag(T2) = %v, want logout command", got)
	}
}
