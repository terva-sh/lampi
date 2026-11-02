package web

import (
	"strings"
	"testing"
	"time"

	"terva.sh/lampi/internal/catalog"
	"terva.sh/lampi/internal/protocol"
)

// TKT-01M6F7JMGV9XDW5Z4SZJ74J9H3: list pages use the wide main, a long
// project wraps at its separators instead of being cut off, machines
// show by device name, and times read as UTC to the second with the
// full value kept.
func TestSessionsTableWideAndNamed(t *testing.T) {
	lake, idp, h, _ := rawLake(t, nil)
	ctx := t.Context()
	now := time.Now()
	remote := "ssh://git@git.example.com:2222/a-long-owner/a-rather-long-repository-name.git"
	m := protocol.Manifest{CaptureProtocol: protocol.Version, MachineID: "machine-a", Harness: "claude", NativeSessionID: "wide-1",
		Project:   protocol.Project{CWD: "/home/me/src/app", GitRemote: remote},
		Artifacts: []protocol.Artifact{{Kind: protocol.KindTranscriptJSONL, RelPath: "p/wide-1.jsonl", SHA256: putBlob(t, lake, []byte("{}\n")), Size: 3}}}
	if _, err := lake.Catalog.Ingest(ctx, m, now, []catalog.Decision{{Relation: protocol.RelationHead, Record: true, Head: true}}, nil); err != nil {
		t.Fatal(err)
	}
	ds, err := lake.Catalog.SyncTokenFile(ctx, []catalog.TokenEntry{{Hash: strings.Repeat("b", 64), Name: "laptop"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lake.Catalog.BindMachine(ctx, ds[0].ID, "machine-a", now); err != nil {
		t.Fatal(err)
	}
	admin := signInAs(t, idp, h, "owners")
	page := get(h, "/sessions", admin).Body.String()
	for _, want := range []string{
		`<main id="main" class="wide">`,
		`<table class="sessions-table">`,
		`ssh:<wbr>/<wbr>/<wbr>git@git.example.com:<wbr>2222/<wbr>a-long-owner/<wbr>`,
		`<span class="sub" title="machine-a">laptop</span>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("sessions page lacks %q", want)
		}
	}
	stamp := now.UTC().Format("2006-01-02 15:04:05Z")
	if !strings.Contains(page, `">`+stamp+`</time>`) && !strings.Contains(page, `">`+now.UTC().Add(-time.Second).Format("2006-01-02 15:04:05Z")+`</time>`) {
		t.Errorf("sessions page does not show the update as %s", stamp)
	}
	if strings.Contains(get(h, "/sessions", admin).Body.String(), `<span class="sub mono">machine-a</span>`) {
		t.Error("the machine still shows by ID")
	}
	if !strings.Contains(get(h, "/review", admin).Body.String(), `<main id="main" class="wide">`) {
		t.Error("the review page is not wide")
	}
}

func TestWhen(t *testing.T) {
	for in, want := range map[string]string{
		"2026-11-02T22:21:00.8352861Z": "2026-11-02 22:21:00Z",
		"2026-11-02T16:21:00-06:00":    "2026-11-02 22:21:00Z",
		"never":                        "never",
		"":                             "",
	} {
		if got := when(in); got != want {
			t.Errorf("when(%q) = %q, want %q", in, got, want)
		}
	}
}
