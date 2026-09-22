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
are hints. The catalog's `ingested_at` is the server clock. This client
checks that version 1 is in `protocol_versions` and refuses a file larger
than `max_blob_bytes`. Chunking above that limit is not implemented.

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

A 200 body is the ACK. The client may advance a watermark only after it
sees this. `internal/watermark` enforces that. `terva-lampi sync` commits
the cursor from this ACK and leaves it unchanged when the POST fails.
`byte_watermark_prev` is the stored offset when this upload continues
that cursor, and 0 when the file is new or was rewritten.

```json
{
  "session_uid": "01ARZ3NDEKTSV4RRFFQ69G5FAV",
  "artifact_ids": ["01ARZ3NDEKTSV4RRFFQ69G5FAW"],
  "head_sha256": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
}
```

`session_uid` is assigned once per `(harness, native_session_id)`. A
second machine posting the same native id joins that row and adds
provenance. Repeating the same artifact digest returns the same
`artifact_id`. A new digest for the same path adds an artifact and moves
`head_sha256`. The previous blob stays in the CAS.

`head_sha256` is the transcript artifact when one is present, otherwise
the last artifact.

## Not in this scaffold

These rules are the protocol. The code does not apply them yet.

**Append-only merge.** If the client file starts with the stored bytes,
only the tail is a new blob and `head_sha256` moves. If the stored file
starts with the client file, the client is stale. If neither is a prefix,
the copy is `divergent_copy`: both blobs are kept, linked, and not merged.
Today a changed file is a whole new blob. The session uid still stays.

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
| 409 | Manifest names a digest that is not in the CAS |
| 200 | Hello, check, put, manifest ACK, health |
