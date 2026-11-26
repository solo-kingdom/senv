package handler

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wii/senv/internal/server/store"
)

func TestHandlerGzipResponseAndSmallSkip(t *testing.T) {
	st := newFakeStore()
	_, token := st.addClientToken("alice", "laptop", "active")
	s := New(st)

	if rec := doRequest(t, s, "PUT", "/v1/vaults/main/metadata", token, map[string]any{
		"blob": []byte("meta"),
	}); rec.Code != http.StatusOK {
		t.Fatalf("put metadata = %d: %s", rec.Code, rec.Body.String())
	}

	// Small /healthz should not be gzipped even with Accept-Encoding: gzip.
	reqSmall := httptest.NewRequest("GET", "/healthz", nil)
	reqSmall.Header.Set("Accept-Encoding", "gzip")
	recSmall := httptest.NewRecorder()
	s.ServeHTTP(recSmall, reqSmall)
	if recSmall.Code != http.StatusOK {
		t.Fatalf("healthz = %d", recSmall.Code)
	}
	if ce := recSmall.Header().Get("Content-Encoding"); ce != "" {
		t.Fatalf("small response Content-Encoding = %q, want empty", ce)
	}

	// Push an entry with > 512 bytes so GET /entries triggers gzip.
	largeCiphertext := bytes.Repeat([]byte("a"), 2048)
	if rec := doRequest(t, s, "POST", "/v1/vaults/main/entries", token, map[string]any{
		"entries": []map[string]any{{
			"kind":       "env",
			"grp":        "default",
			"key":        "LARGE",
			"ciphertext": largeCiphertext,
			"revision":   0,
		}},
	}); rec.Code != http.StatusOK {
		t.Fatalf("push = %d: %s", rec.Code, rec.Body.String())
	}

	reqGzip := httptest.NewRequest("GET", "/v1/vaults/main/entries?since=0", nil)
	reqGzip.Header.Set("Authorization", "Bearer "+token)
	reqGzip.Header.Set("Accept-Encoding", "gzip")
	recGzip := httptest.NewRecorder()
	s.ServeHTTP(recGzip, reqGzip)
	if recGzip.Code != http.StatusOK {
		t.Fatalf("pull gzip = %d: %s", recGzip.Code, recGzip.Body.String())
	}
	if ce := recGzip.Header().Get("Content-Encoding"); ce != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", ce)
	}
	if !strings.Contains(recGzip.Header().Get("Vary"), "Accept-Encoding") {
		t.Fatalf("Vary = %q, want Accept-Encoding", recGzip.Header().Get("Vary"))
	}

	zr, err := gzip.NewReader(bytes.NewReader(recGzip.Body.Bytes()))
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	decoded, err := io.ReadAll(zr)
	_ = zr.Close()
	if err != nil {
		t.Fatalf("read gzip body: %v", err)
	}
	var pullResp struct {
		Entries []store.Entry `json:"entries"`
	}
	if err := json.Unmarshal(decoded, &pullResp); err != nil {
		t.Fatalf("unmarshal decoded: %v", err)
	}
	if len(pullResp.Entries) != 1 || !bytes.Equal(pullResp.Entries[0].Ciphertext, largeCiphertext) {
		t.Fatalf("unexpected decoded entries: %+v", pullResp.Entries)
	}
}

func TestHandlerPullPaginationAndMetadataHash(t *testing.T) {
	st := newFakeStore()
	_, token := st.addClientToken("alice", "laptop", "active")
	s := New(st)

	metaBlob := []byte("meta-blob-v1")
	sum := sha256.Sum256(metaBlob)
	wantHash := hex.EncodeToString(sum[:])
	if rec := doRequest(t, s, "PUT", "/v1/vaults/main/metadata", token, map[string]any{
		"blob": metaBlob,
	}); rec.Code != http.StatusOK {
		t.Fatalf("put metadata = %d", rec.Code)
	}

	entries := []map[string]any{
		{"kind": "env", "grp": "default", "key": "K1", "ciphertext": []byte("v1"), "revision": 0},
		{"kind": "env", "grp": "default", "key": "K2", "ciphertext": []byte("v2"), "revision": 0, "deleted": true},
		{"kind": "env", "grp": "default", "key": "K3", "ciphertext": []byte("v3"), "revision": 0},
		{"kind": "env", "grp": "default", "key": "K4", "ciphertext": []byte("v4"), "revision": 0},
	}
	if rec := doRequest(t, s, "POST", "/v1/vaults/main/entries", token, map[string]any{
		"entries": entries,
	}); rec.Code != http.StatusOK {
		t.Fatalf("push = %d: %s", rec.Code, rec.Body.String())
	}

	// Page 1 with since=0, limit=2, include_deleted=0 -> should skip K2 (deleted) and return K1, K3 with has_more=true.
	rec1 := doRequest(t, s, "GET", "/v1/vaults/main/entries?since=0&limit=2&include_deleted=0", token, nil)
	if rec1.Code != http.StatusOK {
		t.Fatalf("page 1 = %d: %s", rec1.Code, rec1.Body.String())
	}
	var p1 struct {
		Entries      []store.Entry `json:"entries"`
		Revision     int64         `json:"revision"`
		HasMore      bool          `json:"has_more"`
		NextSince    int64         `json:"next_since"`
		MetadataHash string        `json:"metadata_hash"`
	}
	if err := json.Unmarshal(rec1.Body.Bytes(), &p1); err != nil {
		t.Fatalf("unmarshal p1: %v", err)
	}
	if len(p1.Entries) != 2 || !p1.HasMore || p1.NextSince == 0 {
		t.Fatalf("unexpected page 1: len=%d has_more=%v next_since=%d", len(p1.Entries), p1.HasMore, p1.NextSince)
	}
	if p1.MetadataHash != wantHash {
		t.Fatalf("metadata_hash = %q, want %q", p1.MetadataHash, wantHash)
	}
	for _, e := range p1.Entries {
		if e.Deleted {
			t.Fatalf("include_deleted=0 returned deleted entry: %+v", e)
		}
	}

	// Page 2 from NextSince -> returns K4 with has_more=false.
	rec2 := doRequest(t, s, "GET", "/v1/vaults/main/entries?since="+json.Number("0").String(), token, nil)
	_ = rec2
	url2 := "/v1/vaults/main/entries?since=" + strconvFormatInt(p1.NextSince) + "&limit=2&include_deleted=0"
	rec2 = doRequest(t, s, "GET", url2, token, nil)
	if rec2.Code != http.StatusOK {
		t.Fatalf("page 2 = %d: %s", rec2.Code, rec2.Body.String())
	}
	var p2 struct {
		Entries []store.Entry `json:"entries"`
		HasMore bool          `json:"has_more"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &p2); err != nil {
		t.Fatalf("unmarshal p2: %v", err)
	}
	if len(p2.Entries) != 1 || p2.Entries[0].Key != "K4" || p2.HasMore {
		t.Fatalf("unexpected page 2: %+v", p2)
	}
}

func TestHandlerLookupEndpointAndConflicts(t *testing.T) {
	st := newFakeStore()
	_, token := st.addClientToken("alice", "laptop", "active")
	s := New(st)

	if rec := doRequest(t, s, "PUT", "/v1/vaults/main/metadata", token, map[string]any{
		"blob": []byte("meta"),
	}); rec.Code != http.StatusOK {
		t.Fatalf("put metadata = %d", rec.Code)
	}

	cipher := []byte("secret-payload-xyz")
	cipherSum := sha256.Sum256(cipher)
	wantCipherHash := hex.EncodeToString(cipherSum[:])

	if rec := doRequest(t, s, "POST", "/v1/vaults/main/entries", token, map[string]any{
		"entries": []map[string]any{{
			"kind": "env", "grp": "default", "key": "API_KEY", "ciphertext": cipher, "revision": 0,
		}},
	}); rec.Code != http.StatusOK {
		t.Fatalf("push = %d", rec.Code)
	}

	// Lookup with meta_only=true returns Size and CiphertextSHA256 without Ciphertext.
	recMeta := doRequest(t, s, "POST", "/v1/vaults/main/entries/lookup", token, map[string]any{
		"keys":      []store.EntryKey{{Kind: "env", Grp: "default", Key: "API_KEY"}},
		"meta_only": true,
	})
	if recMeta.Code != http.StatusOK {
		t.Fatalf("lookup meta_only = %d: %s", recMeta.Code, recMeta.Body.String())
	}
	var metaResp struct {
		Entries []store.Entry `json:"entries"`
	}
	if err := json.Unmarshal(recMeta.Body.Bytes(), &metaResp); err != nil {
		t.Fatalf("unmarshal lookup meta: %v", err)
	}
	if len(metaResp.Entries) != 1 || len(metaResp.Entries[0].Ciphertext) != 0 ||
		metaResp.Entries[0].Size != int64(len(cipher)) || metaResp.Entries[0].CiphertextSHA256 != wantCipherHash {
		t.Fatalf("unexpected lookup meta_only entry: %+v", metaResp.Entries)
	}

	// Lookup with meta_only=false returns full Ciphertext.
	recFull := doRequest(t, s, "POST", "/v1/vaults/main/entries/lookup", token, map[string]any{
		"keys":      []store.EntryKey{{Kind: "env", Grp: "default", Key: "API_KEY"}},
		"meta_only": false,
	})
	if recFull.Code != http.StatusOK {
		t.Fatalf("lookup full = %d", recFull.Code)
	}
	var fullResp struct {
		Entries []store.Entry `json:"entries"`
	}
	if err := json.Unmarshal(recFull.Body.Bytes(), &fullResp); err != nil {
		t.Fatalf("unmarshal lookup full: %v", err)
	}
	if len(fullResp.Entries) != 1 || !bytes.Equal(fullResp.Entries[0].Ciphertext, cipher) {
		t.Fatalf("unexpected lookup full entry: %+v", fullResp.Entries)
	}

	// Push conflict at stale revision=0 returns CiphertextSHA256 in conflict.
	recConflict := doRequest(t, s, "POST", "/v1/vaults/main/entries", token, map[string]any{
		"entries": []map[string]any{{
			"kind": "env", "grp": "default", "key": "API_KEY", "ciphertext": []byte("other"), "revision": 0,
		}},
	})
	if recConflict.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d, want 409", recConflict.Code)
	}
	var conflictResp struct {
		Conflicts []store.Conflict `json:"conflicts"`
	}
	if err := json.Unmarshal(recConflict.Body.Bytes(), &conflictResp); err != nil {
		t.Fatalf("unmarshal conflict: %v", err)
	}
	if len(conflictResp.Conflicts) != 1 || conflictResp.Conflicts[0].CiphertextSHA256 != wantCipherHash {
		t.Fatalf("conflict CiphertextSHA256 = %+v, want %q", conflictResp.Conflicts, wantHashOr(wantCipherHash))
	}
}

func strconvFormatInt(v int64) string {
	return json.Number(string(appendInt(nil, v))).String()
}

func appendInt(dst []byte, v int64) []byte {
	if v == 0 {
		return append(dst, '0')
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return append(dst, buf[i:]...)
}

func wantHashOr(s string) string { return s }
