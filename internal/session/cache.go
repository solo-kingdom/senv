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

// getTempCachePath returns the path to the temporary session cache file
// (used for "restart" and "duration" timeout types)
func getTempCachePath() string {
	// Prefer XDG_RUNTIME_DIR
	if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
		cacheDir := filepath.Join(runtimeDir, "senv")
		os.MkdirAll(cacheDir, 0700)
		return filepath.Join(cacheDir, fmt.Sprintf("session-%d", os.Getuid()))
	}

	// Fallback to /tmp
	return filepath.Join("/tmp", fmt.Sprintf("senv-session-%d", os.Getuid()))
}

// getPersistentCachePath returns the path to the persistent session cache file
// (used for "never" timeout type to survive system reboots)
func getPersistentCachePath() string {
	// Use user's data directory for persistent storage
	if dataDir, err := os.UserHomeDir(); err == nil {
		cacheDir := filepath.Join(dataDir, ".local", "share", "senv", "session")
		os.MkdirAll(cacheDir, 0700)
		return filepath.Join(cacheDir, fmt.Sprintf("session-%d", os.Getuid()))
	}

	// Fallback to temp directory if home directory is not available
	return getTempCachePath()
}

// getCachePathForType returns the appropriate cache path based on timeout type
func getCachePathForType(timeoutType TimeoutType) string {
	switch timeoutType {
	case TimeoutNever:
		return getPersistentCachePath()
	default:
		// For "restart" and "duration" types, use temp directory
		return getTempCachePath()
	}
}

// getAllCachePaths returns all possible cache paths for loading
func getAllCachePaths() []string {
	return []string{
		getPersistentCachePath(),
		getTempCachePath(),
	}
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
	// Determine cache path based on timeout type
	timeoutType := TimeoutType(cache.TimeoutType)
	cachePath := getCachePathForType(timeoutType)

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

	// Clear cache from other locations to avoid conflicts
	clearOtherCacheLocations(timeoutType)

	return nil
}

// readCacheFile reads and parses a session cache from the given path.
func readCacheFile(cachePath string) (*SessionCache, error) {
	data, err := os.ReadFile(cachePath)
	if err != nil {
		return nil, err
	}

	var cache SessionCache
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil, fmt.Errorf("failed to unmarshal cache: %w", err)
	}

	return &cache, nil
}

// loadCache loads the session cache from disk
// It searches both persistent and temporary cache locations
func loadCache() (*SessionCache, error) {
	// Try all possible cache paths
	for _, cachePath := range getAllCachePaths() {
		cache, err := readCacheFile(cachePath)
		if err != nil {
			if os.IsNotExist(err) {
				continue // Try next location
			}
			return nil, fmt.Errorf("failed to read cache file: %w", err)
		}

		return cache, nil
	}

	return nil, nil // No cache file exists in any location
}

// loadCacheForDataPath loads the session cache for the given data path.
// When multiple cache files exist, only the one matching dataPathHash is returned.
func loadCacheForDataPath(dataPath string) (*SessionCache, error) {
	expectedHash := hashDataPath(dataPath)

	for _, cachePath := range getAllCachePaths() {
		cache, err := readCacheFile(cachePath)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("failed to read cache file: %w", err)
		}

		if cache.DataPathHash == expectedHash {
			return cache, nil
		}
	}

	return nil, nil
}

// clearCache removes all session cache files
func clearCache() error {
	var lastErr error
	for _, cachePath := range getAllCachePaths() {
		err := os.Remove(cachePath)
		if err != nil && !os.IsNotExist(err) {
			lastErr = err
		}
	}
	if lastErr != nil {
		return fmt.Errorf("failed to remove cache file: %w", lastErr)
	}
	return nil
}

// clearOtherCacheLocations removes cache files from locations other than the current one
func clearOtherCacheLocations(currentType TimeoutType) {
	currentPath := getCachePathForType(currentType)
	for _, cachePath := range getAllCachePaths() {
		if cachePath != currentPath {
			os.Remove(cachePath)
		}
	}
}
