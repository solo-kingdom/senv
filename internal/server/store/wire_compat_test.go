package store

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestEntryWireBackwardCompatibility(t *testing.T) {
	// Old clients send the pre-descriptor wire shape. New server code must
	// continue to accept it and fill only server-side fields after writing.
	const legacy = `{
		"kind":"env","grp":"default","key":"A",
		"ciphertext":"Y2E=","base_revision":1,"revision":1,"deleted":false
	}`
	var got Entry
	if err := json.Unmarshal([]byte(legacy), &got); err != nil {
		t.Fatalf("unmarshal legacy entry: %v", err)
	}
	if got.Kind != "env" || got.Grp != "default" || got.Key != "A" ||
		string(got.Ciphertext) != "ca" || got.BaseRevision != 1 ||
		got.Revision != 1 || got.Deleted || !got.UpdatedAt.IsZero() {
		t.Fatalf("legacy entry decoded to %+v", got)
	}

	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal entry: %v", err)
	}
	var roundTrip Entry
	if err := json.Unmarshal(encoded, &roundTrip); err != nil {
		t.Fatalf("unmarshal entry: %v", err)
	}
	if !reflect.DeepEqual(roundTrip, got) || !bytes.Equal(roundTrip.Ciphertext, got.Ciphertext) {
		t.Fatalf("wire round trip changed entry: %+v != %+v", roundTrip, got)
	}
}
