package imapwire_test

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"testing"

	"github.com/emersion/go-imap/v2/internal/imapwire"
)

// TestDecoderLiteralMaxTwoSegments feeds the stream in two segments (the
// declared-length header first, then the literal body) and checks that the
// following response tag is still parsed correctly: the next response must
// not be consumed as part of the literal.
func TestDecoderLiteralMaxTwoSegments(t *testing.T) {
	pr, pw := io.Pipe()
	go func() {
		segments := []string{
			"{5}\r\n",
			"hello",
			"TAG2 OK done\r\n",
		}
		for _, s := range segments {
			if _, err := io.WriteString(pw, s); err != nil {
				pw.CloseWithError(err)
				return
			}
		}
		pw.Close()
	}()

	dec := imapwire.NewDecoder(bufio.NewReader(pr), imapwire.ConnSideClient)

	var lit string
	if !dec.LiteralMax(&lit, 5) {
		t.Fatalf("LiteralMax failed: %v", dec.Err())
	}
	if lit != "hello" {
		t.Fatalf("literal = %q, want %q", lit, "hello")
	}

	var tag, status, text string
	if !dec.ExpectAtom(&tag) || !dec.ExpectSP() || !dec.ExpectAtom(&status) || !dec.ExpectSP() || !dec.ExpectAtom(&text) || !dec.ExpectCRLF() {
		t.Fatalf("failed to decode next response: %v", dec.Err())
	}
	if tag != "TAG2" || status != "OK" || text != "done" {
		t.Fatalf("next response = %q %q %q, want TAG2 OK done", tag, status, text)
	}
}

// TestDecoderLiteralMaxTooLarge verifies that a declared size above the
// limit is rejected before any literal bytes are read or buffered.
func TestDecoderLiteralMaxTooLarge(t *testing.T) {
	dec := newTestDecoder("{100}\r\n" + strings.Repeat("x", 100))

	var lit string
	if dec.LiteralMax(&lit, 4) {
		t.Fatalf("LiteralMax accepted a literal larger than the limit: %q", lit)
	}
	if lit != "" {
		t.Fatalf("LiteralMax populated the literal on rejection: %q", lit)
	}

	var tooLong *imapwire.LiteralTooLongError
	if !errors.As(dec.Err(), &tooLong) {
		t.Fatalf("error is %T (%v), want *LiteralTooLongError", dec.Err(), dec.Err())
	}
	if tooLong.Size != 100 || tooLong.Max != 4 {
		t.Fatalf("error fields = {Size:%v Max:%v}, want {Size:100 Max:4}", tooLong.Size, tooLong.Max)
	}
}

// TestDecoderLiteralMaxUnexpectedEOF verifies that an EOF in the middle of
// a declared literal is reported explicitly, including the number of
// missing bytes. The stream is closed right after the partial body, so no
// later response data can be mistaken for literal bytes.
func TestDecoderLiteralMaxUnexpectedEOF(t *testing.T) {
	pr, pw := io.Pipe()
	go func() {
		if _, err := io.WriteString(pw, "{5}\r\n"); err != nil {
			pw.CloseWithError(err)
			return
		}
		if _, err := io.WriteString(pw, "abc"); err != nil {
			pw.CloseWithError(err)
			return
		}
		pw.Close()
	}()

	dec := imapwire.NewDecoder(bufio.NewReader(pr), imapwire.ConnSideClient)

	var lit string
	if dec.LiteralMax(&lit, 10) {
		t.Fatalf("LiteralMax accepted a truncated literal: %q", lit)
	}

	var eofErr *imapwire.LiteralUnexpectedEOFError
	if !errors.As(dec.Err(), &eofErr) {
		t.Fatalf("error is %T (%v), want *LiteralUnexpectedEOFError", dec.Err(), dec.Err())
	}
	if eofErr.Size != 5 || eofErr.N != 3 || eofErr.Remaining != 2 {
		t.Fatalf("error fields = {Size:%v N:%v Remaining:%v}, want {Size:5 N:3 Remaining:2}", eofErr.Size, eofErr.N, eofErr.Remaining)
	}
	if !errors.Is(dec.Err(), io.ErrUnexpectedEOF) {
		t.Fatalf("errors.Is(err, io.ErrUnexpectedEOF) = false, want true")
	}
}

// ExampleDecoder_LiteralMax reads a length-capped literal from an in-memory
// two-segment stream and then decodes the following response tag.
func ExampleDecoder_LiteralMax() {
	pr, pw := io.Pipe()
	go func() {
		segments := []string{
			"{11}\r\n",
			"hello world",
			"TAG OK\r\n",
		}
		for _, s := range segments {
			if _, err := io.WriteString(pw, s); err != nil {
				pw.CloseWithError(err)
				return
			}
		}
		pw.Close()
	}()

	dec := imapwire.NewDecoder(bufio.NewReader(pr), imapwire.ConnSideClient)

	var lit string
	if !dec.LiteralMax(&lit, 64) {
		log.Fatal(dec.Err())
	}

	var tag, status string
	if !dec.ExpectAtom(&tag) || !dec.ExpectSP() || !dec.ExpectAtom(&status) || !dec.ExpectCRLF() {
		log.Fatal(dec.Err())
	}

	fmt.Printf("literal=%q next-tag=%s status=%s\n", lit, tag, status)
	// Output: literal="hello world" next-tag=TAG status=OK
}
