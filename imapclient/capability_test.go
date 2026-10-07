package imapclient_test

import (
	"bufio"
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// TestCapability_rounds checks the case-insensitive capability view across
// two full CAPABILITY response rounds and one half response which fails
// parsing. The capability set must only be updated once a whole CAPABILITY
// response has been received: the failed round must not leave a partial
// capability set behind.
func TestCapability_rounds(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()

	client := imapclient.New(clientConn, nil)
	defer client.Close()

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- capabilityScript(serverConn)
	}()

	if err := client.WaitGreeting(); err != nil {
		t.Fatalf("WaitGreeting() = %v", err)
	}

	// The greeting carries the initial capabilities
	if caps := client.Caps(); !caps.Has(imap.CapIMAP4rev1) {
		t.Errorf("after greeting: Caps().Has(IMAP4rev1) = false")
	}
	if orig, ok := client.CapView().Get("imap4rev1"); !ok || orig != "IMAP4rev1" {
		t.Errorf("after greeting: CapView().Get(imap4rev1) = %q, %v, want %q, true", orig, ok, "IMAP4rev1")
	}

	// Round 1: mixed-case capabilities, duplicates and AUTH= parameters
	cmd := client.Capability()
	view, err := cmd.WaitCaps()
	if err != nil {
		t.Fatalf("round 1: WaitCaps() = %v", err)
	}
	if got := view.Params("auth"); !reflect.DeepEqual(got, []string{"PLAIN", "login"}) {
		t.Errorf("round 1: Params(auth) = %v, want %v", got, []string{"PLAIN", "login"})
	}
	if orig, ok := view.Get("auth=PLAIN"); !ok || orig != "AUTH=PLAIN" {
		t.Errorf("round 1: Get(auth=PLAIN) = %q, %v, want %q, true", orig, ok, "AUTH=PLAIN")
	}
	if orig, ok := view.Get("AUTH=LOGIN"); !ok || orig != "auth=login" {
		t.Errorf("round 1: Get(AUTH=LOGIN) = %q, %v, want %q, true", orig, ok, "auth=login")
	}
	wantList := []imap.Cap{"IMAP4rev2", "AUTH=PLAIN", "auth=login", "X-Case"}
	if got := view.List(); !reflect.DeepEqual(got, wantList) {
		t.Errorf("round 1: List() = %v, want %v", got, wantList)
	}
	// Old capability view, with canonical names and implied capabilities
	caps, err := cmd.Wait()
	if err != nil {
		t.Fatalf("round 1: Wait() = %v", err)
	}
	for _, c := range []imap.Cap{imap.CapIMAP4rev2, imap.AuthCap("PLAIN"), imap.AuthCap("LOGIN"), "X-CASE", imap.CapEnable} {
		if !caps.Has(c) {
			t.Errorf("round 1: Caps().Has(%q) = false, want true", c)
		}
	}
	if !client.Caps().Has(imap.AuthCap("PLAIN")) || !client.CapView().Has("x-CASE") {
		t.Errorf("round 1: client capability views not updated")
	}

	// Round 2: a new response replaces the previous capabilities
	cmd2 := client.Capability()
	caps2, err := cmd2.Wait()
	if err != nil {
		t.Fatalf("round 2: Wait() = %v", err)
	}
	if caps2.Has(imap.AuthCap("PLAIN")) {
		t.Errorf("round 2: Caps().Has(AUTH=PLAIN) = true, want false")
	}
	if !caps2.Has(imap.AuthCap("SCRAM-SHA-256")) {
		t.Errorf("round 2: Caps().Has(AUTH=SCRAM-SHA-256) = false, want true")
	}
	view2, err := cmd2.WaitCaps()
	if err != nil {
		t.Fatalf("round 2: WaitCaps() = %v", err)
	}
	if got := view2.Params("AUTH"); !reflect.DeepEqual(got, []string{"SCRAM-SHA-256"}) {
		t.Errorf("round 2: Params(AUTH) = %v, want %v", got, []string{"SCRAM-SHA-256"})
	}
	if view2.Has("x-case") {
		t.Errorf("round 2: Has(x-case) = true, want false")
	}

	// Round 3: the response fails parsing halfway through
	cmd3 := client.Capability()
	if _, err := cmd3.Wait(); err == nil {
		t.Fatalf("round 3: Wait() = nil, want error")
	}
	// The failed command doesn't carry a partial capability set
	if view, err := cmd3.WaitCaps(); err == nil || view != nil {
		t.Errorf("round 3: WaitCaps() = %v, %v, want nil, error", view, err)
	}
	// The failed round must not leave a partial capability set behind:
	// the round 2 capabilities are still in effect
	if caps := client.Caps(); !caps.Has(imap.AuthCap("SCRAM-SHA-256")) || caps.Has(imap.AuthCap("OAUTHBEARER")) {
		t.Errorf("round 3: Caps() = %v, want round 2 capabilities", caps)
	}
	if got := client.CapView().Params("AUTH"); !reflect.DeepEqual(got, []string{"SCRAM-SHA-256"}) {
		t.Errorf("round 3: Params(AUTH) = %v, want %v", got, []string{"SCRAM-SHA-256"})
	}

	if err := <-serverErr; err != nil {
		t.Fatalf("server script: %v", err)
	}
}

// capabilityScript plays the server side of TestCapability_rounds.
func capabilityScript(conn net.Conn) error {
	r := bufio.NewReader(conn)
	if _, err := fmt.Fprintf(conn, "* OK [CAPABILITY IMAP4rev1] ready\r\n"); err != nil {
		return err
	}

	rounds := []string{
		"* CAPABILITY IMAP4rev2 AUTH=PLAIN auth=login X-Case x-case\r\n",
		"* CAPABILITY IMAP4rev2 AUTH=SCRAM-SHA-256\r\n",
		"* CAPABILITY IMAP4rev2 AUTH=OAUTHBEARER )\r\n", // malformed: half a round
	}
	for i, resp := range rounds {
		tag, err := readCapabilityCommand(r)
		if err != nil {
			return fmt.Errorf("round %d: %w", i+1, err)
		}
		if _, err := fmt.Fprintf(conn, resp); err != nil {
			return err
		}
		if i < len(rounds)-1 {
			if _, err := fmt.Fprintf(conn, "%s OK CAPABILITY completed\r\n", tag); err != nil {
				return err
			}
		}
	}
	return nil
}

// readCapabilityCommand reads a single CAPABILITY command line and returns
// its tag.
func readCapabilityCommand(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	fields := strings.Fields(line)
	if len(fields) != 2 || !strings.EqualFold(fields[1], "CAPABILITY") {
		return "", fmt.Errorf("unexpected command %q", strings.TrimSpace(line))
	}
	return fields[0], nil
}
