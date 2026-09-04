package session

import (
	"sync"
	"testing"
)

func TestFallbackConcurrentStartsLeaveVerifiableCache(t *testing.T) {
	isolateSessionCache(t)
	fallbackRoot := t.TempDir()
	t.Setenv("TMPDIR", fallbackRoot)
	t.Setenv("XDG_RUNTIME_DIR", "")
	setRuntimeProbe(t, runtimeFilesystemMemory, nil)
	configPath, dataPath := setupProject(t, "correct-secret")
	timeout, err := ParseTimeout("never")
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- sessionManagerForTest(t, configPath, dataPath).StartSession("correct-secret", timeout)
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent StartSession: %v", err)
		}
	}
	if _, err := sessionManagerForTest(t, configPath, dataPath).GetCachedKey(); err != nil {
		t.Fatalf("cache after concurrent fallback starts is not immediately valid: %v", err)
	}
}
