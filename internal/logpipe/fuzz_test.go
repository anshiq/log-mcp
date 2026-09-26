package logpipe

import (
	"testing"
)

// FuzzSegmentRoundtrip: decode(encode(r)) == r, and the decoder never
// panics on arbitrary bytes (torn-write recovery depends on it).
func FuzzSegmentRoundtrip(f *testing.F) {
	f.Add(uint64(1), int64(2), uint8(1), uint8(0), []byte("hello"))
	f.Fuzz(func(t *testing.T, seq uint64, ts int64, stream, flags uint8, payload []byte) {
		if len(payload) > MaxPayload {
			t.Skip()
		}
		r := &Record{Seq: seq, TS: ts, Stream: stream % 5, Flags: flags % 4, Payload: payload}
		body := encodeRecord(r)
		got, err := decodeRecord(body)
		if err != nil {
			t.Fatalf("roundtrip: %v", err)
		}
		if got.Seq != r.Seq || got.TS != r.TS || got.Stream != r.Stream || got.Flags != r.Flags {
			t.Fatalf("mismatch: %+v vs %+v", got, r)
		}
		if string(got.Payload) != string(r.Payload) {
			t.Fatal("payload mismatch")
		}
		// Arbitrary bytes must error, never panic.
		if len(body) > 4 {
			if _, err := decodeRecord(body[:len(body)-1]); err == nil {
				t.Fatal("truncated body should fail CRC")
			}
		}
	})
}
