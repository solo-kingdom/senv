package tests

import (
	"os"
	"testing"

	"github.com/wii/senv/internal/crypto"
)

// TestMain lowers the vault-creation PBKDF2 cost for the suite. Creating a
// vault otherwise costs a full production derivation (~68ms, ~780ms under
// -race) and these suites create many; the unified test cost keeps the same
// metadata validation path (LegacyIterations is the supported floor).
func TestMain(m *testing.M) {
	crypto.LowerIterationsForTesting()
	os.Exit(m.Run())
}
