package normalize

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"strings"
)

// Private, temporary row spool. Length-prefixed strings avoid escaping and
// parsing EventJSON a second time while keeping only one row in memory.
func writeSpoolRow(w *bufio.Writer, row ParquetRow) error {
	var header [5]byte
	binary.LittleEndian.PutUint32(header[:4], uint32(row.SchemaVersion))
	if row.ContentText != nil {
		header[4] |= 1
	}
	if row.ProjectID != nil {
		header[4] |= 2
	}
	if _, err := w.Write(header[:]); err != nil {
		return err
	}
	text, project := "", ""
	if row.ContentText != nil {
		text = *row.ContentText
	}
	if row.ProjectID != nil {
		project = *row.ProjectID
	}
	for _, s := range [...]string{row.EventID, row.SessionID, row.Harness, row.RecordedAt, row.IngestedAt, row.EventType, row.Actor, row.CWDHash, row.RawType, row.EventJSON, text, project} {
		var size [binary.MaxVarintLen64]byte
		n := binary.PutUvarint(size[:], uint64(len(s)))
		if _, err := w.Write(size[:n]); err != nil {
			return err
		}
		if _, err := w.WriteString(s); err != nil {
			return err
		}
	}
	return nil
}

func readSpoolRow(r *bufio.Reader, fileBytes int64) (ParquetRow, error) {
	var header [5]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return ParquetRow{}, err
	}
	var fields [12]string
	for i := range fields {
		n, err := binary.ReadUvarint(r)
		if err != nil {
			return ParquetRow{}, err
		}
		if n > uint64(fileBytes) || n > uint64(int(^uint(0)>>1)) {
			return ParquetRow{}, fmt.Errorf("normalize: corrupt private row spool")
		}
		var b strings.Builder
		b.Grow(int(n))
		for n > 0 {
			count := int(min(n, uint64(r.Size())))
			chunk, err := r.Peek(count)
			if err != nil {
				return ParquetRow{}, err
			}
			b.Write(chunk)
			_, _ = r.Discard(count)
			n -= uint64(count)
		}
		fields[i] = b.String()
	}
	row := ParquetRow{SchemaVersion: int32(binary.LittleEndian.Uint32(header[:4])), EventID: fields[0], SessionID: fields[1], Harness: fields[2], RecordedAt: fields[3], IngestedAt: fields[4], EventType: fields[5], Actor: fields[6], CWDHash: fields[7], RawType: fields[8], EventJSON: fields[9]}
	if header[4]&1 != 0 {
		row.ContentText = &fields[10]
	}
	if header[4]&2 != 0 {
		row.ProjectID = &fields[11]
	}
	return row, nil
}
