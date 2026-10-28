package normalize

// callNamesMax bounds a callNames table. A result whose call is older
// than the last callNamesMax distinct calls keeps a null name.
// callNameMaxBytes bounds a call id and a name: a longer one is not
// kept, so its result keeps a null name. The table holds at most about
// 2 MiB on a hostile transcript, and far less on a real one.
const (
	callNamesMax     = 4096
	callNameMaxBytes = 256
)

// callNames maps a call id to the tool name its call carried, so that a
// result that names only its call id can carry the name too. Calls come
// before their results, so the recent calls cover the results a
// transcript writes. When full it forgets the oldest call first. The
// zero value is empty and ready. It is parser state for one projection
// pass: a checkpoint that resumes a pass would have to carry it.
type callNames struct {
	names map[string]string
	order []string
	next  int
}

func (c *callNames) remember(callID, name string) {
	if callID == "" || name == "" || len(callID) > callNameMaxBytes || len(name) > callNameMaxBytes {
		return
	}
	if c.names == nil {
		c.names = map[string]string{}
	}
	if _, ok := c.names[callID]; ok {
		c.names[callID] = name
		return
	}
	if len(c.order) < callNamesMax {
		c.order = append(c.order, callID)
	} else {
		delete(c.names, c.order[c.next])
		c.order[c.next] = callID
		c.next = (c.next + 1) % callNamesMax
	}
	c.names[callID] = name
}

// name is the tool name of the call with this id, or "" when the call
// was not seen or has been forgotten.
func (c *callNames) name(callID string) string {
	return c.names[callID]
}
