package imapwire_test

import (
	"bufio"
	"fmt"
	"strings"

	"github.com/emersion/go-imap/v2/internal/imapwire"
)

func ExampleDecoder_LiteralLimited() {
	// An in-memory stream carrying a literal payload followed by the next
	// tagged response.
	stream := strings.NewReader("{11}\r\nHello world\r\nT2 OK done\r\n")
	dec := imapwire.NewDecoder(bufio.NewReader(stream), imapwire.ConnSideServer)

	var body string
	if !dec.LiteralLimited(&body, 1024) {
		fmt.Printf("failed to read literal: %v\n", dec.Err())
		return
	}
	fmt.Printf("body: %q\n", body)

	// The literal is consumed exactly: the following tagged response is
	// decoded correctly.
	dec.ExpectCRLF()
	var tag, typ string
	dec.ExpectAtom(&tag)
	dec.ExpectSP()
	dec.ExpectAtom(&typ)
	fmt.Printf("next response: %s %s\n", tag, typ)

	// Output:
	// body: "Hello world"
	// next response: T2 OK
}
