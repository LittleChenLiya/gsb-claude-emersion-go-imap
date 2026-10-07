package imap_test

import (
	"reflect"
	"testing"

	"github.com/emersion/go-imap/v2"
)

// TestCapSet_AddDedupPreservesSpelling checks that capabilities which only
// differ by case are deduplicated without losing the original spelling.
func TestCapSet_AddDedupPreservesSpelling(t *testing.T) {
	set := make(imap.CapSet)
	set.Add(imap.Cap("imap4rev1"))
	set.Add(imap.Cap("IMAP4REV1"))
	set.Add(imap.Cap("Auth=Plain"))
	set.Add(imap.Cap("AUTH=PLAIN"))
	set.Add(imap.Cap("move"))

	if want := 3; len(set) != want {
		t.Fatalf("len(set) = %v, want %v", len(set), want)
	}

	for _, tc := range []struct {
		query imap.Cap
		want  imap.Cap
	}{
		{imap.CapIMAP4rev1, "imap4rev1"},
		{imap.Cap("IMAP4rev1"), "imap4rev1"},
		{imap.AuthCap("PLAIN"), "Auth=Plain"},
		{imap.AuthCap("plain"), "Auth=Plain"},
		{imap.CapMove, "move"},
	} {
		got, ok := set.Find(tc.query)
		if !ok {
			t.Errorf("Find(%q) not found", tc.query)
		} else if got != tc.want {
			t.Errorf("Find(%q) = %q, want %q", tc.query, got, tc.want)
		}
	}

	if _, ok := set.Find(imap.Cap("CONDSTORE")); ok {
		t.Errorf("Find(CONDSTORE) should not be found")
	}
}

// TestCapSet_HasCaseInsensitive checks that Has matches capabilities
// regardless of the casing used by the server, including implied
// capabilities.
func TestCapSet_HasCaseInsensitive(t *testing.T) {
	set := imap.CapSet{
		imap.Cap("imap4rev2"):      {},
		imap.Cap("literal+"):       {},
		imap.Cap("qresync"):        {},
		imap.Cap("utf8=only"):      {},
		imap.Cap("appendlimit=42"): {},
	}

	for _, c := range []imap.Cap{
		imap.CapIMAP4rev2,
		imap.CapMove,      // implied by IMAP4rev2
		imap.CapNamespace, // implied by IMAP4rev2
		imap.CapLiteralPlus,
		imap.CapLiteralMinus, // implied by LITERAL+
		imap.CapCondStore,    // implied by QRESYNC
		imap.CapUTF8Accept,   // implied by UTF8=ONLY
		imap.CapAppendLimit,
	} {
		if !set.Has(c) {
			t.Errorf("Has(%q) = false, want true", c)
		}
	}

	if set.Has(imap.CapIMAP4rev1) {
		t.Errorf("Has(IMAP4rev1) = true, want false")
	}
}

// TestCapSet_ParameterizedCaps checks that parameterized capabilities are
// still split into name and parameter, case-insensitively, while preserving
// the original parameter spelling.
func TestCapSet_ParameterizedCaps(t *testing.T) {
	set := imap.CapSet{
		imap.Cap("auth=SCRAM-SHA-256"): {},
		imap.Cap("APPENDLIMIT=1000"):   {},
		imap.Cap("quota=res-STORAGE"):  {},
		imap.Cap("Thread=REFS"):        {},
	}

	if mechs := set.AuthMechanisms(); !reflect.DeepEqual(mechs, []string{"SCRAM-SHA-256"}) {
		t.Errorf("AuthMechanisms() = %v, want [SCRAM-SHA-256]", mechs)
	}

	limit, ok := set.AppendLimit()
	if !ok {
		t.Fatalf("AppendLimit() ok = false, want true")
	}
	if limit == nil || *limit != 1000 {
		t.Errorf("AppendLimit() = %v, want 1000", limit)
	}

	if res := set.QuotaResourceTypes(); !reflect.DeepEqual(res, []imap.QuotaResourceType{imap.QuotaResourceStorage}) {
		t.Errorf("QuotaResourceTypes() = %v, want [STORAGE]", res)
	}

	if algs := set.ThreadAlgorithms(); !reflect.DeepEqual(algs, []imap.ThreadAlgorithm{"REFS"}) {
		t.Errorf("ThreadAlgorithms() = %v, want [REFS]", algs)
	}
}
