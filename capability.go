package imap

import (
	"strconv"
	"strings"
)

// Cap represents an IMAP capability.
type Cap string

// Registered capabilities.
//
// See: https://www.iana.org/assignments/imap-capabilities/
const (
	CapIMAP4rev1 Cap = "IMAP4rev1" // RFC 3501
	CapIMAP4rev2 Cap = "IMAP4rev2" // RFC 9051

	CapStartTLS      Cap = "STARTTLS"
	CapLoginDisabled Cap = "LOGINDISABLED"

	// Folded in IMAP4rev2
	CapNamespace    Cap = "NAMESPACE"     // RFC 2342
	CapUnselect     Cap = "UNSELECT"      // RFC 3691
	CapUIDPlus      Cap = "UIDPLUS"       // RFC 4315
	CapESearch      Cap = "ESEARCH"       // RFC 4731
	CapSearchRes    Cap = "SEARCHRES"     // RFC 5182
	CapEnable       Cap = "ENABLE"        // RFC 5161
	CapIdle         Cap = "IDLE"          // RFC 2177
	CapSASLIR       Cap = "SASL-IR"       // RFC 4959
	CapListExtended Cap = "LIST-EXTENDED" // RFC 5258
	CapListStatus   Cap = "LIST-STATUS"   // RFC 5819
	CapMove         Cap = "MOVE"          // RFC 6851
	CapLiteralMinus Cap = "LITERAL-"      // RFC 7888
	CapStatusSize   Cap = "STATUS=SIZE"   // RFC 8438
	CapChildren     Cap = "CHILDREN"      // RFC 3348

	CapACL              Cap = "ACL"                // RFC 4314
	CapAppendLimit      Cap = "APPENDLIMIT"        // RFC 7889
	CapBinary           Cap = "BINARY"             // RFC 3516
	CapCatenate         Cap = "CATENATE"           // RFC 4469
	CapCondStore        Cap = "CONDSTORE"          // RFC 7162
	CapConvert          Cap = "CONVERT"            // RFC 5259
	CapCreateSpecialUse Cap = "CREATE-SPECIAL-USE" // RFC 6154
	CapESort            Cap = "ESORT"              // RFC 5267
	CapFilters          Cap = "FILTERS"            // RFC 5466
	CapID               Cap = "ID"                 // RFC 2971
	CapLanguage         Cap = "LANGUAGE"           // RFC 5255
	CapListMyRights     Cap = "LIST-MYRIGHTS"      // RFC 8440
	CapLiteralPlus      Cap = "LITERAL+"           // RFC 7888
	CapLoginReferrals   Cap = "LOGIN-REFERRALS"    // RFC 2221
	CapMailboxReferrals Cap = "MAILBOX-REFERRALS"  // RFC 2193
	CapMetadata         Cap = "METADATA"           // RFC 5464
	CapMetadataServer   Cap = "METADATA-SERVER"    // RFC 5464
	CapMultiAppend      Cap = "MULTIAPPEND"        // RFC 3502
	CapMultiSearch      Cap = "MULTISEARCH"        // RFC 7377
	CapNotify           Cap = "NOTIFY"             // RFC 5465
	CapObjectID         Cap = "OBJECTID"           // RFC 8474
	CapPreview          Cap = "PREVIEW"            // RFC 8970
	CapQResync          Cap = "QRESYNC"            // RFC 7162
	CapQuota            Cap = "QUOTA"              // RFC 9208
	CapQuotaSet         Cap = "QUOTASET"           // RFC 9208
	CapReplace          Cap = "REPLACE"            // RFC 8508
	CapSaveDate         Cap = "SAVEDATE"           // RFC 8514
	CapSearchFuzzy      Cap = "SEARCH=FUZZY"       // RFC 6203
	CapSort             Cap = "SORT"               // RFC 5256
	CapSortDisplay      Cap = "SORT=DISPLAY"       // RFC 5957
	CapSpecialUse       Cap = "SPECIAL-USE"        // RFC 6154
	CapUnauthenticate   Cap = "UNAUTHENTICATE"     // RFC 8437
	CapURLPartial       Cap = "URL-PARTIAL"        // RFC 5550
	CapURLAuth          Cap = "URLAUTH"            // RFC 4467
	CapUTF8Accept       Cap = "UTF8=ACCEPT"        // RFC 6855
	CapUTF8Only         Cap = "UTF8=ONLY"          // RFC 6855
	CapWithin           Cap = "WITHIN"             // RFC 5032
	CapUIDOnly          Cap = "UIDONLY"            // RFC 9586
	CapListMetadata     Cap = "LIST-METADATA"      // RFC 9590
	CapInProgress       Cap = "INPROGRESS"         // RFC 9585
)

var imap4rev2Caps = CapSet{
	CapNamespace:    {},
	CapUnselect:     {},
	CapUIDPlus:      {},
	CapESearch:      {},
	CapSearchRes:    {},
	CapEnable:       {},
	CapIdle:         {},
	CapSASLIR:       {},
	CapListExtended: {},
	CapListStatus:   {},
	CapMove:         {},
	CapLiteralMinus: {},
	CapStatusSize:   {},
	CapChildren:     {},
}

// AuthCap returns the capability name for an SASL authentication mechanism.
func AuthCap(mechanism string) Cap {
	return Cap("AUTH=" + mechanism)
}

// CapSet is a set of capabilities.
//
// The set preserves the original spelling of capabilities as advertised by
// the server, while lookups (Has, Find) are case-insensitive as mandated by
// the IMAP specifications. Parameterized capabilities (e.g. "AUTH=PLAIN")
// are matched on both their name and parameter, case-insensitively.
type CapSet map[Cap]struct{}

func (set CapSet) has(c Cap) bool {
	_, ok := set[c]
	return ok
}

// splitCapParam splits a capability into its name and parameter, separated
// by the first "=". ok is false if the capability has no parameter.
func splitCapParam(c Cap) (name, param string, ok bool) {
	s := string(c)
	i := strings.IndexByte(s, '=')
	if i < 0 {
		return s, "", false
	}
	return s[:i], s[i+1:], true
}

// capNamesEqualFold reports whether two capabilities are equal,
// case-insensitively. For parameterized capabilities (e.g. "AUTH=PLAIN"),
// both the name and the parameter are compared case-insensitively.
func capNamesEqualFold(a, b Cap) bool {
	return strings.EqualFold(string(a), string(b))
}

// Add inserts a capability into the set.
//
// Capability names are case-insensitive: if a capability which only differs
// by case is already present in the set, the original spelling is kept and
// the duplicate is dropped.
func (set CapSet) Add(c Cap) {
	if _, ok := set.Find(c); ok {
		return
	}
	set[c] = struct{}{}
}

// Find returns the capability as stored in the set, preserving its original
// spelling. The lookup is case-insensitive.
func (set CapSet) Find(c Cap) (Cap, bool) {
	if set.has(c) {
		return c, true
	}
	for stored := range set {
		if capNamesEqualFold(stored, c) {
			return stored, true
		}
	}
	return "", false
}

func (set CapSet) Copy() CapSet {
	newSet := make(CapSet, len(set))
	for c := range set {
		newSet[c] = struct{}{}
	}
	return newSet
}

// Has checks whether a capability is supported.
//
// The lookup is case-insensitive: a capability advertised by the server with
// a different casing still matches.
//
// Some capabilities are implied by others, as such Has may return true even if
// the capability is not in the map.
func (set CapSet) Has(c Cap) bool {
	if _, ok := set.Find(c); ok {
		return true
	}

	if _, ok := set.Find(CapIMAP4rev2); ok && imap4rev2Caps.has(c) {
		return true
	}

	if c == CapLiteralMinus && set.Has(CapLiteralPlus) {
		return true
	}
	if c == CapCondStore && set.Has(CapQResync) {
		return true
	}
	if c == CapUTF8Accept && set.Has(CapUTF8Only) {
		return true
	}
	if c == CapAppendLimit {
		_, ok := set.AppendLimit()
		return ok
	}

	return false
}

// AuthMechanisms returns the list of supported SASL mechanisms for
// authentication.
//
// The "AUTH=" capability name is matched case-insensitively, while each
// returned mechanism keeps its original spelling.
func (set CapSet) AuthMechanisms() []string {
	var l []string
	for c := range set {
		name, param, ok := splitCapParam(c)
		if !ok || !strings.EqualFold(name, "AUTH") {
			continue
		}
		l = append(l, param)
	}
	return l
}

// AppendLimit checks the APPENDLIMIT capability.
//
// If the server supports APPENDLIMIT, ok is true. If the server doesn't have
// the same upload limit for all mailboxes, limit is nil and per-mailbox
// limits must be queried via STATUS.
func (set CapSet) AppendLimit() (limit *uint32, ok bool) {
	if _, found := set.Find(CapAppendLimit); found {
		return nil, true
	}

	for c := range set {
		name, param, isParam := splitCapParam(c)
		if !isParam || !strings.EqualFold(name, "APPENDLIMIT") {
			continue
		}

		limit64, err := strconv.ParseUint(param, 10, 32)
		if err == nil && limit64 > 0 {
			limit32 := uint32(limit64)
			return &limit32, true
		}
	}

	limit32 := ^uint32(0)
	return &limit32, false
}

// QuotaResourceTypes returns the list of supported QUOTA resource types.
func (set CapSet) QuotaResourceTypes() []QuotaResourceType {
	var l []QuotaResourceType
	for c := range set {
		name, param, ok := splitCapParam(c)
		if !ok || !strings.EqualFold(name, "QUOTA") {
			continue
		}
		if len(param) < 4 || !strings.EqualFold(param[:4], "RES-") {
			continue
		}
		l = append(l, QuotaResourceType(param[4:]))
	}
	return l
}

// ThreadAlgorithms returns the list of supported threading algorithms.
func (set CapSet) ThreadAlgorithms() []ThreadAlgorithm {
	var l []ThreadAlgorithm
	for c := range set {
		name, param, ok := splitCapParam(c)
		if !ok || !strings.EqualFold(name, "THREAD") {
			continue
		}
		l = append(l, ThreadAlgorithm(param))
	}
	return l
}
