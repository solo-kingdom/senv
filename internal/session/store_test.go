package session

import (
	"runtime"
	"testing"
)

func TestDefaultSessionStorePlatformSelection(t *testing.T) {
	store := defaultSessionStore()
	if runtime.GOOS == "darwin" {
		if _, ok := store.(keychainStore); !ok {
			t.Fatalf("darwin default store = %T, want keychainStore", store)
		}
		return
	}
	if _, ok := store.(tmpfsStore); !ok {
		t.Fatalf("%s default store = %T, want tmpfsStore", runtime.GOOS, store)
	}
}
