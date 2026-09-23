# Capture protocol 1

`terva-lampi serve` and `terva-lampi sync` speak this over HTTP JSON.
The types live in `internal/protocol`. The version field is
`capture_protocol`. This tree speaks version 1 only.

Agents dial the lake. The lake does not dial the agents. Laptops behind
NAT do not open an inbound port.

`GET /healthz` is the exception to auth: it returns `{"status":"ok"}` and
nothing about the catalog, so a process probe does not need a token.
Every `/v1` route requires `Authorization: Bearer <token>` when the
server was started with `--token-file`. With no token file, `terva-lampi serve`
accepts `/v1` unauthenticated only on a loopback address and refuses any
other `--addr`. The client reads the token from `--token-file` and does
not take it as an argument. The server stores a SHA-256 of each device
token and rewrites that file to `sha256:<hex>` lines. One tenant, many
devices: each device has its own token. The client's copy stays the
secret; point `serve` at a copy.

## GET /v1/stats

Catalog counts for an operator. This is not healthz. It uses the same
bearer check as the other `/v1` routes, and it returns no session bodies.

```json
{"sessions": 1, "artifacts": 2, "machines": 1}
```

`sessions` and `artifacts` are row counts. `machines` is the number of
distinct `machine_id` values in provenance. `terva-lampi status` prints
these. A process probe should keep using `/healthz`.

## POST /v1/hello

The client calls this first. The body is ignored.

```json
{
  "server_time": "2026-09-22T16:10:00Z",
  "protocol_versions": [1],
  "max_blob_bytes": 33554432
}
```

`server_time` is there so a client can notice clock skew. Manifest mtimes
are hints. The catalog's `ingested_at` is the server clock. The client
warns when its clock and `server_time` differ by more than five minutes
(`protocol.ClockSkewWarn`) and still uploads. It checks that version 1
is in `protocol_versions`. A PUT body, a Content-Range total, and one
chunk each stay under `max_blob_bytes`. A file that is already over that
cap is split into chunks of at most that size. The manifest names the
chunks. The lake does not install one object for that concatenation.

## POST /v1/blobs/check

Request body is a JSON array of lowercase sha256 hex digests, not an object.

```json
["0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"]
```

```json
{"missing": ["0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"]}
```

Digests the lake already has are omitted. Duplicates in the request are
reported once.

## PUT /v1/blobs/{sha256}

The body is the raw bytes. `Content-Length` is optional. The server hashes
the body and refuses it when the hash does not equal the path. A digest
that is already stored is a success and writes nothing:

```json
{"exists": true, "sha256": "...", "complete": true}
```

`exists` is false when this call stored the object. The filesystem key is
`sha256/<ab>/<rest of the digest>`. `complete` is true for a finished
object, including one that already existed.

A single body larger than `max_blob_bytes` is refused. Two resume forms
are accepted. Both install the digest only when the pieces assemble, and
both store nothing when that digest is already present.

`Content-Range: bytes start-end/total` writes that inclusive slice. `end`
is the last byte. The total is required. A gap leaves the upload
incomplete (`complete` false) under `partial/` and does not install the
object. When the ranges cover `[0, total)`, the bytes are hashed, and
the object is installed only if the hash is the path. A mismatch deletes
the partial, so the client can send the bytes again. A later PUT of a
digest that is already stored does not write the range.

A JSON body is the other form:

```json
{"chunk_sha256s": ["<sha256 of chunk 0>", "<sha256 of chunk 1>"]}
```

Each chunk is its own object, uploaded with the ordinary PUT. The server
concatenates them in order and installs the result when the hash matches
the path and the concatenation fits under `max_blob_bytes`. A longer
concatenation is refused here, because this PUT installs one object.
A file over the cap is not installed by this call: its chunks are
ordinary PUTs, and the manifest records the list. A chunk that is not
in the CAS is `409` with `missing`. The `Content-Type` is
`application/json`. Sending `Content-Range` and a chunk list on the
same PUT is `400`.

## POST /v1/manifests

One logical session. The server stores a row only when every artifact
digest is already in the CAS. Otherwise it returns 409 and `missing`.

```json
{
  "capture_protocol": 1,
  "machine_id": "01ARZ3NDEKTSV4RRFFQ69G5FAV",
  "harness": "terva",
  "harness_version": "0.137.0",
  "native_session_id": "20260922-161000-abcd1234",
  "project": {
    "cwd": "/home/drew/src/foo",
    "cwd_hash": "a1b2c3d4e5f60708",
    "git_remote": "git@github.com:org/foo.git",
    "git_commit": "fedcba9876543210fedcba9876543210fedcba98",
    "git_root": "0123456789abcdef0123456789abcdef01234567",
    "project_id": "github.com/org/foo@0123456789abcdef0123456789abcdef01234567"
  },
  "artifacts": [
    {
      "kind": "transcript_jsonl",
      "relpath": "sessions/a1b2c3d4e5f60708/20260922-161000-abcd1234.jsonl",
      "size": 120400,
      "mtime": "2026-09-22T16:10:00Z",
      "sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
      "chunk_sha256s": null,
      "byte_watermark_prev": 100000,
      "tail_sha256": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
      "redaction": {"status": "scanned", "ruleset": "v1", "hits": 0}
    }
  ],
  "lineage": {"parent_native_id": null, "fork_point": null}
}
```

`harness` is `terva`, `claude`, `codex`, or `opencode`. For terva,
`harness_version` is the producer version from the meta line when that
line has one. For Claude Code, Codex, and OpenCode, `harness_version`
is the adapter's pinned reader version. The on-disk object for those
three is internal to the adapter and is not part of this protocol.
`history.jsonl` under a Codex home is not a session. An OpenCode
session is one `opencode export` document under `export/`. The WAL
sidecar next to `opencode.db` is not a session.

`cwd_hash` is terva's `hex(sha256(cwd)[:8])`. It buckets a path on one
machine. It is not a project id across machines. Claude, Codex, and
OpenCode use the same function so an allow rule written against that
hash still matches. OpenCode takes the cwd from `info.directory` on
the export. A discovered database file has no directory, so the
allowlist refuses that blob. `git_remote` is origin's URL when the
session cwd has a `.git`, and empty otherwise. `git_commit` is HEAD.
`git_root` is the first
parentless commit on that HEAD's first-parent chain. `project_id` is
the remote folded the same way as an allow rule, then `@`, then
`git_root` in lowercase hex. `git@github.com:org/foo.git` and
`https://github.com/org/foo` with the same root are one id. The path
and `cwd_hash` are not inputs. A missing origin, a missing root, or a
shallow clone leaves `project_id` empty, and those sessions are not
grouped. The lake recomputes `project_id` from `git_remote` and
`git_root` on ingest and does not keep a client value that disagrees.
The client allowlist matches the cwd, this hash, or the remote before
the manifest is sent.

`fork_point` is raw JSON. terva uses an index. The sketch allows null.

`kind` is `transcript_jsonl` or `errors_jsonl` for the sidecar that sits
next to a terva transcript.

`sha256` is always the full file. `chunk_sha256s` lists the CAS objects
that concatenate to it, in order. Null means the file was one PUT.
`chunk_lengths`, when set, is parallel to that list: each stored
object's length in bytes. The lengths sum to `size`. Lengths are
required when that sum is greater than `max_blob_bytes`. A non-empty
list is accepted when every chunk is already stored; a missing chunk
is `409` and `missing`. When the concatenation fits under
`max_blob_bytes`, the lake installs that one object and Layer B
compares it. When it does not fit, the chunks stay separate, the
assembled bytes are not stored, and Layer B reads the concatenation.
A length that does not match the stored object, or a hash that is not
`sha256`, is `400`. `chunk_sha256s` is not combined with a tail: a tail
is one blob, named by `tail_sha256`, and assembling a tail also stays
under `max_blob_bytes`. A file over the cap is sent whole, as chunks,
with `byte_watermark_prev` 0.

`byte_watermark_prev` of 0 means the PUT body is that file and
`tail_sha256` equals `sha256`. A non-zero prev
means the PUT body is only the bytes after that offset, `tail_sha256` is
the hash of those bytes, and `size` is the full file. `terva-lampi sync`
sends the tail form when the local watermark is a strict prefix of the
file. The example above is that form.

The lake compares the full bytes to the stored head for that path:

| Client bytes | Result |
|--------------|--------|
| Same digest as the head | No-op. Same artifact id. No new blob. |
| Strict extension of the head | Assemble a tail (or accept the whole file). `head_sha256` moves. The new artifact's relation is `grown_from`. |
| Strict prefix of the head | The client is stale. The head stays. `relation` is `stale`. |
| Neither is a prefix | New artifact, `relation` `divergent_copy`. The head stays. The copies are not merged. |

A tail whose prev is not the stored head's length, or whose assembly
hash is not `sha256`, is `409` `{"error":"prefix mismatch"}`. The client
PUTs the whole file and posts the manifest again with prev 0.

A 200 body is the ACK. The client may advance a watermark only after it
sees this. `internal/watermark` enforces that. `terva-lampi sync` commits
the cursor from this ACK and leaves it unchanged when the POST fails.
On `stale`, the transcript cursor's offset advances to `head_size`.
Size and sha256 stay the local file, which is still a prefix of that
head. The next sync does not post the prefix again. The lake does not
send the missing suffix back.

```json
{
  "session_uid": "01ARZ3NDEKTSV4RRFFQ69G5FAV",
  "artifact_ids": ["01ARZ3NDEKTSV4RRFFQ69G5FAW"],
  "head_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "head_size": 120400,
  "relation": "grown_from"
}
```

`relation` is `head`, `grown_from`, `divergent_copy`, `unchanged`, or
`stale`. It is the transcript artifact's relation when one is present.

`session_uid` is assigned once per `(harness, native_session_id)`. An
alias maps `(harness, native_session_id, machine_id)` to that uid. A
second machine posting the same native id joins that row. The same
digest adds a provenance row and no blob. Repeating the same artifact
digest returns the same `artifact_id`.

`head_sha256` is the transcript artifact when one is present, otherwise
the last artifact. A `divergent_copy` or a `stale` post does not change it.

## Not in this scaffold

**Pull.** Push only. A stale client is not repaired from the lake.

**Redaction.** `redaction.status` of `scanned` means ruleset v1 ran and
found nothing. `ruleset` is `v1`. `override` means the same scan found
hits and `redaction.upload_hits` was set; `hits` is the count, not the
secrets. A hit without that override is quarantined locally and is not
in a manifest. `unscanned` is what a client sends when it did not scan.
`serve` does not scan again.

## Errors

Failures are JSON: `{"error":"..."}`. A missing-blob conflict adds
`"missing": ["<sha256>", ...]`.

| Status | When |
|--------|------|
| 400 | Bad JSON, bad digest, bad content-range, assembled hash mismatch, unsupported protocol, tail combined with chunks, catalog rejection |
| 401 | Bearer token missing or wrong |
| 409 | Manifest or chunk list names a digest that is not in the CAS, or a tail is not a prefix extension |
| 200 | Hello, check, put, manifest ACK, health |
