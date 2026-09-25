package catalog

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"

	"terva.sh/lampi/internal/protocol"
)

// BlobReader opens one immutable object. CAS objects are content
// addressed, so a read during the ingest transaction is stable.
type BlobReader interface {
	Open(digest string) (io.ReadCloser, error)
}

// relateBuf is how much of each object Relate holds at once.
const relateBuf = 64 << 10

// Relate is protocol.RelationOf for the stored object and the client
// object, read side by side as streams. Neither is held in memory, so
// comparing a file past the blob cap costs two buffers, not two files.
// The client bytes are hashed on the way; a hash that is not client
// is an error.
func Relate(blobs BlobReader, stored, client string) (string, error) {
	if stored == client {
		return protocol.RelationUnchanged, nil
	}
	s, err := blobs.Open(stored)
	if err != nil {
		return "", fmt.Errorf("catalog: head %s: %w", stored, err)
	}
	defer s.Close()
	c, err := blobs.Open(client)
	if err != nil {
		return "", fmt.Errorf("catalog: blob %s: %w", client, err)
	}
	defer c.Close()
	h := sha256.New()
	cr := io.TeeReader(c, h)
	rel, err := relateStreams(s, cr)
	if err != nil {
		return "", fmt.Errorf("catalog: compare %s with %s: %w", client, stored, err)
	}
	// The comparison can stop at the first difference. The rest of the
	// client still has to hash to its digest.
	if _, err := io.Copy(io.Discard, cr); err != nil {
		return "", fmt.Errorf("catalog: blob %s: %w", client, err)
	}
	if hex.EncodeToString(h.Sum(nil)) != client {
		return "", fmt.Errorf("catalog: blob %s does not match its sha256", client)
	}
	return rel, nil
}

// relateStreams reads stored and client in step. The first byte that
// differs is a divergent copy. The one that ends first, with every
// byte before equal, is the prefix of the other.
func relateStreams(stored, client io.Reader) (string, error) {
	sb := make([]byte, relateBuf)
	cb := make([]byte, relateBuf)
	for {
		sn, serr := io.ReadFull(stored, sb)
		cn, cerr := io.ReadFull(client, cb)
		if serr != nil && !endOf(serr) {
			return "", serr
		}
		if cerr != nil && !endOf(cerr) {
			return "", cerr
		}
		n := min(sn, cn)
		if !bytes.Equal(sb[:n], cb[:n]) {
			return protocol.RelationDivergentCopy, nil
		}
		switch {
		case sn == cn && endOf(serr) && endOf(cerr):
			return protocol.RelationUnchanged, nil
		case sn < cn:
			// stored ended inside this buffer and client went on.
			return protocol.RelationGrownFrom, nil
		case cn < sn:
			return protocol.RelationStale, nil
		}
		// Both buffers were full and equal.
	}
}

func endOf(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}
