package adapter

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// The in-process object reader. It reads loose objects and version 2
// pack indexes with their packs, including offset and ref deltas. The
// checkout is not trusted, so every size, offset, and delta chain is
// bounded, and anything unexpected is a miss rather than an error.
const (
	maxObjectBytes = 1 << 20
	maxDeltaDepth  = 64
	maxPackFiles   = 256
	maxWalkBytes   = 256 << 20
)

const (
	packCommit   = 1
	packOfsDelta = 6
	packRefDelta = 7
)

// objectStore finds commits under a repository's object directories
// and its alternates. Packs are opened on the first miss in loose
// objects and stay open until close.
type objectStore struct {
	dirs    []string
	hashLen int
	packs   []*packFile
	opened  bool
	// spent is the bytes inflated from packs so far. One walk stops
	// reading packs past maxWalkBytes.
	spent int64
}

func newObjectStore(common string, hashLen int) *objectStore {
	return &objectStore{dirs: objectDirs(common), hashLen: hashLen}
}

func (s *objectStore) close() {
	for _, p := range s.packs {
		p.close()
	}
	s.packs = nil
}

// commit returns the body of the commit named hash.
func (s *objectStore) commit(hash string) ([]byte, bool) {
	if data, ok := looseCommit(s.dirs, hash); ok {
		return data, true
	}
	typ, data, ok := s.packed(hash, 0)
	if !ok || typ != packCommit {
		return nil, false
	}
	return data, true
}

func (s *objectStore) packed(hash string, depth int) (int, []byte, bool) {
	name, err := hex.DecodeString(hash)
	if err != nil || len(name) != s.hashLen {
		return 0, nil, false
	}
	s.openPacks()
	for _, p := range s.packs {
		off, ok := p.find(name)
		if !ok {
			continue
		}
		return p.object(off, depth)
	}
	return 0, nil, false
}

// object resolves a ref delta's base, which may sit in another pack or
// in a loose object.
func (s *objectStore) object(hash string, depth int) (int, []byte, bool) {
	if raw, ok := s.loose(hash); ok {
		typ, data, ok := splitGitObject(raw)
		if !ok {
			return 0, nil, false
		}
		code, ok := looseType(typ)
		return code, data, ok
	}
	return s.packed(hash, depth)
}

func (s *objectStore) loose(hash string) ([]byte, bool) {
	for _, dir := range s.dirs {
		if b, ok := readLoose(filepath.Join(dir, hash[:2], hash[2:])); ok {
			return b, true
		}
	}
	return nil, false
}

func looseType(typ string) (int, bool) {
	switch typ {
	case "commit":
		return 1, true
	case "tree":
		return 2, true
	case "blob":
		return 3, true
	case "tag":
		return 4, true
	}
	return 0, false
}

func (s *objectStore) openPacks() {
	if s.opened {
		return
	}
	s.opened = true
	for _, dir := range s.dirs {
		idxs, _ := filepath.Glob(filepath.Join(dir, "pack", "pack-*.idx"))
		for _, idx := range idxs {
			if len(s.packs) >= maxPackFiles {
				return
			}
			p, ok := openPack(idx, strings.TrimSuffix(idx, ".idx")+".pack", s)
			if ok {
				s.packs = append(s.packs, p)
			}
		}
	}
}

// packFile is one version 2 index and its pack. The index is read
// with ReadAt so a large repository does not load it whole.
type packFile struct {
	idx      *os.File
	pack     *os.File
	packSize int64
	count    int64
	fanout   [256]uint32
	store    *objectStore
}

const idxHeader = 8 + 256*4

func openPack(idxPath, packPath string, store *objectStore) (*packFile, bool) {
	idx, err := openRegular(idxPath)
	if err != nil {
		return nil, false
	}
	pack, err := openRegular(packPath)
	if err != nil {
		idx.Close()
		return nil, false
	}
	p := &packFile{idx: idx, pack: pack, store: store}
	if !p.load() {
		p.close()
		return nil, false
	}
	return p, true
}

func (p *packFile) load() bool {
	ist, err := p.idx.Stat()
	if err != nil {
		return false
	}
	pst, err := p.pack.Stat()
	if err != nil {
		return false
	}
	p.packSize = pst.Size()
	head := make([]byte, idxHeader)
	if _, err := p.idx.ReadAt(head, 0); err != nil {
		return false
	}
	if !bytes.Equal(head[:4], []byte{0xff, 't', 'O', 'c'}) || binary.BigEndian.Uint32(head[4:8]) != 2 {
		return false
	}
	prev := uint32(0)
	for i := range p.fanout {
		v := binary.BigEndian.Uint32(head[8+i*4:])
		if v < prev {
			return false
		}
		p.fanout[i] = v
		prev = v
	}
	p.count = int64(p.fanout[255])
	h := int64(p.store.hashLen)
	// names, crc32s, and 4-byte offsets, then two trailing checksums.
	return ist.Size() >= idxHeader+p.count*(h+8)+2*h
}

func (p *packFile) close() {
	p.idx.Close()
	p.pack.Close()
}

// find returns the pack offset of name.
func (p *packFile) find(name []byte) (int64, bool) {
	h := int64(p.store.hashLen)
	lo := int64(0)
	if name[0] > 0 {
		lo = int64(p.fanout[name[0]-1])
	}
	hi := int64(p.fanout[name[0]])
	buf := make([]byte, h)
	for lo < hi {
		mid := lo + (hi-lo)/2
		if _, err := p.idx.ReadAt(buf, idxHeader+mid*h); err != nil {
			return 0, false
		}
		switch bytes.Compare(buf, name) {
		case 0:
			return p.offset(mid)
		case -1:
			lo = mid + 1
		default:
			hi = mid
		}
	}
	return 0, false
}

func (p *packFile) offset(i int64) (int64, bool) {
	h := int64(p.store.hashLen)
	small := idxHeader + p.count*(h+4)
	var b [8]byte
	if _, err := p.idx.ReadAt(b[:4], small+i*4); err != nil {
		return 0, false
	}
	v := binary.BigEndian.Uint32(b[:4])
	if v&0x80000000 == 0 {
		return int64(v), true
	}
	large := small + p.count*4 + int64(v&0x7fffffff)*8
	if _, err := p.idx.ReadAt(b[:], large); err != nil {
		return 0, false
	}
	off := binary.BigEndian.Uint64(b[:])
	if off > uint64(p.packSize) {
		return 0, false
	}
	return int64(off), true
}

// object reads the entry at off and resolves any delta chain under it.
func (p *packFile) object(off int64, depth int) (int, []byte, bool) {
	if depth > maxDeltaDepth || off < 12 || off >= p.packSize {
		return 0, nil, false
	}
	r := bufio.NewReader(io.NewSectionReader(p.pack, off, p.packSize-off))
	c, err := r.ReadByte()
	if err != nil {
		return 0, nil, false
	}
	typ := int(c>>4) & 7
	size := int64(c & 0x0f)
	shift := uint(4)
	for c&0x80 != 0 {
		if c, err = r.ReadByte(); err != nil || shift > 56 {
			return 0, nil, false
		}
		size |= int64(c&0x7f) << shift
		shift += 7
	}
	if size > maxObjectBytes {
		return 0, nil, false
	}
	switch typ {
	case 1, 2, 3, 4:
		data, ok := p.store.inflate(r, size)
		return typ, data, ok
	case packOfsDelta:
		rel, ok := ofsDeltaDistance(r)
		if !ok || rel <= 0 || rel >= off {
			return 0, nil, false
		}
		delta, ok := p.store.inflate(r, size)
		if !ok {
			return 0, nil, false
		}
		baseTyp, base, ok := p.object(off-rel, depth+1)
		if !ok {
			return 0, nil, false
		}
		data, ok := applyDelta(base, delta)
		return baseTyp, data, ok
	case packRefDelta:
		name := make([]byte, p.store.hashLen)
		if _, err := io.ReadFull(r, name); err != nil {
			return 0, nil, false
		}
		delta, ok := p.store.inflate(r, size)
		if !ok {
			return 0, nil, false
		}
		baseTyp, base, ok := p.store.object(hex.EncodeToString(name), depth+1)
		if !ok {
			return 0, nil, false
		}
		data, ok := applyDelta(base, delta)
		return baseTyp, data, ok
	}
	return 0, nil, false
}

// ofsDeltaDistance reads git's offset encoding, where each
// continuation byte adds one before shifting.
func ofsDeltaDistance(r io.ByteReader) (int64, bool) {
	c, err := r.ReadByte()
	if err != nil {
		return 0, false
	}
	n := int64(c & 0x7f)
	for i := 0; c&0x80 != 0; i++ {
		if c, err = r.ReadByte(); err != nil || i > 7 {
			return 0, false
		}
		n = ((n + 1) << 7) | int64(c&0x7f)
	}
	return n, true
}

func inflate(r io.Reader, size int64) ([]byte, bool) {
	zr, err := zlib.NewReader(r)
	if err != nil {
		return nil, false
	}
	defer zr.Close()
	data, err := io.ReadAll(io.LimitReader(zr, size+1))
	if err != nil || int64(len(data)) != size {
		return nil, false
	}
	return data, true
}

// applyDelta builds a target from base and a git delta: the two sizes,
// then copy and insert instructions.
func applyDelta(base, delta []byte) ([]byte, bool) {
	src, delta, ok := deltaSize(delta)
	if !ok || src != int64(len(base)) {
		return nil, false
	}
	dst, delta, ok := deltaSize(delta)
	if !ok || dst < 0 || dst > maxObjectBytes {
		return nil, false
	}
	out := make([]byte, 0, dst)
	for len(delta) > 0 {
		op := delta[0]
		delta = delta[1:]
		switch {
		case op&0x80 != 0:
			var off, n int64
			for i := uint(0); i < 4; i++ {
				if op&(1<<i) == 0 {
					continue
				}
				if len(delta) == 0 {
					return nil, false
				}
				off |= int64(delta[0]) << (8 * i)
				delta = delta[1:]
			}
			for i := uint(0); i < 3; i++ {
				if op&(0x10<<i) == 0 {
					continue
				}
				if len(delta) == 0 {
					return nil, false
				}
				n |= int64(delta[0]) << (8 * i)
				delta = delta[1:]
			}
			if n == 0 {
				n = 0x10000
			}
			if off+n > int64(len(base)) || int64(len(out))+n > dst {
				return nil, false
			}
			out = append(out, base[off:off+n]...)
		case op != 0:
			n := int(op)
			if n > len(delta) || int64(len(out)+n) > dst {
				return nil, false
			}
			out = append(out, delta[:n]...)
			delta = delta[n:]
		default:
			return nil, false
		}
	}
	if int64(len(out)) != dst {
		return nil, false
	}
	return out, true
}

func deltaSize(b []byte) (int64, []byte, bool) {
	var n int64
	for i := uint(0); i < 64; i += 7 {
		if len(b) == 0 {
			return 0, nil, false
		}
		c := b[0]
		b = b[1:]
		n |= int64(c&0x7f) << i
		if c&0x80 == 0 {
			return n, b, true
		}
	}
	return 0, nil, false
}

// openRegular opens path only when it is a regular file. A FIFO or a
// device planted in a checkout would otherwise block or never end.
func openRegular(path string) (*os.File, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, os.ErrInvalid
	}
	return os.Open(path)
}

// inflate reads one zlib stream of size bytes and charges it to the
// walk's budget.
func (s *objectStore) inflate(r io.Reader, size int64) ([]byte, bool) {
	if s.spent+size > maxWalkBytes {
		return nil, false
	}
	s.spent += size
	return inflate(r, size)
}
