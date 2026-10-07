package imapclient

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/internal/imapwire"
)

// TestReadCapabilities_noPartialSetOnError checks that a CAPABILITY response
// which fails parsing midway doesn't produce a partially populated set.
func TestReadCapabilities_noPartialSetOnError(t *testing.T) {
	dec := imapwire.NewDecoder(bufio.NewReader(strings.NewReader(` IMAP4rev1 AUTH= "broken`)), imapwire.ConnSideClient)
	caps, err := readCapabilities(dec)
	if err == nil {
		t.Fatal("readCapabilities() = nil error, want error")
	}
	if caps != nil {
		t.Fatalf("readCapabilities() returned a partial capability set on error: %v", caps)
	}
}

// TestClient_capsSurviveFailedCapabilityRound checks that a failed CAPABILITY
// response doesn't leave half a capability set behind: the client keeps
// exposing the capabilities from the last successful round.
func TestClient_capsSurviveFailedCapabilityRound(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()

	c := New(clientConn, nil)
	defer c.Close()

	rw := bufio.NewReadWriter(bufio.NewReader(serverConn), bufio.NewWriter(serverConn))
	write := func(s string) {
		t.Helper()
		if _, err := rw.WriteString(s); err != nil {
			t.Fatalf("server: WriteString() = %v", err)
		}
		if err := rw.Flush(); err != nil {
			t.Fatalf("server: Flush() = %v", err)
		}
	}
	readCmdTag := func() string {
		t.Helper()
		line, err := rw.ReadString('\n')
		if err != nil {
			t.Fatalf("server: ReadString() = %v", err)
		}
		tag, _, _ := strings.Cut(strings.TrimSpace(line), " ")
		return tag
	}

	write("* OK [CAPABILITY IMAP4rev1] ready\r\n")

	// One full successful round
	type result struct {
		caps imap.CapSet
		err  error
	}
	cmdRes := make(chan result, 1)
	go func() {
		caps, err := c.Capability().Wait()
		cmdRes <- result{caps, err}
	}()
	tag := readCmdTag()
	write("* CAPABILITY IMAP4rev2 AUTH=SCRAM-SHA-256\r\n" + tag + " OK done\r\n")
	select {
	case res := <-cmdRes:
		if res.err != nil {
			t.Fatalf("Capability().Wait() = %v", res.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for CAPABILITY command")
	}

	// Half round: truncated CAPABILITY response
	go func() {
		caps, err := c.Capability().Wait()
		cmdRes <- result{caps, err}
	}()
	readCmdTag()
	write(`* CAPABILITY IMAP4rev1 AUTH= "broken` + "\r\n")
	select {
	case res := <-cmdRes:
		if res.err == nil {
			t.Fatal("Capability().Wait() = nil, want error")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for CAPABILITY command")
	}

	// The failed round must not have touched the client's capability set
	c.mutex.Lock()
	caps := c.caps
	c.mutex.Unlock()

	if caps == nil {
		t.Fatal("client capabilities reset after failed round")
	}
	if !caps.Has(imap.CapIMAP4rev2) {
		t.Errorf("client lost capabilities from the last successful round (caps = %v)", caps)
	}
	if _, ok := caps.Find(imap.CapIMAP4rev1); ok {
		t.Errorf("client capabilities contain entries from the failed round (caps = %v)", caps)
	}
	if want := 2; len(caps) != want {
		t.Errorf("len(caps) = %v, want %v (caps = %v)", len(caps), want, caps)
	}
}
