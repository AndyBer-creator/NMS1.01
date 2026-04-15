package http

import (
	"testing"
	"time"
)

func TestIncidentCursorRoundTrip(t *testing.T) {
	at := time.Now().UTC().Truncate(time.Nanosecond)
	id := int64(12345)
	cur := encodeIncidentCursor(at, id)
	gotAt, gotID, err := decodeIncidentCursor(cur)
	if err != nil {
		t.Fatalf("decodeIncidentCursor: %v", err)
	}
	if gotID != id {
		t.Fatalf("id mismatch: got %d want %d", gotID, id)
	}
	if !gotAt.Equal(at) {
		t.Fatalf("time mismatch: got %s want %s", gotAt, at)
	}
}

func TestIncidentCursorInvalid(t *testing.T) {
	if _, _, err := decodeIncidentCursor("%%%"); err == nil {
		t.Fatal("expected decode error for malformed cursor")
	}
}
