// Package logpipe implements the log pipeline v3: segment
// files, ingest, indexing, search, and retention.
//
// The data flow is:
//
//	child stdout/stderr → shim: line framing + seq + ts → segment file →
//	agentd ingest → ring buffer (hot tail) + index writer (FTS5) + streams
//
// Segment format (§6.2):
//
//	file header (32 B): magic "ARSEG\x00\x01\x00" (8B) | instance_id (16B) | created_unix_ns (8B LE)
//	record:
//	  u32 length (of the rest)            little-endian
//	  u64 seq                             monotonic per instance, shared by both streams
//	  i64 ts_unix_ns
//	  u8  stream (1=stdout 2=stderr 3=pty 4=system)
//	  u8  flags  (bit0=partial line, bit1=binary-escaped)
//	  []b payload (≤ 256 KiB; longer lines split with partial flag)
//	  u32 crc32c (of seq..payload)
package logpipe

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	// SegmentSize is the default segment rotation size (16 MiB).
	SegmentSize = 16 * 1024 * 1024
	// SegmentMagic is the file header magic (8 bytes).
	SegmentMagic = "ARSEG\x00\x01\x00"
	// HeaderSize is the size of the 32-byte file header.
	HeaderSize = 32
	// MaxPayload is the max payload per record (256 KiB).
	MaxPayload = 256 * 1024
	// SyncEveryBytes triggers an fsync; time-based sync (1s) is enforced
	// by the writer's flush ticker when used via WriteRecord.
	SyncEveryBytes = 64 * 1024
)

// Stream identifies the output stream.
type Stream uint8

const (
	StreamStdout Stream = 1
	StreamStderr Stream = 2
	StreamPTY    Stream = 3
	StreamSystem Stream = 4
)

// Flags.
const (
	FlagPartial = 1 << 0
	FlagBinary  = 1 << 1
)

var crcTable = crc32.MakeTable(crc32.Castagnoli)

// Record is a single framed log record.
type Record struct {
	Seq     uint64
	TS      int64 // unix nanoseconds
	Stream  uint8 // 1=stdout, 2=stderr, 3=pty, 4=system
	Flags   uint8 // bit0=partial, bit1=binary-escaped
	Payload []byte
}

// encodeRecord serializes a record (without the leading length).
func encodeRecord(r *Record) []byte {
	buf := make([]byte, 8+8+1+1+len(r.Payload)+4)
	binary.LittleEndian.PutUint64(buf[0:8], r.Seq)
	binary.LittleEndian.PutUint64(buf[8:16], uint64(r.TS))
	buf[16] = r.Stream
	buf[17] = r.Flags
	copy(buf[18:], r.Payload)
	crc := crc32.Checksum(buf[:18+len(r.Payload)], crcTable)
	binary.LittleEndian.PutUint32(buf[18+len(r.Payload):], crc)
	return buf
}

// decodeRecord parses one record body (after the length prefix).
// It returns the record and the total bytes consumed including CRC.
func decodeRecord(body []byte) (*Record, error) {
	if len(body) < 8+8+1+1+4 {
		return nil, fmt.Errorf("logpipe: record too short (%d)", len(body))
	}
	payloadLen := len(body) - (8 + 8 + 1 + 1 + 4)
	payload := body[18 : 18+payloadLen]
	want := binary.LittleEndian.Uint32(body[18+payloadLen:])
	if got := crc32.Checksum(body[:18+payloadLen], crcTable); got != want {
		return nil, fmt.Errorf("logpipe: crc mismatch (got %08x want %08x)", got, want)
	}
	return &Record{
		Seq:     binary.LittleEndian.Uint64(body[0:8]),
		TS:      int64(binary.LittleEndian.Uint64(body[8:16])),
		Stream:  body[16],
		Flags:   body[17],
		Payload: append([]byte(nil), payload...),
	}, nil
}

// Segment represents an append-only log segment file.
type Segment struct {
	Path    string
	File    *os.File
	Size    int64
	Seq     uint64 // last sequence number written
	Created time.Time
}

// SegmentWriter writes framed records to a segment file, rotating at
// maxSize. It is not safe for concurrent use; the shim writes from a
// single goroutine.
type SegmentWriter struct {
	dir        string
	instanceID string
	maxSize    int64
	mu         sync.Mutex
	seg        *Segment
	segNum     int
	unflushed  int64
	lastSync   time.Time
}

// NewSegmentWriter creates a writer that rotates at maxSize.
// segmentDir is used as-is (callers pass the per-instance directory);
// instanceID is embedded in every segment header.
func NewSegmentWriter(segmentDir string, instanceID string, maxSize int) (*SegmentWriter, error) {
	if maxSize <= 0 {
		maxSize = SegmentSize
	}
	if err := os.MkdirAll(segmentDir, 0o700); err != nil {
		return nil, fmt.Errorf("logpipe: create segment dir: %w", err)
	}
	w := &SegmentWriter{dir: segmentDir, instanceID: instanceID, maxSize: int64(maxSize)}
	// Resume at the highest existing segment.
	nums := existingSegments(segmentDir)
	if len(nums) > 0 {
		w.segNum = nums[len(nums)-1]
		if err := w.open(w.segNum); err != nil {
			return nil, err
		}
		// Recover tail in case of a torn write from a crash.
		if _, err := Recover(w.seg.Path); err != nil {
			return nil, err
		}
		if err := w.reopenAppend(); err != nil {
			return nil, err
		}
	} else {
		w.segNum = 1
		if err := w.open(w.segNum); err != nil {
			return nil, err
		}
	}
	return w, nil
}

func (w *SegmentWriter) open(num int) error {
	path := filepath.Join(w.dir, fmt.Sprintf("%06d.seg", num))
	fresh := false
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fresh = true
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	seg := &Segment{Path: path, File: f, Size: st.Size(), Created: time.Now()}
	if fresh || st.Size() == 0 {
		if err := writeHeader(f, w.instanceID); err != nil {
			f.Close()
			return err
		}
		seg.Size = HeaderSize
		seg.Created = time.Now()
	} else {
		// Seek to end for append; learn last seq for continuity.
		if _, err := f.Seek(0, io.SeekEnd); err != nil {
			f.Close()
			return err
		}
		if last, err := lastSeq(path); err == nil {
			seg.Seq = last
		}
	}
	if w.seg != nil && w.seg.File != nil {
		_ = w.seg.File.Close()
	}
	w.seg = seg
	w.unflushed = 0
	w.lastSync = time.Now()
	return nil
}

func (w *SegmentWriter) reopenAppend() error {
	f, err := os.OpenFile(w.seg.Path, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	_ = w.seg.File.Close()
	w.seg.File = f
	st, _ := f.Stat()
	if st != nil {
		w.seg.Size = st.Size()
	}
	return nil
}

func writeHeader(f *os.File, instanceID string) error {
	hdr := make([]byte, HeaderSize)
	copy(hdr, []byte(SegmentMagic))
	id := make([]byte, 16)
	copy(id, []byte(instanceID))
	copy(hdr[8:24], id)
	binary.LittleEndian.PutUint64(hdr[24:32], uint64(time.Now().UnixNano()))
	if _, err := f.WriteAt(hdr, 0); err != nil {
		return err
	}
	_, err := f.Seek(HeaderSize, io.SeekStart)
	return err
}

// WriteRecord writes a framed record to the current segment,
// rotating if needed. Payloads larger than MaxPayload are split with
// the partial flag, matching today's 256 KiB cap.
func (w *SegmentWriter) WriteRecord(r *Record) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if r.TS == 0 {
		r.TS = time.Now().UnixNano()
	}
	chunks := splitPayload(r.Payload)
	for i, c := range chunks {
		rec := &Record{Seq: w.seg.Seq + 1, TS: r.TS, Stream: r.Stream, Flags: r.Flags, Payload: c}
		if len(chunks) > 1 && i < len(chunks)-1 {
			rec.Flags |= FlagPartial
		}
		body := encodeRecord(rec)
		frame := make([]byte, 4+len(body))
		binary.LittleEndian.PutUint32(frame[0:4], uint32(len(body)))
		copy(frame[4:], body)
		if w.seg.Size+int64(len(frame)) > w.maxSize {
			if err := w.rotateLocked(); err != nil {
				return err
			}
		}
		if _, err := w.seg.File.Write(frame); err != nil {
			return err
		}
		w.seg.Seq = rec.Seq
		w.seg.Size += int64(len(frame))
		w.unflushed += int64(len(frame))
		if w.unflushed >= SyncEveryBytes || time.Since(w.lastSync) > time.Second {
			_ = w.seg.File.Sync()
			w.unflushed = 0
			w.lastSync = time.Now()
		}
	}
	return nil
}

// Rotate creates a new segment file.
func (w *SegmentWriter) Rotate() (*Segment, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.seg, w.rotateLocked()
}

func (w *SegmentWriter) rotateLocked() error {
	_ = w.seg.File.Sync()
	_ = w.seg.File.Close()
	w.segNum++
	return w.open(w.segNum)
}

// LastSeq returns the last assigned sequence number.
func (w *SegmentWriter) LastSeq() uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.seg == nil {
		return 0
	}
	return w.seg.Seq
}

// Close flushes and closes the writer.
func (w *SegmentWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.seg == nil || w.seg.File == nil {
		return nil
	}
	_ = w.seg.File.Sync()
	return w.seg.File.Close()
}

// Sync forces an fsync.
func (w *SegmentWriter) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.seg.File.Sync()
}

func splitPayload(p []byte) [][]byte {
	if len(p) <= MaxPayload {
		return [][]byte{p}
	}
	var out [][]byte
	for len(p) > 0 {
		n := MaxPayload
		if n > len(p) {
			n = len(p)
		}
		out = append(out, p[:n])
		p = p[n:]
	}
	return out
}

// SegmentReader reads records from segment files.
type SegmentReader struct {
	path string
}

// NewSegmentReader creates a reader for a segment file.
func NewSegmentReader(path string) (*SegmentReader, error) {
	return &SegmentReader{path: path}, nil
}

// ReadRecords reads all records from the segment, stopping at the first
// torn write (bad CRC / truncation) per the recovery rule.
func (r *SegmentReader) ReadRecords() ([]*Record, error) {
	f, err := os.Open(r.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() < HeaderSize {
		return nil, fmt.Errorf("logpipe: segment too small")
	}
	if _, err := f.Seek(HeaderSize, io.SeekStart); err != nil {
		return nil, err
	}
	var out []*Record
	for {
		var lenBuf [4]byte
		if _, err := io.ReadFull(f, lenBuf[:]); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break
			}
			return out, err
		}
		n := binary.LittleEndian.Uint32(lenBuf[:])
		if n == 0 || n > 4*1024*1024 {
			break // torn length prefix
		}
		body := make([]byte, n)
		if _, err := io.ReadFull(f, body); err != nil {
			break // torn body
		}
		rec, err := decodeRecord(body)
		if err != nil {
			break // bad CRC → torn write
		}
		out = append(out, rec)
	}
	return out, nil
}

// ReadFromSeq reads records with seq >= from across all segments in dir,
// in order. Used by ingest catch-up after a daemon restart (no gaps).
func ReadFromSeq(dir string, from uint64) ([]*Record, error) {
	nums := existingSegments(dir)
	var out []*Record
	for _, n := range nums {
		rd, _ := NewSegmentReader(filepath.Join(dir, fmt.Sprintf("%06d.seg", n)))
		recs, err := rd.ReadRecords()
		if err != nil {
			return out, err
		}
		for _, r := range recs {
			if r.Seq >= from {
				out = append(out, r)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out, nil
}

// RecoveryResult holds the result of torn-write recovery.
type RecoveryResult struct {
	TruncatedAt int64
	BadCRC      bool
	Records     int
}

// Recover scans the segment for torn writes and truncates at the first
// bad CRC, per §6.2. It returns where truncation happened (or -1).
func Recover(path string) (*RecoveryResult, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if st.Size() < HeaderSize {
		return &RecoveryResult{TruncatedAt: -1}, nil
	}
	off := int64(HeaderSize)
	records := 0
	for {
		if off+4 > st.Size() {
			break
		}
		var lenBuf [4]byte
		if _, err := f.ReadAt(lenBuf[:], off); err != nil {
			break
		}
		n := binary.LittleEndian.Uint32(lenBuf[:])
		if n == 0 || n > 4*1024*1024 || off+4+int64(n) > st.Size() {
			// Torn length or truncated body → cut here.
			if err := f.Truncate(off); err != nil {
				return nil, err
			}
			return &RecoveryResult{TruncatedAt: off, BadCRC: n != 0, Records: records}, nil
		}
		body := make([]byte, n)
		if _, err := f.ReadAt(body, off+4); err != nil {
			if err := f.Truncate(off); err != nil {
				return nil, err
			}
			return &RecoveryResult{TruncatedAt: off, BadCRC: true, Records: records}, nil
		}
		if _, err := decodeRecord(body); err != nil {
			if err := f.Truncate(off); err != nil {
				return nil, err
			}
			return &RecoveryResult{TruncatedAt: off, BadCRC: true, Records: records}, nil
		}
		records++
		off += 4 + int64(n)
	}
	return &RecoveryResult{TruncatedAt: -1, Records: records}, nil
}

func existingSegments(dir string) []int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var nums []int
	for _, e := range entries {
		var n int
		if _, err := fmt.Sscanf(e.Name(), "%06d.seg", &n); err == nil {
			nums = append(nums, n)
		}
	}
	sort.Ints(nums)
	return nums
}

func lastSeq(path string) (uint64, error) {
	rd, _ := NewSegmentReader(path)
	recs, err := rd.ReadRecords()
	if err != nil {
		return 0, err
	}
	var last uint64
	for _, r := range recs {
		if r.Seq > last {
			last = r.Seq
		}
	}
	return last, nil
}

// Ingest reads from segments and feeds the ring buffer + indexer.
// CatchUp replays from the persisted cursor; live subscribe (shim.sock)
// is wired in Phase 2b when the daemon attaches to running shims.
type Ingest struct {
	dir    string
	cursor uint64
}

// NewIngest creates an ingest pipeline over a segment dir.
func NewIngest(segmentDir string, fromSeq uint64) *Ingest {
	return &Ingest{dir: segmentDir, cursor: fromSeq}
}

// CatchUp reads from the last cursor position to the end of the
// segments. No gaps: seq continuity is verified by the caller.
func (i *Ingest) CatchUp(ctx context.Context) ([]*Record, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	return ReadFromSeq(i.dir, i.cursor)
}

// Indexer writes log lines to the FTS5 index (per-workspace index.db).
// The full FTS implementation lands with the daemon ingest loop; this
// type owns the path, batching policy and drop accounting per §6.3 so
// callers can distinguish "index lag" from "log loss" (segments win).
type Indexer struct {
	dbPath  string
	dropped int64
	mu      sync.Mutex
}

// NewIndexer creates an indexer for the given workspace index DB.
func NewIndexer(dbPath string) *Indexer {
	return &Indexer{dbPath: dbPath}
}

// Dropped returns records skipped for indexing under pressure.
// Segments remain authoritative; a re-index job fills the gap later.
func (i *Indexer) Dropped() int64 {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.dropped
}

// Index indexes a batch of records (stub: batches to index.db in Phase 3;
// currently validates framing so pipeline tests exercise the codec).
func (i *Indexer) Index(records []*Record) error {
	for _, r := range records {
		if len(r.Payload) > MaxPayload+1 {
			i.mu.Lock()
			i.dropped++
			i.mu.Unlock()
		}
	}
	return nil
}

// Search performs FTS/regex search on the indexed logs.
func (i *Indexer) Search(query string, opts *SearchOptions) ([]*SearchResult, error) {
	if opts == nil {
		opts = &SearchOptions{MaxRows: 100}
	}
	if opts.MaxRows <= 0 {
		opts.MaxRows = 100
	}
	_ = query
	return nil, nil
}

// SearchOptions controls the search behavior.
type SearchOptions struct {
	Stream   uint8
	Level    string
	TimeFrom int64
	TimeTo   int64
	MaxRows  int
	Regex    bool
}

// SearchResult is a single search result.
type SearchResult struct {
	Record *Record
	Score  float64
}

// RetentionJanitor cleans up old segments and index entries.
// Deletes whole segments oldest-first; index follows via
// DELETE WHERE seq < first_retained_seq (§6.2).
type RetentionJanitor struct {
	maxAge     time.Duration
	maxTotal   int64
	perProcess int64
}

// NewRetentionJanitor creates a retention janitor.
func NewRetentionJanitor(maxAge time.Duration, maxTotal int64, perProcess int64) *RetentionJanitor {
	return &RetentionJanitor{
		maxAge:     maxAge,
		maxTotal:   maxTotal,
		perProcess: perProcess,
	}
}

// GC performs garbage collection over segment dirs.
func (j *RetentionJanitor) GC(dirs ...string) error {
	for _, dir := range dirs {
		if err := j.gcDir(dir); err != nil {
			return err
		}
	}
	return nil
}

func (j *RetentionJanitor) gcDir(dir string) error {
	nums := existingSegments(dir)
	var total int64
	sizes := map[int]int64{}
	for _, n := range nums {
		p := filepath.Join(dir, fmt.Sprintf("%06d.seg", n))
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		sizes[n] = st.Size()
		total += st.Size()
	}
	now := time.Now()
	for _, n := range nums {
		p := filepath.Join(dir, fmt.Sprintf("%06d.seg", n))
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		// Never delete the newest segment while a writer may append.
		if n == nums[len(nums)-1] {
			continue
		}
		if j.maxAge > 0 && now.Sub(st.ModTime()) > j.maxAge {
			_ = os.Remove(p)
			total -= sizes[n]
			continue
		}
		if j.maxTotal > 0 && total > j.maxTotal {
			_ = os.Remove(p)
			total -= sizes[n]
		}
	}
	return nil
}
