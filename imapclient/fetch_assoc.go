package imapclient

import (
	"sort"

	"github.com/emersion/go-imap/v2"
)

// FetchNumAssoc associates message sequence numbers with UIDs for FETCH
// responses.
//
// IMAP servers identify messages in FETCH responses by their sequence
// number, and may include a UID data item. Sequence numbers and UIDs live in
// separate number spaces: FetchNumAssoc keeps them in two distinct tables so
// that a sequence number is never used as a UID key nor the other way
// around.
//
// FETCH responses without a UID data item are recorded with an explicit
// unbound state, see IsUnbound and UnboundSeqNums.
//
// Sequence numbers change when messages are expunged: call Expunge for each
// EXPUNGE response to keep the association tables in sync with the server's
// current response.
//
// The zero value is ready to use. FetchNumAssoc is not safe for concurrent
// use.
type FetchNumAssoc struct {
	bySeqNum map[uint32]imap.UID
	byUID    map[imap.UID]uint32
	unbound  map[uint32]bool
}

func (a *FetchNumAssoc) init() {
	if a.bySeqNum == nil {
		a.bySeqNum = make(map[uint32]imap.UID)
		a.byUID = make(map[imap.UID]uint32)
		a.unbound = make(map[uint32]bool)
	}
}

// Add records a FETCH response for a message sequence number.
//
// If uid is non-zero, the sequence number is bound to the UID. If uid is
// zero (the server didn't include a UID data item in its response), the
// sequence number is recorded as unbound, unless it is already bound to a
// UID.
func (a *FetchNumAssoc) Add(seqNum uint32, uid imap.UID) {
	if seqNum == 0 {
		return
	}
	a.init()
	if uid == 0 {
		if _, ok := a.bySeqNum[seqNum]; !ok {
			a.unbound[seqNum] = true
		}
		return
	}
	// Drop any previous binding for either number before rebinding
	a.unbindSeqNum(seqNum)
	a.unbindUID(uid)
	delete(a.unbound, seqNum)
	a.bySeqNum[seqNum] = uid
	a.byUID[uid] = seqNum
}

// AddBuffer records a message collected via FetchCommand.Collect.
func (a *FetchNumAssoc) AddBuffer(buf *FetchMessageBuffer) {
	a.Add(buf.SeqNum, buf.UID)
}

// AddBuffers records a list of messages collected via FetchCommand.Collect.
func (a *FetchNumAssoc) AddBuffers(bufs []*FetchMessageBuffer) {
	for _, buf := range bufs {
		a.AddBuffer(buf)
	}
}

// UID returns the UID bound to a message sequence number.
//
// ok is false if the sequence number is unknown or unbound. Use IsUnbound to
// distinguish between the two cases.
func (a *FetchNumAssoc) UID(seqNum uint32) (uid imap.UID, ok bool) {
	uid, ok = a.bySeqNum[seqNum]
	return uid, ok
}

// SeqNum returns the message sequence number bound to a UID.
//
// ok is false if the UID is unknown.
func (a *FetchNumAssoc) SeqNum(uid imap.UID) (seqNum uint32, ok bool) {
	seqNum, ok = a.byUID[uid]
	return seqNum, ok
}

// IsUnbound reports whether a FETCH response was seen for a message sequence
// number without a UID data item, and the sequence number hasn't been bound
// to a UID since then.
func (a *FetchNumAssoc) IsUnbound(seqNum uint32) bool {
	return a.unbound[seqNum]
}

// UnboundSeqNums returns the sequence numbers recorded as unbound, in
// ascending order.
func (a *FetchNumAssoc) UnboundSeqNums() []uint32 {
	var l []uint32
	for seqNum := range a.unbound {
		l = append(l, seqNum)
	}
	sort.Slice(l, func(i, j int) bool { return l[i] < l[j] })
	return l
}

// Len returns the number of bound sequence number/UID pairs.
func (a *FetchNumAssoc) Len() int {
	return len(a.bySeqNum)
}

// Expunge records an EXPUNGE response: the binding for the expunged sequence
// number is dropped and all greater sequence numbers are shifted down by
// one, matching the server's current response.
//
// It should be called once per EXPUNGE response, e.g. with each sequence
// number returned by ExpungeCommand or passed to
// UnilateralDataHandler.Expunge.
func (a *FetchNumAssoc) Expunge(seqNum uint32) {
	if seqNum == 0 {
		return
	}
	a.init()

	// Drop the expunged message
	a.unbindSeqNum(seqNum)
	delete(a.unbound, seqNum)

	// Collect the remaining sequence numbers above the expunged one, then
	// shift them down by one. Iterating in ascending order guarantees the
	// target slot has already been vacated.
	seqNums := make([]uint32, 0, len(a.bySeqNum)+len(a.unbound))
	for n := range a.bySeqNum {
		if n > seqNum {
			seqNums = append(seqNums, n)
		}
	}
	for n := range a.unbound {
		if n > seqNum {
			seqNums = append(seqNums, n)
		}
	}
	sort.Slice(seqNums, func(i, j int) bool { return seqNums[i] < seqNums[j] })
	for _, n := range seqNums {
		if uid, ok := a.bySeqNum[n]; ok {
			delete(a.bySeqNum, n)
			a.bySeqNum[n-1] = uid
			a.byUID[uid] = n - 1
		} else {
			delete(a.unbound, n)
			a.unbound[n-1] = true
		}
	}
}

// unbindSeqNum drops any binding for a sequence number, in both directions.
func (a *FetchNumAssoc) unbindSeqNum(seqNum uint32) {
	if uid, ok := a.bySeqNum[seqNum]; ok {
		delete(a.bySeqNum, seqNum)
		delete(a.byUID, uid)
	}
}

// unbindUID drops any binding for a UID, in both directions.
func (a *FetchNumAssoc) unbindUID(uid imap.UID) {
	if seqNum, ok := a.byUID[uid]; ok {
		delete(a.byUID, uid)
		delete(a.bySeqNum, seqNum)
	}
}
