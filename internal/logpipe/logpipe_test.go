package logpipe

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSegmentWriteRead(t *testing.T) {
	dir := t.TempDir()
	w, err := NewSegmentWriter(filepath.Join(dir, "segs"), "inst_test_001", 1024*1024)
	if err != nil {
		t.Fatalf("NewSegmentWriter: %v", err)
	}
	defer w.Close()
	for i := 0; i < 100; i++ {
		if err := w.WriteRecord(&Record{Stream: uint8(StreamStdout), Payload: []byte("hello world")}); err != nil {
			t.Fatalf("WriteRecord: %v", err)
		}
	}
	if err := w.Sync(); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	recs, err := ReadFromSeq(filepath.Join(dir, "segs"), 1)
	if err != nil {
		t.Fatalf("ReadFromSeq: %v", err)
	}
	if len(recs) != 100 {
		t.Fatalf("got %d records, want 100", len(recs))
	}
	for i, r := range recs {
		if r.Seq != uint64(i+1) {
			t.Fatalf("record %d seq=%d", i, r.Seq)
		}
		if !bytes.Equal(r.Payload, []byte("hello world")) {
			t.Fatalf("record %d payload=%q", i, r.Payload)
		}
	}
}

func TestSegmentRotation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "segs")
	w, err := NewSegmentWriter(dir, "inst_rot", 2048)
	if err != nil {
		t.Fatalf("NewSegmentWriter: %v", err)
	}
	defer w.Close()
	for i := 0; i < 200; i++ {
		_ = w.WriteRecord(&Record{Stream: 1, Payload: bytes.Repeat([]byte("x"), 100)})
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) < 2 {
		t.Fatalf("expected rotation, got %d segments", len(entries))
	}
}

func TestTornWriteRecovery(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "segs")
	w, err := NewSegmentWriter(dir, "inst_torn", 1024*1024)
	if err != nil {
		t.Fatalf("NewSegmentWriter: %v", err)
	}
	for i := 0; i < 10; i++ {
		_ = w.WriteRecord(&Record{Stream: 1, Payload: []byte("good")})
	}
	_ = w.Sync()
	path := w.seg.Path
	_ = w.Close()
	// Append garbage to simulate a torn write.
	f, _ := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o600)
	_, _ = f.Write([]byte{0xff, 0xff, 0x00, 0x01, 0x02})
	f.Close()
	res, err := Recover(path)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if res.Records != 10 {
		t.Fatalf("records=%d want 10", res.Records)
	}
	rd, _ := NewSegmentReader(path)
	recs, _ := rd.ReadRecords()
	if len(recs) != 10 {
		t.Fatalf("after recovery got %d records, want 10", len(recs))
	}
}

func TestCatchUpNoGaps(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "segs")
	w, _ := NewSegmentWriter(dir, "inst_gap", 1024*1024)
	for i := 0; i < 50; i++ {
		_ = w.WriteRecord(&Record{Stream: 2, Payload: []byte("line")})
	}
	_ = w.Close()
	ing := NewIngest(dir, 25)
	recs, err := ing.CatchUp(context.Background())
	if err != nil {
		t.Fatalf("CatchUp: %v", err)
	}
	if len(recs) != 26 { // 25..50 inclusive
		t.Fatalf("got %d want 26", len(recs))
	}
	for i, r := range recs {
		if r.Seq != uint64(25+i) {
			t.Fatalf("gap at %d: seq=%d", i, r.Seq)
		}
	}
}

func TestLargeLineSplit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "segs")
	w, _ := NewSegmentWriter(dir, "inst_big", 8*1024*1024)
	defer w.Close()
	big := bytes.Repeat([]byte("a"), MaxPayload+100)
	if err := w.WriteRecord(&Record{Stream: 1, Payload: big}); err != nil {
		t.Fatalf("WriteRecord: %v", err)
	}
	_ = w.Sync()
	recs, _ := ReadFromSeq(dir, 1)
	if len(recs) != 2 {
		t.Fatalf("expected 2 split records, got %d", len(recs))
	}
	if recs[0].Flags&FlagPartial == 0 {
		t.Fatal("first chunk should carry partial flag")
	}
	var joined []byte
	for _, r := range recs {
		joined = append(joined, r.Payload...)
	}
	if !bytes.Equal(joined, big) {
		t.Fatal("split payload does not rejoin")
	}
}
