package recall

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"terva.sh/lampi/internal/normalize"
)

// Excerpt limits. A span is bounded by events and by output bytes.
const (
	// ExcerptMaxEvents is the most events one excerpt covers.
	ExcerptMaxEvents = 200
	// ExcerptBytes caps the rendered text. Events past it are dropped
	// and the excerpt says so.
	ExcerptBytes = 512 << 10
	// excerptContentMax is how much of one event's text an excerpt
	// keeps.
	excerptContentMax = 64 << 10
)

// Excerpt is a run of events rendered as plain text ready to paste
// into a new session. Text is what the viewer copies and the plain
// page serves. Its header names the session, harness, project and a
// link back to the first event.
type Excerpt struct {
	SessionUID string `json:"session_uid"`
	Generation int64  `json:"generation"`
	From       int64  `json:"from"`
	To         int64  `json:"to"`
	Events     int    `json:"events"`
	Truncated  bool   `json:"truncated"`
	Link       string `json:"link"`
	Text       string `json:"text"`
}

// ExcerptRequest is a span [From, From+Count). Gen pins the generation
// when Pinned is set. Origin, when set, makes the header link absolute,
// so a pasted excerpt can be followed from anywhere.
type ExcerptRequest struct {
	From   int64
	Count  int
	Gen    int64
	Pinned bool
	Origin string
}

// Excerpt renders the span of uid's published events as text. It pins
// one generation the way Events does, so a copy never mixes two. A
// span that starts past the end is ErrInvalid.
func (r *Reader) Excerpt(ctx context.Context, uid string, req ExcerptRequest) (Excerpt, error) {
	if req.From < 0 || req.Count < 1 || req.Count > ExcerptMaxEvents || req.Gen < 0 {
		return Excerpt{}, ErrInvalid
	}
	snap, err := r.open(ctx, uid)
	if err != nil {
		return Excerpt{}, err
	}
	defer snap.Close()
	if req.Pinned && req.Gen != snap.pub.Gen {
		return Excerpt{}, ErrGenerationChanged
	}
	summary, err := r.catalog.DashboardSession(ctx, uid)
	if err != nil {
		return Excerpt{}, err
	}
	br := bufio.NewReaderSize(snap.f, 64<<10)
	if req.From > 0 {
		if _, _, err := skipLines(ctx, br, req.From); err != nil {
			return Excerpt{}, err
		}
	}
	ex := Excerpt{SessionUID: uid, Generation: snap.pub.Gen, From: req.From, To: req.From}
	ex.Link = req.Origin + EventLink(uid, snap.pub.Gen, req.From)
	var body strings.Builder
	pos := req.From
	for ex.Events < req.Count {
		if err := ctx.Err(); err != nil {
			return Excerpt{}, err
		}
		line, n, rerr := readLine(br, MaxLine)
		if rerr == io.EOF && n == 0 {
			break
		}
		if rerr != nil && rerr != io.EOF {
			return Excerpt{}, rerr
		}
		block := renderEvent(pos, line)
		if ex.Events > 0 && body.Len()+len(block) > ExcerptBytes {
			ex.Truncated = true
			break
		}
		body.WriteString(block)
		ex.Events++
		pos++
		ex.To = pos
		if rerr == io.EOF {
			break
		}
	}
	if ex.Events == 0 {
		return ex, ErrInvalid
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Excerpt from a stored %s session (lampi session lake).\n", summary.Harness)
	fmt.Fprintf(&b, "Session: %s (%s)\nProject: %s\n", summary.NativeID, uid, summary.ProjectLabel)
	fmt.Fprintf(&b, "Events #%d to #%d of generation %d. Source: %s\n", ex.From, ex.To-1, ex.Generation, ex.Link)
	b.WriteString(body.String())
	if ex.Truncated {
		fmt.Fprintf(&b, "\n[excerpt ends at #%d: size limit reached]\n", ex.To-1)
	}
	ex.Text = b.String()
	return ex, nil
}

// renderEvent is one event as a labelled plain-text block. Opaque
// encrypted content is dropped, as in the viewer. The rendering is not
// JSON, so a paste is legible, and it keeps order and position.
func renderEvent(pos int64, line []byte) string {
	var b strings.Builder
	if line == nil {
		fmt.Fprintf(&b, "\n[#%d oversized event omitted]\n", pos)
		return b.String()
	}
	var ev normalize.Event
	if json.Unmarshal(line, &ev) != nil {
		fmt.Fprintf(&b, "\n[#%d unreadable event omitted]\n", pos)
		return b.String()
	}
	who := ev.Actor
	if ev.EventType != "" && ev.EventType != normalize.EventMessage {
		who += " " + ev.EventType
	}
	fmt.Fprintf(&b, "\n[#%d %s", pos, who)
	if ev.Tool.Name != nil {
		fmt.Fprintf(&b, " %s", *ev.Tool.Name)
	}
	if ev.Tool.IsError != nil && *ev.Tool.IsError {
		b.WriteString(" (error)")
	}
	if ev.RecordedAt != "" {
		fmt.Fprintf(&b, " %s", ev.RecordedAt)
	}
	b.WriteString("]\n")
	if ev.ContentText != nil && *ev.ContentText != "" {
		text := *ev.ContentText
		if len(text) > excerptContentMax {
			text = truncate(text, excerptContentMax) + "\n… content truncated"
		}
		b.WriteString(text)
		if !strings.HasSuffix(text, "\n") {
			b.WriteString("\n")
		}
	}
	if ev.Extra != nil {
		m := map[string]any(ev.Extra)
		if stripOpaque(m, 0) {
			b.WriteString("[encrypted content omitted]\n")
		}
	}
	return b.String()
}
