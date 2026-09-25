---
schema: 3
id: TKT-01M3B369A74CEB0MPSVH2MAHQV
title: "Redaction v2: JSON-escaped context, rule gaps, base64 values"
type: bug
status: done
status_reason: null
priority: urgent
due_on: null
labels:
  - area/redact
  - area/adapter
assignees: []
milestone: null
parent: TKT-01M3B35JS8J2F83FG4J7ZC0199
origin: null
dependencies: []
blocks_on: none
references: []
claim: null
archive: null
created_at: 2026-09-25T01:34:31Z
updated_at: 2026-09-25T01:59:59Z
created_by:
  id: agent:claude-code/eh1m
  name: Claude Code cloud agent
updated_by:
  id: agent:claude-code/eh1m
  name: Claude Code cloud agent
extensions: {}
---

## Description

Ruleset v1 misses secrets in the context where transcripts hold them: inside JSON strings.

### Findings

- Escapes defeat `\b`. Proven. The scan runs on raw JSONL, where a line break in a string is `\` then `n`. `n`, `t`, and `r` are word characters, so the leading `\b` on most rules in `internal/redact/redact.go:59-81` fails. A key after `\n` or `\t` scans clean and uploads. `Strip` has the same gap on tool arguments kept as raw JSON text, so the key reaches ShareGPT rows.
- Other gaps. Proven. `AWS_SECRET_ACCESS_KEY=\"…\"` inside JSON, `BEGIN PGP PRIVATE KEY BLOCK`, a Google key ending in `-` (trailing `\b`), and Slack `xapp-`. `Strip` of a JSON-escaped PEM key replaces the BEGIN line and leaves the body.
- Base64 hides secrets. Proven. The Cursor adapters export non-UTF-8 values as `{"base64":…}` (`cursorcli/snapshot.go:327`, `cursor/snapshot.go:444`), and the upload scan only sees the export.
- The AWS documentation example key `AKIAIOSFODNN7EXAMPLE` is a hit, so a session that read SDK docs is quarantined for good.

### Approach

Scan a length-preserving view in which JSON escapes are padded, and map spans back. Replace the trailing `\b` on fixed-length rules. Allow ` BLOCK` in the PEM header and add `xapp-`. Scan raw row values in the Cursor adapters before encoding. Skip a short list of published example values. Ship this as ruleset `v2`, so the manifest stamp tells old scans from new. A table test covers every rule in escaped context.

## Acceptance criteria

- [x] Every rule matches its secret after a JSON escape; a table test covers it
- [x] PGP private key blocks, xapp- tokens, and a Google key ending in - are matched
- [x] Strip removes the whole of a JSON-escaped PEM key
- [x] Cursor raw values are scanned before base64 encoding
- [x] Manifests stamp ruleset v2

## Implementation plan

### Verified

All four findings reproduce against `internal/redact/redact.go`. Also measured: v1 Scan runs at about 3 MB/s on a 32 MiB JSONL (10.5 s). Every rule that opens with `\b` loses Go's literal-prefix search, so each one walks the whole buffer in the NFA.

### Approach

- Scan and Strip run on a length-preserving view of the bytes. A run of backslashes plus the escape character becomes padding (spaces) plus the decoded character, so `\n` is " \n" and `\"` is ` "`. `\/` becomes `//`. `\uXXXX` becomes spaces plus the ASCII character. Offsets map back 1:1, so Strip replaces the raw span. A run of backslashes is treated as one escape, so doubly escaped tool arguments (`\\n`) are covered too. A buffer with no backslash is scanned as is.
- Each rule names the literal prefixes a match starts with. The scan finds them with `bytes.Index`, checks the leading word boundary in code, and runs the rule anchored at that offset. That replaces the leading `\b` and should make v2 faster than v1, not slower.
- The trailing `\b` on Google keys and SendGrid keys becomes a check that the next byte is not a key character, so a key ending in `-` matches.
- Rules: PEM header allows several words and ` BLOCK` (PGP) and line breaks allow `\r` padding. The AWS secret rule accepts quotes around the key name and `SecretAccessKey` (CLI JSON). Slack adds `xapp-`. New fixed-prefix rules: anthropic-key (`sk-ant-`, split from openai-key), pypi-token, huggingface-token, sendgrid-key, digitalocean-token. Twilio has no fixed-prefix secret (the SK SID is not the secret), so it is left out. No generic password= rules.
- Published example values (AWS doc key pair, Slack's placeholder webhook) are not hits.
- Scan counts the same de-overlapped spans Strip replaces, so one `sk-ant-` key is one hit.
- Cursor IDE and CLI adapters scan each raw value they encode as base64 (and hex meta that is not JSON) and put the result on the bundle, keyed by the export digest. prepare.go adds it to the export scan, so a hit quarantines the export.
- Ruleset stamp becomes `v2`. The lake does not check the stamp, so v1 manifests stay valid; an api test pins that.
- Docs: README, architecture, protocol.

## Notes

**agent:claude-code/eh1m** at 2026-09-25T01:59:33Z

### Where this differs from the proposed approach, and why

- Prefix search instead of the leading `\b`. Each rule names its literal prefixes. `bytes.Index` finds them, the leading boundary is checked in code, and the rule runs anchored there. v1 took 7.8 s to scan a 32 MiB Claude-style JSONL (4.3 MB/s): every `\b`-led rule walked the whole buffer in the NFA. v2 takes 0.44 s (76 MB/s). It allocates one copy of the buffer for the escape view (34 MB/op against 54 KB/op). The case-insensitive AWS rule lowers 64 KiB windows, so it does not need a second copy. Scan's signature is unchanged.
- Escape view. A run of backslashes counts as one escape, so doubly escaped tool arguments (`\\n` inside a JSON string that holds JSON) are covered too. `\/` becomes `//`, and the Slack webhook rule accepts `/+`.
- Scan counts the de-overlapped spans that Strip replaces. `sk-ant-` is its own rule (anthropic-key) ahead of openai-key, so one key is one hit.
- The AWS secret rule also takes a quoted key name and `SecretAccessKey` (CLI JSON output). The span starts at `secret` and takes in a preceding `aws_`. The Slack webhook span starts at `hooks.slack.com`, so Strip leaves `https://` in place.
- Cursor: `encodeValue` returns the scan of any value it turns into base64. Cursor CLI hex meta that does not decode to JSON has its decoded bytes scanned as well. The adapters put the total on the new `adapter.Bundle.Hidden`, keyed by export digest. The cursor IDE counts only rows that stay in the export after the composer filter. `prepare.go` adds that total to the export's own scan.
- Twilio is left out: its auth token has no prefix, and the `SK` id that has one is not the secret.

### Open

- Normalized events still carry `redaction: {status: "none", ruleset: "v1"}` (normalize/*.go). That object says the event text was not changed. It is not a manifest stamp, so I left it alone. Changing it touches every normalizer and its golden tests.
- v2 does not rescan bytes a v1 client already put on the lake.

## Summary

Ruleset v2 landed on claude/elegant-feynman-eh1mdd-redact. Scan and Strip read a length-preserving view of JSON escapes, so a key after \n, \t, \r, \" or \uXXXX matches, and so does one inside doubly escaped tool arguments. Strip removes a JSON-escaped PEM or PGP block whole. The rules find their literal prefixes with bytes.Index, which makes a 32 MiB scan 7.8 s to 0.44 s. New rules: PGP private-key blocks, Slack xapp-, Anthropic, PyPI, Hugging Face, SendGrid, and DigitalOcean. A Google or SendGrid key ending in '-' now matches. AWS and Slack documentation examples are not hits. The Cursor IDE and CLI readers scan raw values before base64 (and CLI hex meta) and pass the result on adapter.Bundle.Hidden, which the upload adds to the export scan. The quarantine path is unchanged. Manifests stamp v2. The lake accepts v1 and v2 (api test). Tests: rules_test.go covers 28 fixtures in 9 contexts for Scan and Strip, plus examples, near-misses, and fold windows. Adapter unit tests and an end-to-end Sync quarantine test cover the IDE and the CLI.
