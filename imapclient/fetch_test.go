package imapclient_test

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

func TestFetch(t *testing.T) {
	client, server := newClientServerPair(t, imap.ConnStateSelected)
	defer client.Close()
	defer server.Close()

	seqSet := imap.SeqSetNum(1)
	bodySection := &imap.FetchItemBodySection{}
	fetchOptions := &imap.FetchOptions{
		BodySection: []*imap.FetchItemBodySection{bodySection},
	}
	messages, err := client.Fetch(seqSet, fetchOptions).Collect()
	if err != nil {
		t.Fatalf("failed to fetch first message: %v", err)
	} else if len(messages) != 1 {
		t.Fatalf("len(messages) = %v, want 1", len(messages))
	}

	msg := messages[0]
	if len(msg.BodySection) != 1 {
		t.Fatalf("len(msg.BodySection) = %v, want 1", len(msg.BodySection))
	}
	b := msg.FindBodySection(bodySection)
	if b == nil {
		t.Fatalf("FindBodySection() = nil")
	}
	body := strings.ReplaceAll(string(b), "\r\n", "\n")
	if body != simpleRawMessage {
		t.Errorf("body mismatch: got \n%v\n but want \n%v", body, simpleRawMessage)
	}
}

func TestFetchSeqUIDTracker(t *testing.T) {
	client, server := newClientServerPair(t, imap.ConnStateSelected)
	defer client.Close()
	defer server.Close()

	// The fixture already contains one message, append two more
	for i := 0; i < 2; i++ {
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

	tracker := imapclient.NewSeqUIDTracker()

	// Fetch all messages with their UID and record the associations
	fetchOptions := &imap.FetchOptions{UID: true, Flags: true}
	messages, err := client.Fetch(imap.SeqSetNum(1, 2, 3), fetchOptions).Collect()
	if err != nil {
		t.Fatalf("Fetch().Collect() = %v", err)
	} else if len(messages) != 3 {
		t.Fatalf("len(messages) = %v, want 3", len(messages))
	}
	for _, msg := range messages {
		tracker.UpdateBuffer(msg)
	}

	// Check the association table: sequence numbers and UIDs are kept in
	// separate indexes
	if n := tracker.Len(); n != 3 {
		t.Errorf("tracker.Len() = %v, want 3", n)
	}
	for i, msg := range messages {
		seqNum := uint32(i + 1)
		if status := tracker.Status(seqNum); status != imapclient.SeqUIDBound {
			t.Errorf("tracker.Status(%v) = %v, want %v", seqNum, status, imapclient.SeqUIDBound)
		}
		if uid, ok := tracker.UID(seqNum); !ok || uid != msg.UID {
			t.Errorf("tracker.UID(%v) = %v, %v, want %v, true", seqNum, uid, ok, msg.UID)
		}
		if got, ok := tracker.SeqNum(msg.UID); !ok || got != seqNum {
			t.Errorf("tracker.SeqNum(%v) = %v, %v, want %v, true", msg.UID, got, ok, seqNum)
		}
	}

	// The original Fetch read path is unaffected
	bodySection := &imap.FetchItemBodySection{}
	bodyMsgs, err := client.Fetch(imap.SeqSetNum(1), &imap.FetchOptions{
		BodySection: []*imap.FetchItemBodySection{bodySection},
	}).Collect()
	if err != nil {
		t.Fatalf("Fetch().Collect() = %v", err)
	} else if len(bodyMsgs) != 1 {
		t.Fatalf("len(bodyMsgs) = %v, want 1", len(bodyMsgs))
	}
	b := bodyMsgs[0].FindBodySection(bodySection)
	if b == nil {
		t.Fatalf("FindBodySection() = nil")
	}
	body := strings.ReplaceAll(string(b), "\r\n", "\n")
	if body != simpleRawMessage {
		t.Errorf("body mismatch: got \n%v\n but want \n%v", body, simpleRawMessage)
	}

	// Expunge the first message: sequence numbers shift down by one
	storeFlags := imap.StoreFlags{
		Op:    imap.StoreFlagsAdd,
		Flags: []imap.Flag{imap.FlagDeleted},
	}
	if err := client.Store(imap.SeqSetNum(1), &storeFlags, nil).Close(); err != nil {
		t.Fatalf("Store().Close() = %v", err)
	}
	seqNums, err := client.Expunge().Collect()
	if err != nil {
		t.Fatalf("Expunge().Collect() = %v", err)
	} else if len(seqNums) != 1 || seqNums[0] != 1 {
		t.Fatalf("Expunge().Collect() = %v, want [1]", seqNums)
	}
	for _, seqNum := range seqNums {
		tracker.Expunge(seqNum)
	}

	uid2, uid3 := messages[1].UID, messages[2].UID
	if n := tracker.Len(); n != 2 {
		t.Errorf("tracker.Len() = %v, want 2", n)
	}
	if uid, ok := tracker.UID(1); !ok || uid != uid2 {
		t.Errorf("tracker.UID(1) = %v, %v, want %v, true", uid, ok, uid2)
	}
	if uid, ok := tracker.UID(2); !ok || uid != uid3 {
		t.Errorf("tracker.UID(2) = %v, %v, want %v, true", uid, ok, uid3)
	}
	if status := tracker.Status(3); status != imapclient.SeqUIDUnknown {
		t.Errorf("tracker.Status(3) = %v, want %v", status, imapclient.SeqUIDUnknown)
	}
	if seqNum, ok := tracker.SeqNum(uid3); !ok || seqNum != 2 {
		t.Errorf("tracker.SeqNum(%v) = %v, %v, want 2, true", uid3, seqNum, ok)
	}
}

func TestFetchSeqUIDTracker_noUID(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer serverConn.Close()

	client := imapclient.New(clientConn, nil)
	defer client.Close()

	// In-memory server fixture: replies to FETCH with a response which
	// doesn't contain a UID item
	go func() {
		if _, err := fmt.Fprintf(serverConn, "* OK [CAPABILITY IMAP4rev1] ready\r\n"); err != nil {
			return
		}
		line, err := bufio.NewReader(serverConn).ReadString('\n')
		if err != nil {
			return
		}
		tag := strings.Fields(line)[0]
		fmt.Fprintf(serverConn, "* 1 FETCH (FLAGS (\\Seen))\r\n")
		fmt.Fprintf(serverConn, "%v OK FETCH completed\r\n", tag)
	}()

	messages, err := client.Fetch(imap.SeqSetNum(1), &imap.FetchOptions{Flags: true}).Collect()
	if err != nil {
		t.Fatalf("Fetch().Collect() = %v", err)
	} else if len(messages) != 1 {
		t.Fatalf("len(messages) = %v, want 1", len(messages))
	}
	msg := messages[0]
	if msg.UID != 0 {
		t.Fatalf("msg.UID = %v, want 0", msg.UID)
	}

	// A FETCH response without a UID item is recorded as explicitly unbound
	tracker := imapclient.NewSeqUIDTracker()
	tracker.UpdateBuffer(msg)
	if status := tracker.Status(1); status != imapclient.SeqUIDUnbound {
		t.Errorf("tracker.Status(1) = %v, want %v", status, imapclient.SeqUIDUnbound)
	}
	if uid, ok := tracker.UID(1); ok {
		t.Errorf("tracker.UID(1) = %v, %v, want 0, false", uid, ok)
	}
	if n := tracker.Len(); n != 0 {
		t.Errorf("tracker.Len() = %v, want 0", n)
	}
	if status := tracker.Status(2); status != imapclient.SeqUIDUnknown {
		t.Errorf("tracker.Status(2) = %v, want %v", status, imapclient.SeqUIDUnknown)
	}
}
