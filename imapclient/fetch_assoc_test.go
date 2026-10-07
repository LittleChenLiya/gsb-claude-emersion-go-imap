package imapclient_test

import (
	"bufio"
	"io"
	"net"
	"reflect"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// TestFetchNumAssoc checks that sequence numbers and UIDs are kept in
// separate key spaces and that responses without UID get an explicit
// unbound state.
func TestFetchNumAssoc(t *testing.T) {
	var assoc imapclient.FetchNumAssoc

	assoc.Add(1, 101)
	assoc.Add(2, 102)
	assoc.Add(3, 0) // response without UID

	if uid, ok := assoc.UID(1); !ok || uid != 101 {
		t.Errorf("UID(1) = %v, %v, want 101, true", uid, ok)
	}
	if seqNum, ok := assoc.SeqNum(101); !ok || seqNum != 1 {
		t.Errorf("SeqNum(101) = %v, %v, want 1, true", seqNum, ok)
	}

	// The two number spaces must not be mixed: a UID is not a valid
	// sequence number key and vice versa.
	if _, ok := assoc.UID(101); ok {
		t.Errorf("UID(101) = _, true, want _, false: UID leaked into sequence number space")
	}
	if _, ok := assoc.SeqNum(1); ok {
		t.Errorf("SeqNum(1) = _, true, want _, false: sequence number leaked into UID space")
	}

	// The response without UID must be reported as explicitly unbound.
	if uid, ok := assoc.UID(3); ok || uid != 0 {
		t.Errorf("UID(3) = %v, %v, want 0, false", uid, ok)
	}
	if !assoc.IsUnbound(3) {
		t.Errorf("IsUnbound(3) = false, want true")
	}
	if assoc.IsUnbound(1) {
		t.Errorf("IsUnbound(1) = true, want false")
	}
	if got, want := assoc.UnboundSeqNums(), []uint32{3}; !reflect.DeepEqual(got, want) {
		t.Errorf("UnboundSeqNums() = %v, want %v", got, want)
	}
	if got, want := assoc.Len(), 2; got != want {
		t.Errorf("Len() = %v, want %v", got, want)
	}

	// A later response carrying the UID binds the sequence number and
	// clears the unbound state.
	assoc.Add(3, 103)
	if uid, ok := assoc.UID(3); !ok || uid != 103 {
		t.Errorf("UID(3) = %v, %v, want 103, true", uid, ok)
	}
	if assoc.IsUnbound(3) {
		t.Errorf("IsUnbound(3) = true, want false")
	}

	// A later response without UID must not drop an existing binding.
	assoc.Add(3, 0)
	if uid, ok := assoc.UID(3); !ok || uid != 103 {
		t.Errorf("UID(3) = %v, %v, want 103, true", uid, ok)
	}
}

// TestFetchNumAssoc_expunge checks that EXPUNGE responses drop the expunged
// binding and shift higher sequence numbers down.
func TestFetchNumAssoc_expunge(t *testing.T) {
	var assoc imapclient.FetchNumAssoc
	assoc.Add(1, 101)
	assoc.Add(2, 102)
	assoc.Add(3, 103)
	assoc.Add(4, 0) // unbound

	assoc.Expunge(2)

	if _, ok := assoc.SeqNum(102); ok {
		t.Errorf("SeqNum(102) = _, true, want _, false: expunged UID still bound")
	}
	if uid, ok := assoc.UID(1); !ok || uid != 101 {
		t.Errorf("UID(1) = %v, %v, want 101, true", uid, ok)
	}
	if uid, ok := assoc.UID(2); !ok || uid != 103 {
		t.Errorf("UID(2) = %v, %v, want 103, true", uid, ok)
	}
	if seqNum, ok := assoc.SeqNum(103); !ok || seqNum != 2 {
		t.Errorf("SeqNum(103) = %v, %v, want 2, true", seqNum, ok)
	}
	if got, want := assoc.UnboundSeqNums(), []uint32{3}; !reflect.DeepEqual(got, want) {
		t.Errorf("UnboundSeqNums() = %v, want %v", got, want)
	}

	// Expunging the last message only drops its binding.
	assoc.Expunge(3)
	if got, want := assoc.UnboundSeqNums(), []uint32(nil); !reflect.DeepEqual(got, want) {
		t.Errorf("UnboundSeqNums() = %v, want %v", got, want)
	}
	if got, want := assoc.Len(), 2; got != want {
		t.Errorf("Len() = %v, want %v", got, want)
	}
}

// appendTestMessage appends a message to INBOX.
func appendTestMessage(t *testing.T, client *imapclient.Client) {
	t.Helper()
	appendCmd := client.Append("INBOX", int64(len(simpleRawMessage)), nil)
	if _, err := appendCmd.Write([]byte(simpleRawMessage)); err != nil {
		t.Fatalf("AppendCommand.Write() = %v", err)
	}
	if err := appendCmd.Close(); err != nil {
		t.Fatalf("AppendCommand.Close() = %v", err)
	}
	if _, err := appendCmd.Wait(); err != nil {
		t.Fatalf("AppendCommand.Wait() = %v", err)
	}
}

// TestFetchNumAssoc_memserver exercises the association helper against an
// in-memory server: FETCH responses with UID and sequence numbers after
// EXPUNGE. It also checks the collected FetchMessageBuffer values from the
// regular Fetch read path.
func TestFetchNumAssoc_memserver(t *testing.T) {
	client, server := newClientServerPair(t, imap.ConnStateSelected)
	defer client.Close()
	defer server.Close()

	// The fixture starts with one message, add two more: 3 total
	appendTestMessage(t, client)
	appendTestMessage(t, client)

	seqSet := imap.SeqSetNum(1, 2, 3)

	// FETCH with UID: sequence numbers get bound to UIDs
	var assoc imapclient.FetchNumAssoc
	msgs, err := client.Fetch(seqSet, &imap.FetchOptions{Flags: true, UID: true}).Collect()
	if err != nil {
		t.Fatalf("Fetch(FLAGS UID).Collect() = %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("len(msgs) = %v, want 3", len(msgs))
	}
	assoc.AddBuffers(msgs)
	for i, msg := range msgs {
		wantSeqNum := uint32(i + 1)
		// Check the regular Fetch read path
		if msg.SeqNum != wantSeqNum {
			t.Errorf("msg.SeqNum = %v, want %v", msg.SeqNum, wantSeqNum)
		}
		if msg.UID == 0 {
			t.Errorf("msg.UID = 0, want non-zero")
		}
		// Check the association table
		if uid, ok := assoc.UID(wantSeqNum); !ok || uid != msg.UID {
			t.Errorf("assoc.UID(%v) = %v, %v, want %v, true", wantSeqNum, uid, ok, msg.UID)
		}
		if seqNum, ok := assoc.SeqNum(msg.UID); !ok || seqNum != wantSeqNum {
			t.Errorf("assoc.SeqNum(%v) = %v, %v, want %v, true", msg.UID, seqNum, ok, wantSeqNum)
		}
		if assoc.IsUnbound(wantSeqNum) {
			t.Errorf("IsUnbound(%v) = true, want false", wantSeqNum)
		}
	}

	// EXPUNGE the first message: sequence numbers shift down
	storeFlags := imap.StoreFlags{
		Op:    imap.StoreFlagsAdd,
		Flags: []imap.Flag{imap.FlagDeleted},
	}
	if err := client.Store(imap.SeqSetNum(1), &storeFlags, nil).Close(); err != nil {
		t.Fatalf("Store() = %v", err)
	}
	seqNums, err := client.Expunge().Collect()
	if err != nil {
		t.Fatalf("Expunge().Collect() = %v", err)
	}
	if got, want := seqNums, []uint32{1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Expunge().Collect() = %v, want %v", got, want)
	}
	for _, seqNum := range seqNums {
		assoc.Expunge(seqNum)
	}

	// Check the association table against the server's new state
	msgs, err = client.Fetch(imap.SeqSetNum(1, 2), &imap.FetchOptions{UID: true}).Collect()
	if err != nil {
		t.Fatalf("Fetch(UID).Collect() = %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("len(msgs) = %v, want 2", len(msgs))
	}
	for _, msg := range msgs {
		uid, ok := assoc.UID(msg.SeqNum)
		if !ok || uid != msg.UID {
			t.Errorf("after EXPUNGE: assoc.UID(%v) = %v, %v, want %v, true", msg.SeqNum, uid, ok, msg.UID)
		}
		seqNum, ok := assoc.SeqNum(msg.UID)
		if !ok || seqNum != msg.SeqNum {
			t.Errorf("after EXPUNGE: assoc.SeqNum(%v) = %v, %v, want %v, true", msg.UID, seqNum, ok, msg.SeqNum)
		}
	}
	if got, want := assoc.Len(), 2; got != want {
		t.Errorf("after EXPUNGE: Len() = %v, want %v", got, want)
	}
}

// TestFetchNumAssoc_streaming feeds the association helper from the
// streaming Fetch read path (FetchCommand.Next / FetchMessageData.Next).
func TestFetchNumAssoc_streaming(t *testing.T) {
	client, server := newClientServerPair(t, imap.ConnStateSelected)
	defer client.Close()
	defer server.Close()

	var assoc imapclient.FetchNumAssoc
	fetchCmd := client.Fetch(imap.SeqSetNum(1), &imap.FetchOptions{
		UID:   true,
		Flags: true,
	})
	for {
		msg := fetchCmd.Next()
		if msg == nil {
			break
		}
		var uid imap.UID
		for {
			item := msg.Next()
			if item == nil {
				break
			}
			if item, ok := item.(imapclient.FetchItemDataUID); ok {
				uid = item.UID
			}
		}
		assoc.Add(msg.SeqNum, uid)
	}
	if err := fetchCmd.Close(); err != nil {
		t.Fatalf("FetchCommand.Close() = %v", err)
	}

	if uid, ok := assoc.UID(1); !ok || uid == 0 {
		t.Errorf("assoc.UID(1) = %v, %v, want non-zero, true", uid, ok)
	}
	if assoc.IsUnbound(1) {
		t.Errorf("IsUnbound(1) = true, want false")
	}
}

// TestFetchNumAssoc_noUID feeds the association helper from FETCH responses
// without a UID data item, served by a small in-memory fixture: every
// sequence number must be recorded with an explicit unbound state.
func TestFetchNumAssoc_noUID(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()

	client := imapclient.New(clientConn, nil)
	defer client.Close()

	go func() {
		br := bufio.NewReader(serverConn)
		io.WriteString(serverConn, "* OK [CAPABILITY IMAP4rev1] ready\r\n")
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			tag, _, _ := strings.Cut(line, " ")
			var resp string
			if strings.Contains(line, "FETCH") {
				resp = "* 1 FETCH (FLAGS (\\Seen))\r\n" +
					"* 2 FETCH (FLAGS ())\r\n" +
					"* 3 FETCH (FLAGS (\\Recent \\Seen))\r\n" +
					tag + " OK FETCH completed\r\n"
			} else {
				resp = tag + " OK done\r\n"
			}
			if _, err := io.WriteString(serverConn, resp); err != nil {
				return
			}
		}
	}()

	var assoc imapclient.FetchNumAssoc
	msgs, err := client.Fetch(imap.SeqSetNum(1, 2, 3), &imap.FetchOptions{Flags: true}).Collect()
	if err != nil {
		t.Fatalf("Fetch(FLAGS).Collect() = %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("len(msgs) = %v, want 3", len(msgs))
	}
	assoc.AddBuffers(msgs)

	// Check the regular Fetch read path: sequence numbers and flags
	for i, msg := range msgs {
		if want := uint32(i + 1); msg.SeqNum != want {
			t.Errorf("msg.SeqNum = %v, want %v", msg.SeqNum, want)
		}
		if msg.UID != 0 {
			t.Errorf("msg.UID = %v, want 0", msg.UID)
		}
	}
	if got, want := msgs[0].Flags, []imap.Flag{imap.FlagSeen}; !reflect.DeepEqual(got, want) {
		t.Errorf("msgs[0].Flags = %v, want %v", got, want)
	}

	// Check the association table: no bindings, all unbound
	if got, want := assoc.Len(), 0; got != want {
		t.Errorf("Len() = %v, want %v", got, want)
	}
	for _, seqNum := range []uint32{1, 2, 3} {
		if uid, ok := assoc.UID(seqNum); ok || uid != 0 {
			t.Errorf("UID(%v) = %v, %v, want 0, false", seqNum, uid, ok)
		}
		if !assoc.IsUnbound(seqNum) {
			t.Errorf("IsUnbound(%v) = false, want true", seqNum)
		}
	}
	if got, want := assoc.UnboundSeqNums(), []uint32{1, 2, 3}; !reflect.DeepEqual(got, want) {
		t.Errorf("UnboundSeqNums() = %v, want %v", got, want)
	}
}
