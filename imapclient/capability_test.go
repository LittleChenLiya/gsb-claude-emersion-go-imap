package imapclient_test

import (
	"bufio"
	"net"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// capabilityServer is a minimal in-memory fake IMAP server speaking just
// enough of the protocol to test CAPABILITY response parsing.
type capabilityServer struct {
	t  *testing.T
	rw *bufio.ReadWriter
}

func newCapabilityServer(t *testing.T, conn net.Conn) *capabilityServer {
	return &capabilityServer{
		t:  t,
		rw: bufio.NewReadWriter(bufio.NewReader(conn), bufio.NewWriter(conn)),
	}
}

func (s *capabilityServer) writeLine(line string) {
	s.t.Helper()
	if _, err := s.rw.WriteString(line + "\r\n"); err != nil {
		s.t.Fatalf("server: WriteString() = %v", err)
	}
	if err := s.rw.Flush(); err != nil {
		s.t.Fatalf("server: Flush() = %v", err)
	}
}

// readCmd reads a command sent by the client and returns its tag.
func (s *capabilityServer) readCmd() string {
	s.t.Helper()
	line, err := s.rw.ReadString('\n')
	if err != nil {
		s.t.Fatalf("server: ReadString() = %v", err)
	}
	tag, _, _ := strings.Cut(strings.TrimSpace(line), " ")
	return tag
}

type capabilityResult struct {
	caps imap.CapSet
	err  error
}

// TestCapability_caseInsensitiveView exercises two full CAPABILITY response
// rounds plus one truncated (failing) round. It checks that:
//
//   - capabilities are deduplicated case-insensitively without losing their
//     original spelling,
//   - parameterized capabilities (AUTH=) still expose their parameter,
//   - lookups via the existing Has/Caps interface are case-insensitive,
//   - an update only takes effect once a whole CAPABILITY response has been
//     parsed: a failing round leaves the previous capability set intact.
func TestCapability_caseInsensitiveView(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()

	client := imapclient.New(clientConn, nil)
	defer client.Close()

	server := newCapabilityServer(t, serverConn)

	// Send the CAPABILITY command from a separate goroutine: net.Pipe is
	// synchronous, so the client cannot write the command while the test
	// goroutine is busy playing the server side.
	capabilityAsync := func() <-chan capabilityResult {
		ch := make(chan capabilityResult, 1)
		go func() {
			caps, err := client.Capability().Wait()
			ch <- capabilityResult{caps, err}
		}()
		return ch
	}
	waitResult := func(ch <-chan capabilityResult) capabilityResult {
		t.Helper()
		select {
		case res := <-ch:
			return res
		case <-time.After(10 * time.Second):
			t.Fatal("timeout waiting for CAPABILITY command")
			return capabilityResult{}
		}
	}

	// Greeting advertises mixed-case capabilities
	server.writeLine("* OK [CAPABILITY IMAP4rev1 auth=plain] ready")

	// Round 1: mixed-case capabilities, including duplicates which only
	// differ by case
	res1 := capabilityAsync()
	if tag := server.readCmd(); tag != "" {
		server.writeLine("* CAPABILITY imap4rev1 Auth=Plain AUTH=PLAIN move")
		server.writeLine(tag + " OK CAPABILITY done")
	}
	r1 := waitResult(res1)
	if r1.err != nil {
		t.Fatalf("Capability().Wait() = %v", r1.err)
	}
	caps1 := r1.caps

	// Duplicates differing only by case are folded into a single entry
	if want := 3; len(caps1) != want {
		t.Fatalf("len(caps) = %v, want %v (caps = %v)", len(caps1), want, caps1)
	}

	// The original spelling is preserved
	for _, tc := range []struct {
		query imap.Cap
		want  imap.Cap
	}{
		{imap.CapIMAP4rev1, "imap4rev1"},
		{imap.CapMove, "move"},
		{imap.AuthCap("PLAIN"), "Auth=Plain"},
		{imap.AuthCap("plain"), "Auth=Plain"},
	} {
		got, ok := caps1.Find(tc.query)
		if !ok {
			t.Errorf("Find(%q) not found", tc.query)
		} else if got != tc.want {
			t.Errorf("Find(%q) = %q, want %q", tc.query, got, tc.want)
		}
	}

	// The AUTH= parameter is still split out, keeping its original spelling
	if mechs := caps1.AuthMechanisms(); !reflect.DeepEqual(mechs, []string{"Plain"}) {
		t.Errorf("AuthMechanisms() = %v, want [Plain]", mechs)
	}

	// The existing query interface is case-insensitive
	if !caps1.Has(imap.CapMove) || !caps1.Has(imap.CapIMAP4rev1) || !caps1.Has(imap.AuthCap("PLAIN")) {
		t.Errorf("Has() should match capabilities case-insensitively (caps = %v)", caps1)
	}
	if !client.Caps().Has(imap.CapMove) {
		t.Errorf("Client.Caps().Has(MOVE) = false, want true")
	}

	// Round 2: a full new response replaces the previous capabilities
	res2 := capabilityAsync()
	if tag := server.readCmd(); tag != "" {
		server.writeLine("* CAPABILITY IMAP4rev2 AUTH=SCRAM-SHA-256")
		server.writeLine(tag + " OK CAPABILITY done")
	}
	r2 := waitResult(res2)
	if r2.err != nil {
		t.Fatalf("Capability().Wait() = %v", r2.err)
	}
	caps2 := r2.caps
	if !caps2.Has(imap.CapIMAP4rev2) {
		t.Errorf("caps2.Has(IMAP4rev2) = false, want true")
	}
	if mechs := caps2.AuthMechanisms(); !reflect.DeepEqual(mechs, []string{"SCRAM-SHA-256"}) {
		t.Errorf("caps2.AuthMechanisms() = %v, want [SCRAM-SHA-256]", mechs)
	}
	if _, ok := caps2.Find(imap.Cap("move")); ok {
		t.Errorf("caps2 should not contain capabilities from round 1")
	}
	if !client.Caps().Has(imap.CapIMAP4rev2) {
		t.Errorf("Client.Caps() not updated after round 2")
	}

	// The old capability view from round 1 is left untouched
	if !caps1.Has(imap.CapMove) || len(caps1) != 3 {
		t.Errorf("round 1 capability view changed after round 2 (caps = %v)", caps1)
	}

	// Half round: the server sends a truncated CAPABILITY response which
	// fails parsing midway through the capability list
	res3 := capabilityAsync()
	if tag := server.readCmd(); tag != "" {
		server.writeLine(`* CAPABILITY IMAP4rev1 AUTH= "broken`)
	}
	r3 := waitResult(res3)
	if r3.err == nil {
		t.Fatalf("Capability().Wait() = nil, want error")
	}
	if r3.caps != nil {
		t.Fatalf("failed round returned a partial capability set: %v", r3.caps)
	}

	// Note: the client still exposes the capabilities from the last
	// successful round after the failed one. This is verified by
	// TestClient_capsSurviveFailedCapabilityRound in
	// capability_internal_test.go, which inspects the client state
	// directly (Client.Caps cannot be used here because the connection
	// is torn down after the parse error).
}
