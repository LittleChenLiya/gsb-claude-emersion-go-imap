package imapclient

import (
	"github.com/emersion/go-imap/v2"
)

// commandTracker is the client's explicit response attribution layer.
//
// The tracker decides which in-flight command a server response belongs
// to:
//
//   - Tagged completions are matched to their originating command by
//     exact tag, never by recency. A tagged response carrying an unknown
//     tag is rejected with an explicit diagnostic.
//   - Untagged responses are dispatched by protocol category (mailbox
//     data, capability updates, notifications, ...). Notifications such
//     as EXISTS are never treated as a command's response: they update
//     the connection state and are delivered to UnilateralDataHandler,
//     unless a command of the matching protocol category is explicitly
//     waiting for them.
//   - Termination (untagged BYE or connection loss) completes all
//     undecided commands exactly once.
//
// All methods must be called with Client.mutex held.
type commandTracker struct {
	pending []command          // in-flight commands, in send order
	byTag   map[string]command // exact tag lookup
}

// register starts tracking a new in-flight command.
func (t *commandTracker) register(cmd command) {
	if t.byTag == nil {
		t.byTag = make(map[string]command)
	}
	t.pending = append(t.pending, cmd)
	t.byTag[cmd.base().tag] = cmd
}

// deleteByTag stops tracking the command with the exact given tag and
// returns it. If no in-flight command carries this tag, nil is returned.
func (t *commandTracker) deleteByTag(tag string) command {
	cmd, ok := t.byTag[tag]
	if !ok {
		return nil
	}
	delete(t.byTag, tag)
	for i, pending := range t.pending {
		if pending == cmd {
			t.pending = append(t.pending[:i], t.pending[i+1:]...)
			break
		}
	}
	return cmd
}

// findFunc returns the first in-flight command matching f, or nil.
func (t *commandTracker) findFunc(f func(cmd command) bool) command {
	for _, cmd := range t.pending {
		if f(cmd) {
			return cmd
		}
	}
	return nil
}

// findByType returns the first in-flight command of type T, or the zero
// value of T.
func findTrackedCmdByType[T command](t *commandTracker) T {
	for _, cmd := range t.pending {
		if cmd, ok := cmd.(T); ok {
			return cmd
		}
	}
	var zero T
	return zero
}

// drain stops tracking all in-flight commands for which keep returns
// false and returns them, in send order.
func (t *commandTracker) drain(keep func(cmd command) bool) []command {
	var drained, remaining []command
	for _, cmd := range t.pending {
		if keep != nil && keep(cmd) {
			remaining = append(remaining, cmd)
			continue
		}
		delete(t.byTag, cmd.base().tag)
		drained = append(drained, cmd)
	}
	t.pending = remaining
	return drained
}

// completeCommand finishes a command with the provided result.
//
// A command is completed exactly once: if it has already been completed
// (e.g. by an untagged BYE followed by a connection close), this is a
// no-op.
func (c *Client) completeCommand(cmd command, err error) {
	base := cmd.base()
	if !base.completed.CompareAndSwap(false, true) {
		return
	}

	base.done <- err
	close(base.done)

	// Ensure the command is not blocked waiting on continuation requests
	c.mutex.Lock()
	var filtered []continuationRequest
	for _, contReq := range c.contReqs {
		if contReq.cmd != cmd.base() {
			filtered = append(filtered, contReq)
		} else {
			contReq.Cancel(err)
		}
	}
	c.contReqs = filtered
	c.mutex.Unlock()

	switch cmd := cmd.(type) {
	case *authenticateCommand, *loginCommand:
		if err == nil {
			c.setState(imap.ConnStateAuthenticated)
		}
	case *unauthenticateCommand:
		if err == nil {
			c.mutex.Lock()
			c.state = imap.ConnStateNotAuthenticated
			c.mailbox = nil
			c.enabled = make(imap.CapSet)
			c.mutex.Unlock()
		}
	case *SelectCommand:
		if err == nil {
			c.mutex.Lock()
			c.state = imap.ConnStateSelected
			c.mailbox = &SelectedMailbox{
				Name:           cmd.mailbox,
				NumMessages:    cmd.data.NumMessages,
				Flags:          cmd.data.Flags,
				PermanentFlags: cmd.data.PermanentFlags,
			}
			c.mutex.Unlock()
		}
	case *unselectCommand:
		if err == nil {
			c.setState(imap.ConnStateAuthenticated)
		}
	case *logoutCommand:
		if err == nil {
			c.setState(imap.ConnStateLogout)
		}
	case *ListCommand:
		if cmd.pendingData != nil {
			cmd.mailboxes <- cmd.pendingData
		}
		close(cmd.mailboxes)
	case *FetchCommand:
		close(cmd.msgs)
	case *ExpungeCommand:
		close(cmd.seqNums)
	}
}

// abortPendingCommands terminates all undecided commands with err. Each
// command is completed exactly once, even if the connection is closed
// afterwards.
//
// A pending LOGOUT command is left untouched: per RFC 9051 section 6.3.8,
// the server still sends a tagged completion for LOGOUT after the
// untagged BYE.
func (c *Client) abortPendingCommands(err error) {
	c.mutex.Lock()
	pendingCmds := c.tracker.drain(func(cmd command) bool {
		_, isLogout := cmd.(*logoutCommand)
		return isLogout
	})
	c.mutex.Unlock()

	for _, cmd := range pendingCmds {
		c.completeCommand(cmd, err)
	}
}
