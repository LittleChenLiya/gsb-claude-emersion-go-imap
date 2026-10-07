package imapclient

import (
	"fmt"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/internal/imapwire"
)

// Capability sends a CAPABILITY command.
func (c *Client) Capability() *CapabilityCommand {
	cmd := &CapabilityCommand{}
	c.beginCommand("CAPABILITY", cmd).end()
	return cmd
}

func (c *Client) handleCapability() error {
	caps, err := readCapabilities(c.dec)
	if err != nil {
		return err
	}
	c.setCaps(caps)
	if cmd := findPendingCmdByType[*CapabilityCommand](c); cmd != nil {
		cmd.caps = caps
	}
	return nil
}

// CapabilityCommand is a CAPABILITY command.
type CapabilityCommand struct {
	commandBase
	caps *imap.Caps
}

// Wait blocks until the CAPABILITY command has completed and returns the
// capabilities with canonical capability names.
func (cmd *CapabilityCommand) Wait() (imap.CapSet, error) {
	err := cmd.wait()
	return cmd.caps.CapSet(), err
}

// WaitCaps blocks until the CAPABILITY command has completed and returns a
// case-insensitive view of the capabilities which preserves their original
// spelling.
func (cmd *CapabilityCommand) WaitCaps() (*imap.Caps, error) {
	err := cmd.wait()
	return cmd.caps, err
}

// readCapabilities reads capability-data.
//
// The capabilities only take effect once the whole response has been
// parsed: if an error occurs, nil is returned so that a partial capability
// set is never applied.
func readCapabilities(dec *imapwire.Decoder) (*imap.Caps, error) {
	caps := imap.NewCaps()
	for dec.SP() {
		// Some IMAP servers send multiple SP between caps:
		// https://github.com/emersion/go-imap/pull/652
		for dec.SP() {
		}

		var name string
		if !dec.ExpectAtom(&name) {
			return nil, fmt.Errorf("in capability-data: %w", dec.Err())
		}
		caps.Add(imap.Cap(name))
	}
	return caps, nil
}
