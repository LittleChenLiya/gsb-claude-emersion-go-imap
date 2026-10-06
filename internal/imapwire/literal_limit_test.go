package imapwire_test

import (
	"bufio"
	"io"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2/internal/imapwire"
)

// chunkReader returns at most chunkSize bytes per Read, simulating a
// fragmented network stream.
type chunkReader struct {
	r         io.Reader
	chunkSize int
}

func (cr *chunkReader) Read(b []byte) (int, error) {
	if len(b) > cr.chunkSize {
		b = b[:cr.chunkSize]
	}
	return cr.r.Read(b)
}

func newChunkDecoder(s string, chunkSize int) *imapwire.Decoder {
	return imapwire.NewDecoder(
		bufio.NewReader(&chunkReader{r: strings.NewReader(s), chunkSize: chunkSize}),
		imapwire.ConnSideServer,
	)
}

func TestLiteralLimitedTwoSegments(t *testing.T) {
	// The literal header and body arrive in separate segments, and the
	// body is immediately followed by the next response.
	const payload = "0123456789"
	input := "{10}\r\n" + payload + "\r\nT2 OK done\r\n"

	dec := newChunkDecoder(input, 1)

	var body string
	if !dec.LiteralLimited(&body, 10) {
		t.Fatalf("LiteralLimited failed: %v", dec.Err())
	}
	if body != payload {
		t.Fatalf("literal = %q, want %q", body, payload)
	}

	// The literal must be consumed exactly: the following response is
	// still decoded correctly.
	if !dec.ExpectCRLF() {
		t.Fatalf("CRLF after literal: %v", dec.Err())
	}
	var tag, typ string
	if !dec.ExpectAtom(&tag) {
		t.Fatalf("tag: %v", dec.Err())
	}
	if tag != "T2" {
		t.Fatalf("tag = %q, want T2", tag)
	}
	if !dec.ExpectSP() || !dec.ExpectAtom(&typ) {
		t.Fatalf("type: %v", dec.Err())
	}
	if typ != "OK" {
		t.Fatalf("type = %q, want OK", typ)
	}
}

func TestLiteralLimitedHeaderBodySplit(t *testing.T) {
	// First segment: the literal header. Second segment: the body plus the
	// next response.
	header := "{10}\r\n"
	rest := "0123456789\r\nT2 OK\r\n"
	r := io.MultiReader(strings.NewReader(header), strings.NewReader(rest))
	dec := imapwire.NewDecoder(bufio.NewReader(r), imapwire.ConnSideServer)

	var body string
	if !dec.LiteralLimited(&body, 10) {
		t.Fatalf("LiteralLimited failed: %v", dec.Err())
	}
	if body != "0123456789" {
		t.Fatalf("literal = %q", body)
	}
	if !dec.ExpectCRLF() {
		t.Fatalf("CRLF after literal: %v", dec.Err())
	}
	var tag string
	if !dec.ExpectAtom(&tag) || tag != "T2" {
		t.Fatalf("tag = %q, err = %v", tag, dec.Err())
	}
}

func TestLiteralLimitedExactSize(t *testing.T) {
	input := "{10}\r\n" + strings.Repeat("x", 10)
	dec := newTestDecoder(input)

	var body string
	if !dec.LiteralLimited(&body, 10) {
		t.Fatalf("LiteralLimited failed: %v", dec.Err())
	}
	if body != strings.Repeat("x", 10) {
		t.Fatalf("literal = %q", body)
	}
}

func TestLiteralLimitedOverSize(t *testing.T) {
	// The declared size exceeds the cap: reject before allocating.
	input := "{100}\r\n" + strings.Repeat("x", 100)
	dec := newTestDecoder(input)

	var body string
	if dec.LiteralLimited(&body, 10) {
		t.Fatal("LiteralLimited accepted an over-size literal")
	}
	err := dec.Err()
	if err == nil {
		t.Fatal("decoder has no error")
	}
	msg := err.Error()
	for _, want := range []string{"exceeds", "100", "10"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not contain %q", msg, want)
		}
	}
	if body != "" {
		t.Errorf("output string was modified: %q", body)
	}
}

func TestLiteralLimitedPrematureEOF(t *testing.T) {
	// Declared 10 bytes, but the stream ends after 5.
	input := "{10}\r\n01234"
	dec := newTestDecoder(input)

	var body string
	if dec.LiteralLimited(&body, 10) {
		t.Fatal("LiteralLimited accepted a truncated literal")
	}
	err := dec.Err()
	if err == nil {
		t.Fatal("decoder has no error")
	}
	msg := err.Error()
	for _, want := range []string{"EOF", "5", "remaining"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not contain %q", msg, want)
		}
	}
	if body != "" {
		t.Errorf("output string was modified: %q", body)
	}
}
