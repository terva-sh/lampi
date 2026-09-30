package web

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"terva.sh/lampi/internal/normalize"
	"terva.sh/lampi/internal/recall"
)

// minQuietRun is the shortest stretch of quiet events the transcript
// page folds into one section. Two in a row read fine as cards.
const minQuietRun = 3

// transcriptEntry is one card on the transcript page. Target marks the
// deep link's event.
type transcriptEntry struct {
	recall.EventItem
	Target bool
}

// transcriptBlock is either one card or a run of quiet cards shown
// folded under a summary. Open unfolds a run holding the target.
type transcriptBlock struct {
	Entries []transcriptEntry
	Run     bool
	Open    bool
	From    int64
	To      int64
	Kinds   string
}

// quiet says an event is an unknown one with nothing to read: no text
// and no hint about what was left out. These are the bookkeeping
// records a harness writes between turns, such as Claude Code's
// file-history-snapshot and queue-operation lines.
func quiet(it recall.EventItem) bool {
	ev := it.Event
	if ev == nil || it.Oversized || it.Unreadable || it.Opaque || it.Truncated {
		return false
	}
	return ev.EventType == normalize.EventUnknown && (ev.ContentText == nil || *ev.ContentText == "")
}

// unknownKind names what an unknown event was in the harness's own
// terms: the raw record type, with the system subtype or the content
// block type after a slash when there is one.
func unknownKind(ev *normalize.Event) string {
	if ev == nil || ev.RawType == "" {
		return ""
	}
	for _, key := range []string{"subtype", "block_type"} {
		if s, ok := ev.Extra[key].(string); ok && s != "" {
			return ev.RawType + "/" + s
		}
	}
	return ev.RawType
}

// transcriptBlocks groups one page's events for display. A run of at
// least minQuietRun quiet events becomes one folded block; everything
// else is a block of one. Runs are found within the page, so a stretch
// that crosses a page boundary folds on each side separately.
func transcriptBlocks(items []recall.EventItem, target int64, hasTarget bool) []transcriptBlock {
	var out []transcriptBlock
	for i := 0; i < len(items); {
		j := i
		for j < len(items) && quiet(items[j]) {
			j++
		}
		if j-i >= minQuietRun {
			b := transcriptBlock{Run: true, From: items[i].Position, To: items[j-1].Position}
			counts := map[string]int{}
			for _, it := range items[i:j] {
				e := transcriptEntry{EventItem: it, Target: hasTarget && it.Position == target}
				b.Open = b.Open || e.Target
				b.Entries = append(b.Entries, e)
				k := unknownKind(it.Event)
				if k == "" {
					k = "unnamed"
				}
				counts[k]++
			}
			b.Kinds = kindCounts(counts)
			out = append(out, b)
			i = j
			continue
		}
		// Not a run: emit the next event alone, and carry on from the
		// one after, so a short quiet stretch renders card by card.
		it := items[i]
		out = append(out, transcriptBlock{Entries: []transcriptEntry{{EventItem: it, Target: hasTarget && it.Position == target}}})
		i++
	}
	return out
}

// kindCounts renders a run's kinds, most frequent first, as
// "file-history-snapshot ×9, queue-operation ×4".
func kindCounts(counts map[string]int) string {
	kinds := make([]string, 0, len(counts))
	for k := range counts {
		kinds = append(kinds, k)
	}
	slices.SortFunc(kinds, func(a, b string) int {
		return cmp.Or(cmp.Compare(counts[b], counts[a]), cmp.Compare(a, b))
	})
	parts := make([]string, len(kinds))
	for i, k := range kinds {
		parts[i] = fmt.Sprintf("%s ×%d", k, counts[k])
	}
	return strings.Join(parts, ", ")
}
