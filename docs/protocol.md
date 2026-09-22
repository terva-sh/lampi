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
other `--addr`. The comparison is plaintext. Hashing the token at rest is
not implemented.

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
is in `protocol_versions` and refuses a file larger than `max_blob_bytes`.
Chunking above that limit is not implemented.

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
{"exists": true, "sha256": "..."}
```

`exists` is false when this call stored the object. The filesystem key is
`sha256/<ab>/<rest of the digest>`.

A blob larger than `max_blob_bytes` is refused. The chunked form
(`chunk_sha256s`, `Content-Range`) is specified below and returns 400.

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
    "git_commit": "abc123"
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

`cwd_hash` is terva's `hex(sha256(cwd)[:8])`. It buckets a path on one
machine. It is not a project id across machines. `git_remote` is origin's
URL when the session cwd has a `.git`, and empty otherwise. Folding that
into a project id across machines is later work. The client allowlist
matches the cwd, this hash, or the remote before the manifest is sent.

`fork_point` is raw JSON. terva uses an index. The sketch allows null.

`kind` is `transcript_jsonl` or `errors_jsonl` for the sidecar that sits
next to a terva transcript.

`sha256` is always the full file. `byte_watermark_prev` of 0 means the
PUT body is that file and `tail_sha256` equals `sha256`. A non-zero prev
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

**Chunks.** Files over `max_blob_bytes` split into CAS objects. The
manifest lists `chunk_sha256s`. The server assembles the artifact when
every chunk is present. `PUT` with `Content-Range` is the other spelling
of the same idea. Both are refused.

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
| 400 | Bad JSON, bad digest, unsupported protocol, chunked artifact, catalog rejection |
| 401 | Bearer token missing or wrong |
| 409 | Manifest names a digest that is not in the CAS, or a tail is not a prefix extension |
| 200 | Hello, check, put, manifest ACK, health |
