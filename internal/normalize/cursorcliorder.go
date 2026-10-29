package normalize

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
)

// cursorCLIOrder is the order the blobs of a Cursor CLI export are
// projected in, as indexes into doc.Blobs.
//
// The export lists blobs by id, and an id is a content hash, so export
// order is not conversation order. Cursor writes a root blob, a protobuf
// message, each time the conversation changes. Field 1 of a root repeats
// once per message in the context at that time, in conversation order,
// each a 32-byte blob id. The session record's latestRootBlobId names
// the newest root. Earlier roots hold the messages that a compaction
// later dropped.
//
// Each root's list says that one message follows another. The order is
// a topological order of those edges, so a tool call comes before its
// result whenever a root lists both. Where the lists branch, as after a
// compaction, the branch the latest root follows goes last. A cycle,
// which a list that repeats an id can make, is broken where the walk
// meets it. Blobs that no root lists, roots included, follow in export
// order. With no root, the order is the export order.
//
// Nothing in a store orders two branches that the latest root left
// behind: a root carries no time of its own and no link to the root
// before it, so after two compactions the older branches share only
// the messages both kept. Each such branch stays whole and in its own
// order, and between them the walk falls back to export order, which is
// hash order. unsure marks the blobs whose place is that guess: those
// outside the latest root, when two or more stretches of them meet with
// no edge between. With one stretch, it precedes the latest root's
// branch and its place is known.
//
// Reader version 2 exports each blob's rowid as seq, and Cursor inserts
// each root after the one before it. When every root carries a seq, a
// blob outside the latest root is placed by the seq of the first root
// that lists it, so the older branches follow in the order their roots
// were written, and unsure is nil. A version 1 export has no seq and
// keeps the guess above.
//
// Root bytes are decoded only to read the ids. They are not copied into
// any event.
func cursorCLIOrder(doc cursorCLIExport) (order []int, unsure []bool) {
	n := len(doc.Blobs)
	// index holds the blobs whose id is 64 hex digits, the only ids a
	// root can name, keyed by the 32 bytes a root holds.
	index := make(map[[32]byte]int32, n)
	for i, b := range doc.Blobs {
		if key, ok := cursorCLIIDKey(b.ID); ok {
			index[key] = int32(i)
		}
	}
	var ids []int32
	latest := map[int32]int{}
	if key, ok := cursorCLIIDKey(cursorCLILatestRoot(doc.Meta)); ok {
		if root, ok := index[key]; ok {
			var found bool
			if ids, found = cursorCLIRootIDs(doc.Blobs[root].Data, index, ids); found {
				for pos, id := range ids {
					latest[id] = pos
				}
			}
		}
	}

	adj := map[int32][]int32{}
	seen := map[uint64]struct{}{}
	listed := make([]bool, n)
	roots := 0
	// born is, for each listed blob, the least seq of a root that lists
	// it. bySeq holds while every root has a seq.
	born := make([]int64, n)
	bySeq := true
	for _, b := range doc.Blobs {
		var ok bool
		ids, ok = cursorCLIRootIDs(b.Data, index, ids)
		if !ok {
			continue
		}
		roots++
		seq, hasSeq := cursorCLISeq(b.Seq)
		bySeq = bySeq && hasSeq
		for k, id := range ids {
			if hasSeq && (!listed[id] || seq < born[id]) {
				born[id] = seq
			}
			listed[id] = true
			if k == 0 || ids[k-1] == id {
				continue
			}
			from := ids[k-1]
			key := uint64(uint32(from))<<32 | uint64(uint32(id))
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			adj[from] = append(adj[from], id)
		}
	}
	if roots == 0 {
		order = make([]int, n)
		for i := range order {
			order[i] = i
		}
		return order, nil
	}

	// newer sorts the blob that belongs later first: a member of the
	// latest root by its position there, then any other blob by the seq
	// of the first root that lists it when every root has one, then by
	// export index. A depth-first walk that visits the newer successor
	// first finishes it first, so it lands later in the reversed finish
	// order.
	newer := func(a, b int32) bool {
		pa, ina := latest[a]
		pb, inb := latest[b]
		if ina != inb {
			return ina
		}
		if ina {
			return pa > pb
		}
		if bySeq && born[a] != born[b] {
			return born[a] > born[b]
		}
		return a > b
	}
	for from, next := range adj {
		sort.Slice(next, func(i, j int) bool { return newer(next[i], next[j]) })
		adj[from] = next
	}
	var starts []int32
	for i := range listed {
		if listed[i] {
			starts = append(starts, int32(i))
		}
	}
	sort.Slice(starts, func(i, j int) bool { return newer(starts[i], starts[j]) })

	// state is 0 unvisited, 1 on the walk's path, 2 finished.
	state := make([]uint8, n)
	finished := make([]int32, 0, len(starts))
	type frame struct {
		node int32
		next int
	}
	var stack []frame
	for _, s := range starts {
		if state[s] != 0 {
			continue
		}
		state[s] = 1
		stack = append(stack[:0], frame{node: s})
		for len(stack) > 0 {
			top := &stack[len(stack)-1]
			succ := adj[top.node]
			if top.next < len(succ) {
				m := succ[top.next]
				top.next++
				if state[m] == 0 {
					state[m] = 1
					stack = append(stack, frame{node: m})
				}
				continue
			}
			state[top.node] = 2
			finished = append(finished, top.node)
			stack = stack[:len(stack)-1]
		}
	}

	order = make([]int, 0, n)
	for i := len(finished) - 1; i >= 0; i-- {
		order = append(order, int(finished[i]))
	}
	if !bySeq {
		unsure = cursorCLIUnsure(order, adj, latest, n)
	}
	for i := range doc.Blobs {
		if !listed[i] {
			order = append(order, i)
		}
	}
	return order, unsure
}

// cursorCLIUnsure marks the listed blobs whose place between older
// branches no root establishes, or returns nil when there are none.
// order is the walk's order of the listed blobs. A stretch starts where
// two neighbours have no edge between them; a stretch counts when it
// holds a blob outside the latest root.
func cursorCLIUnsure(order []int, adj map[int32][]int32, latest map[int32]int, n int) []bool {
	edge := func(from, to int) bool {
		for _, m := range adj[int32(from)] {
			if int(m) == to {
				return true
			}
		}
		return false
	}
	stretch := make([]int, len(order))
	for k := 1; k < len(order); k++ {
		stretch[k] = stretch[k-1]
		if !edge(order[k-1], order[k]) {
			stretch[k]++
		}
	}
	older := map[int]bool{}
	for k, i := range order {
		if _, in := latest[int32(i)]; !in {
			older[stretch[k]] = true
		}
	}
	if len(older) < 2 {
		return nil
	}
	unsure := make([]bool, n)
	for k, i := range order {
		if _, in := latest[int32(i)]; !in && older[stretch[k]] {
			unsure[i] = true
		}
	}
	return unsure
}

// cursorCLILatestRoot is latestRootBlobId on the session record, meta
// key "0", or "" when it is absent.
func cursorCLILatestRoot(meta []cursorCLIMeta) string {
	for _, row := range meta {
		if row.Key != "0" {
			continue
		}
		obj, ok := jsonObject(row.Value)
		if !ok {
			return ""
		}
		s, _ := jsonString(obj["latestRootBlobId"])
		return s
	}
	return ""
}

var base64WrapperPrefix = []byte(`{"base64"`)

// cursorCLIRootIDs reads a root blob: a base64 wrapper whose bytes are a
// protobuf message with at least one field 1, where every field 1 is a
// 32-byte id of a blob in the export. Any other blob is not a root. The
// wrapper is read in one decode, which also accepts fields beside
// base64; the adapter writes none, and the ids must still all resolve.
// The ids are appended to buf[:0], which is returned for reuse either
// way.
func cursorCLIRootIDs(data json.RawMessage, index map[[32]byte]int32, buf []int32) ([]int32, bool) {
	ids := buf[:0]
	if !bytes.HasPrefix(bytes.TrimSpace(data), base64WrapperPrefix) {
		return ids, false
	}
	var w struct {
		Base64 []byte `json:"base64"`
	}
	if err := json.Unmarshal(data, &w); err != nil {
		return ids, false
	}
	b := w.Base64
	for len(b) > 0 {
		key, n := protoVarint(b)
		if n == 0 {
			return ids, false
		}
		b = b[n:]
		field, wire := key>>3, key&7
		var value []byte
		switch wire {
		case 0:
			_, n = protoVarint(b)
			if n == 0 {
				return ids, false
			}
			b = b[n:]
		case 1, 5:
			size := 8
			if wire == 5 {
				size = 4
			}
			if len(b) < size {
				return ids, false
			}
			b = b[size:]
		case 2:
			size, n := protoVarint(b)
			if n == 0 || size > uint64(len(b)-n) {
				return ids, false
			}
			value = b[n : n+int(size)]
			b = b[n+int(size):]
		default:
			return ids, false
		}
		if field != 1 {
			continue
		}
		if wire != 2 || len(value) != 32 {
			return ids, false
		}
		id, ok := index[[32]byte(value)]
		if !ok {
			return ids, false
		}
		ids = append(ids, id)
	}
	return ids, len(ids) > 0
}

// cursorCLIIDKey is the 32 bytes of a 64-hex-digit blob id.
func cursorCLIIDKey(s string) ([32]byte, bool) {
	var key [32]byte
	if len(s) != 64 {
		return key, false
	}
	if _, err := hex.Decode(key[:], []byte(s)); err != nil {
		return key, false
	}
	return key, true
}

// cursorCLISeq reads a blob's seq. Anything but a JSON integer that
// fits an int64, null and a missing seq included, is no seq.
func cursorCLISeq(raw json.RawMessage) (int64, bool) {
	v, err := strconv.ParseInt(string(bytes.TrimSpace(raw)), 10, 64)
	return v, err == nil
}

// protoVarint reads one protobuf varint. n is 0 when b does not start
// with a complete varint of at most ten bytes.
func protoVarint(b []byte) (v uint64, n int) {
	for i := 0; i < len(b) && i < 10; i++ {
		v |= uint64(b[i]&0x7f) << (7 * i)
		if b[i]&0x80 == 0 {
			return v, i + 1
		}
	}
	return 0, 0
}
