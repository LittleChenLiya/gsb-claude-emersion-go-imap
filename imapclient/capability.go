package imapclient

import (
	"fmt"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/internal"
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
	caps imap.CapSet
}

func (cmd *CapabilityCommand) Wait() (imap.CapSet, error) {
	err := cmd.wait()
	return cmd.caps, err
}

func readCapabilities(dec *imapwire.Decoder) (imap.CapSet, error) {
	caps := make(imap.CapSet)
	for dec.SP() {
		// Some IMAP servers send multiple SP between caps:
		// https://github.com/emersion/go-imap/pull/652
		for dec.SP() {
		}

		// Preserve the original spelling: capability names are
		// case-insensitive and CapSet lookups fold case.
		cap, err := internal.ExpectCapRaw(dec)
		if err != nil {
			// Don't return a partially populated set: the caller must
			// only apply the new capabilities once a whole CAPABILITY
			// response has been successfully parsed.
			return nil, fmt.Errorf("in capability-data: %w", err)
		}
		caps.Add(cap)
	}
	return caps, nil
}
