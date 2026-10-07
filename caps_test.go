package imap_test

import (
	"reflect"
	"sort"
	"testing"

	"github.com/emersion/go-imap/v2"
)

func TestCaps(t *testing.T) {
	caps := imap.NewCaps(
		"IMAP4rev2",
		"AUTH=PLAIN",
		"auth=login",
		"AUTH=PLAIN", // exact duplicate
		"X-Case",
		"x-case", // duplicate with a different case
	)

	// Case-insensitive lookups
	for _, c := range []imap.Cap{"imap4rev2", "IMAP4REV2", "AUTH=PLAIN", "auth=plain", "X-CASE", "x-Case"} {
		if !caps.Has(c) {
			t.Errorf("Has(%q) = false, want true", c)
		}
	}
	for _, c := range []imap.Cap{"AUTH=SCRAM-SHA-256", "X-OTHER"} {
		if caps.Has(c) {
			t.Errorf("Has(%q) = true, want false", c)
		}
	}

	// Capabilities implied by IMAP4rev2 still work
	if !caps.Has(imap.CapEnable) {
		t.Errorf("Has(%q) = false, want true (implied by IMAP4rev2)", imap.CapEnable)
	}

	// Original spelling is preserved
	if orig, ok := caps.Get("auth=plain"); !ok || orig != "AUTH=PLAIN" {
		t.Errorf("Get(%q) = %q, %v, want %q, true", "auth=plain", orig, ok, "AUTH=PLAIN")
	}
	if orig, ok := caps.Get("AUTH=LOGIN"); !ok || orig != "auth=login" {
		t.Errorf("Get(%q) = %q, %v, want %q, true", "AUTH=LOGIN", orig, ok, "auth=login")
	}
	if orig, ok := caps.Get("x-CASE"); !ok || orig != "X-Case" {
		t.Errorf("Get(%q) = %q, %v, want %q, true", "x-CASE", orig, ok, "X-Case")
	}
	if _, ok := caps.Get("AUTH=SCRAM-SHA-256"); ok {
		t.Errorf("Get(%q) ok = true, want false", "AUTH=SCRAM-SHA-256")
	}

	// Parameters are split out, keeping their original spelling
	if params := caps.Params("auth"); !reflect.DeepEqual(params, []string{"PLAIN", "login"}) {
		t.Errorf("Params(%q) = %v, want %v", "auth", params, []string{"PLAIN", "login"})
	}
	if params := caps.Params("X-CASE"); params != nil {
		t.Errorf("Params(%q) = %v, want nil", "X-CASE", params)
	}

	// Duplicates with a different case are deduplicated without losing the
	// original spelling of the first occurrence
	wantList := []imap.Cap{"IMAP4rev2", "AUTH=PLAIN", "auth=login", "X-Case"}
	if list := caps.List(); !reflect.DeepEqual(list, wantList) {
		t.Errorf("List() = %v, want %v", list, wantList)
	}

	// Old capability view with canonical names
	set := caps.CapSet()
	for _, c := range []imap.Cap{imap.CapIMAP4rev2, imap.AuthCap("PLAIN"), imap.AuthCap("LOGIN"), "X-CASE"} {
		if !set.Has(c) {
			t.Errorf("CapSet().Has(%q) = false, want true", c)
		}
	}
	if !set.Has(imap.CapEnable) {
		t.Errorf("CapSet().Has(%q) = false, want true (implied by IMAP4rev2)", imap.CapEnable)
	}
	mechs := set.AuthMechanisms()
	sort.Strings(mechs)
	if !reflect.DeepEqual(mechs, []string{"LOGIN", "PLAIN"}) {
		t.Errorf("CapSet().AuthMechanisms() = %v, want %v", mechs, []string{"LOGIN", "PLAIN"})
	}
}

func TestCaps_nil(t *testing.T) {
	var caps *imap.Caps
	if caps.Has(imap.CapIMAP4rev2) {
		t.Errorf("nil.Has(%q) = true, want false", imap.CapIMAP4rev2)
	}
	if _, ok := caps.Get("AUTH=PLAIN"); ok {
		t.Errorf("nil.Get(%q) ok = true, want false", "AUTH=PLAIN")
	}
	if params := caps.Params("AUTH"); params != nil {
		t.Errorf("nil.Params(%q) = %v, want nil", "AUTH", params)
	}
	if list := caps.List(); list != nil {
		t.Errorf("nil.List() = %v, want nil", list)
	}
	if set := caps.CapSet(); set != nil {
		t.Errorf("nil.CapSet() = %v, want nil", set)
	}
}
