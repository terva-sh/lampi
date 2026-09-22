// Package watermark will remember how far each file has been uploaded.
//
// The key is (machine_id, harness, root_path, relative_path). The record
// is the last uploaded size, mtime, content sha256, and the byte offset
// for an append-only file. The client writes it only after a manifest ACK.
//
// This scaffold sends byte_watermark_prev 0 and the whole file every time
// the digest is new. The server still dedups by sha256, so a re-sync of
// unchanged bytes uploads nothing.
package watermark

// Mark is one file's upload cursor.
type Mark struct {
	MachineID string
	Harness   string
	Root      string
	RelPath   string
	Size      int64
	MTimeUnix int64
	SHA256    string
	Offset    int64
}

// Store persists marks. Not implemented.
type Store interface {
	Get(key Mark) (Mark, bool, error)
	Put(mark Mark) error
}
