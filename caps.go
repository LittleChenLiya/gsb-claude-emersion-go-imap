package imap

import "strings"

// Caps is a case-insensitive view over a set of capabilities.
//
// The original spelling of each capability, as sent by the server, is
// preserved. Capabilities which only differ by case are deduplicated: the
// spelling of the first occurrence is kept. For parameterized capabilities
// such as "AUTH=PLAIN", the parameter is split from the capability name and
// can be queried separately via Params.
//
// A nil Caps is a valid empty view.
type Caps struct {
	list   []Cap            // deduplicated capabilities, original spelling
	set    CapSet           // canonical capability names
	orig   map[Cap]Cap      // canonical capability name -> original spelling
	params map[Cap][]string // canonical capability name -> original parameter values
}

// NewCaps creates a new capability view containing the provided
// capabilities.
func NewCaps(caps ...Cap) *Caps {
	c := &Caps{
		set:    make(CapSet),
		orig:   make(map[Cap]Cap),
		params: make(map[Cap][]string),
	}
	for _, cap := range caps {
		c.Add(cap)
	}
	return c
}

// Add inserts a capability in the view.
//
// If a capability which only differs by case is already present, the
// capability is dropped and the original spelling of the first occurrence is
// kept.
func (caps *Caps) Add(c Cap) {
	canon := canonicalCap(c)
	if _, ok := caps.set[canon]; ok {
		return
	}
	caps.set[canon] = struct{}{}
	caps.list = append(caps.list, c)
	caps.orig[canon] = c

	name, param := splitCapParam(c)
	if param == "" {
		return
	}
	canonName := canonicalCap(Cap(name))
	caps.params[canonName] = append(caps.params[canonName], param)
}

// Has checks whether a capability is supported.
//
// The lookup is case-insensitive. Some capabilities are implied by others,
// as such Has may return true even if the capability was not explicitly
// listed (see CapSet.Has).
func (caps *Caps) Has(c Cap) bool {
	if caps == nil {
		return false
	}
	return caps.set.Has(canonicalCap(c))
}

// Get returns the original spelling of a capability, as sent by the server.
//
// The lookup is case-insensitive. If the capability was not explicitly
// listed, ok is false.
func (caps *Caps) Get(c Cap) (orig Cap, ok bool) {
	if caps == nil {
		return "", false
	}
	orig, ok = caps.orig[canonicalCap(c)]
	return orig, ok
}

// Params returns the original parameter values of a parameterized
// capability.
//
// For instance, if the server sent "AUTH=PLAIN auth=login", Params("AUTH")
// returns []string{"PLAIN", "login"}. The capability name lookup is
// case-insensitive. If the capability has no parameter, nil is returned.
func (caps *Caps) Params(name Cap) []string {
	if caps == nil {
		return nil
	}
	return caps.params[canonicalCap(name)]
}

// List returns the capabilities in their original spelling, in the order in
// which they were added. Capabilities which only differ by case are
// deduplicated.
func (caps *Caps) List() []Cap {
	if caps == nil {
		return nil
	}
	return append([]Cap(nil), caps.list...)
}

// CapSet returns the capabilities as a CapSet with canonical capability
// names.
func (caps *Caps) CapSet() CapSet {
	if caps == nil {
		return nil
	}
	return caps.set.Copy()
}

// canonicalCap returns the canonical spelling of a capability.
func canonicalCap(c Cap) Cap {
	// Only two caps are not fully uppercase
	for _, canon := range []Cap{CapIMAP4rev1, CapIMAP4rev2} {
		if strings.EqualFold(string(c), string(canon)) {
			return canon
		}
	}
	return Cap(strings.ToUpper(string(c)))
}

// splitCapParam splits a parameterized capability (e.g. "AUTH=PLAIN") into
// its name and parameter. If the capability has no parameter, param is
// empty.
func splitCapParam(c Cap) (name, param string) {
	s := string(c)
	if i := strings.IndexByte(s, '='); i >= 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}
