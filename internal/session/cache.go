package session

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// getCachePath returns the path to the session cache file
func getCachePath() string {
	// Prefer XDG_RUNTIME_DIR
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		cacheDir := filepath.Join(runtimeDir, "senv")
		os.MkdirAll(cacheDir, 0700)
		return filepath.Join(cacheDir, fmt.Sprintf("session-%d", os.Getuid()))
	}

	// Fallback to /tmp
	return filepath.Join("/tmp", fmt.Sprintf("senv-session-%d", os.Getuid()))
}

// generateSessionID generates a unique session ID
func generateSessionID() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return fmt.Sprintf("sess-%s", hex.EncodeToString(bytes)[:16])
}

// hashDataPath creates a hash of the data path for validation
func hashDataPath(dataPath string) string {
	hash := sha256.Sum256([]byte(dataPath))
	return hex.EncodeToString(hash[:16])
}

// saveCache saves the session cache to disk
func saveCache(cache *SessionCache) error {
	cachePath := getCachePath()

	// Ensure parent directory exists
	cacheDir := filepath.Dir(cachePath)
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return fmt.Errorf("failed to create cache directory: %w", err)
	}

	// Marshal cache to JSON
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal cache: %w", err)
	}

	// Write to file with secure permissions
	if err := os.WriteFile(cachePath, data, 0600); err != nil {
		return fmt.Errorf("failed to write cache file: %w", err)
	}

	return nil
}

// loadCache loads the session cache from disk
func loadCache() (*SessionCache, error) {
	cachePath := getCachePath()

	data, err := os.ReadFile(cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No cache file exists
		}
		return nil, fmt.Errorf("failed to read cache file: %w", err)
	}

	var cache SessionCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cache: %w", err)
	}

	return &cache, nil
}

// clearCache removes the session cache file
func clearCache() error {
	cachePath := getCachePath()

	err := os.Remove(cachePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove cache file: %w", err)
	}

	return nil
}
