package imapclient_test

import (
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// newScriptedClient creates an IMAP client connected to a scripted,
// in-memory server. It returns the client and a writer the test uses to
// feed raw server responses. Commands sent by the client are drained in
// the background.
func newScriptedClient(t *testing.T, options *imapclient.Options) (*imapclient.Client, io.Writer) {
	t.Helper()

	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() { serverConn.Close() })

	// Drain commands written by the client
	go func() {
		io.Copy(io.Discard, serverConn)
	}()

	client := imapclient.New(clientConn, options)
	t.Cleanup(func() { client.Close() })
	return client, serverConn
}

// TestCommandAttribution exercises the client's explicit response
// attribution layer with a scripted six-response session:
//
//  1. "* OK [CAPABILITY IMAP4rev2] ready" - greeting
//  2. "* 7 EXISTS" - notification, not any command's response
//  3. "T2 NO ..." - tagged completion, delivered out of order
//  4. "T1 OK ..." - tagged completion
//  5. "* OK ..." - notification
//  6. "* BYE ..." - terminates all undecided commands, exactly once
func TestCommandAttribution(t *testing.T) {
	mailboxCh := make(chan *imapclient.UnilateralDataMailbox, 8)
	options := &imapclient.Options{
		UnilateralDataHandler: &imapclient.UnilateralDataHandler{
			Mailbox: func(data *imapclient.UnilateralDataMailbox) {
				mailboxCh <- data
			},
		},
	}
	client, w := newScriptedClient(t, options)

	// Response 1: greeting
	io.WriteString(w, "* OK [CAPABILITY IMAP4rev2] ready\r\n")
	if err := client.WaitGreeting(); err != nil {
		t.Fatalf("WaitGreeting() = %v", err)
	}

	// Four interleaved commands, tagged T1 to T4
	cmd1 := client.Noop()
	cmd2 := client.Noop()
	cmd3 := client.Noop()
	cmd4 := client.Noop()

	// Responses 2-6
	io.WriteString(w, "* 7 EXISTS\r\n")
	io.WriteString(w, "T2 NO second command failed\r\n")
	io.WriteString(w, "T1 OK first command done\r\n")
	io.WriteString(w, "* OK unsolicited status\r\n")
	io.WriteString(w, "* BYE server is closing\r\n")

	// Each tagged completion is attributed to its originating command,
	// even though they completed in reverse order
	if err := cmd1.Wait(); err != nil {
		t.Errorf("cmd1.Wait() = %v, want nil", err)
	}
	var imapErr *imap.Error
	if err := cmd2.Wait(); !errors.As(err, &imapErr) || imapErr.Type != imap.StatusResponseTypeNo {
		t.Errorf("cmd2.Wait() = %v, want NO error", err)
	} else if imapErr.Text != "second command failed" {
		t.Errorf("cmd2 error text = %q, want %q", imapErr.Text, "second command failed")
	}

	// The EXISTS event is a notification: it was not attributed to any of
	// the in-flight commands
	select {
	case data := <-mailboxCh:
		if data.NumMessages == nil || *data.NumMessages != 7 {
			t.Errorf("UnilateralDataMailbox.NumMessages = %v, want 7", data.NumMessages)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("EXISTS notification not delivered to UnilateralDataHandler")
	}

	// BYE terminated the two remaining undecided commands, exactly once
	for i, cmd := range []*imapclient.Command{cmd3, cmd4} {
		err := cmd.Wait()
		if !errors.As(err, &imapErr) || imapErr.Type != imap.StatusResponseTypeBye {
			t.Errorf("cmd%v.Wait() = %v, want BYE error", i+3, err)
		} else if imapErr.Text != "server is closing" {
			t.Errorf("cmd%v error text = %q, want %q", i+3, imapErr.Text, "server is closing")
		}
		// Waiting again yields the very same result: no duplicate
		// completion happened when the connection was torn down
		if again := cmd.Wait(); again != err {
			t.Errorf("cmd%v.Wait() called twice = %v then %v", i+3, err, again)
		}
	}
}

// TestUnknownTaggedResponse checks that a tagged response carrying a tag
// which matches no in-flight command produces an explicit diagnostic
// naming the offending tag.
func TestUnknownTaggedResponse(t *testing.T) {
	client, w := newScriptedClient(t, nil)

	io.WriteString(w, "* OK [CAPABILITY IMAP4rev2] ready\r\n")
	if err := client.WaitGreeting(); err != nil {
		t.Fatalf("WaitGreeting() = %v", err)
	}

	cmd := client.Noop() // tag T1

	// The server replies with a tag matching no pending command
	io.WriteString(w, "T9 OK bogus\r\n")

	err := cmd.Wait()
	if err == nil {
		t.Fatal("cmd.Wait() = nil, want unknown tag error")
	}
	if !strings.Contains(err.Error(), "unknown tag") || !strings.Contains(err.Error(), `"T9"`) {
		t.Errorf("cmd.Wait() = %q, want explicit diagnostic naming unknown tag T9", err)
	}
}
