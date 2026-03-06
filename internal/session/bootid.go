package session

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// GetSystemBootID returns a unique identifier for the current system boot
// This is used to invalidate session caches when the system restarts
func GetSystemBootID() (string, error) {
	switch runtime.GOOS {
	case "linux":
		return getLinuxBootID()
	case "darwin":
		return getMacOSBootID()
	default:
		// Fallback for other systems
		return getFallbackBootID()
	}
}

// getLinuxBootID reads the boot ID from /proc/sys/kernel/random/boot_id
func getLinuxBootID() (string, error) {
	data, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", fmt.Errorf("failed to read boot_id: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// getMacOSBootID gets the boot time using sysctl
func getMacOSBootID() (string, error) {
	// Use sysctl to get kern.boottime
	cmd := exec.Command("sysctl", "-n", "kern.boottime")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get boot time: %w", err)
	}

	// Parse output format: { sec = 1234567890, usec = 0 } Fri Mar  6 22:00:00 2026
	// Extract the sec value as a unique boot identifier
	outputStr := strings.TrimSpace(string(output))

	// Use hash of the boot time output as ID
	hash := sha256.Sum256([]byte(outputStr))
	return fmt.Sprintf("macos-%x", hash[:16]), nil
}

// getFallbackBootID provides a fallback method for unsupported systems
func getFallbackBootID() (string, error) {
	// Try to use uptime -s (available on most Unix systems)
	cmd := exec.Command("uptime", "-s")
	output, err := cmd.Output()
	if err == nil {
		bootTime := strings.TrimSpace(string(output))
		hash := sha256.Sum256([]byte(bootTime))
		return fmt.Sprintf("uptime-%x", hash[:16]), nil
	}

	// Last resort: use a combination of hostname and current time
	// This won't survive a reboot, but it's better than nothing
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "unknown"
	}

	// Use /proc/stat on Linux-like systems
	if data, err := os.ReadFile("/proc/stat"); err == nil {
		lines := strings.Split(string(data), "\n")
		if len(lines) > 0 {
			// Use first line (cpu statistics) as boot identifier
			hash := sha256.Sum256([]byte(lines[0]))
			return fmt.Sprintf("stat-%x", hash[:16]), nil
		}
	}

	// Final fallback - use hostname hash
	hash := sha256.Sum256([]byte(hostname))
	return fmt.Sprintf("host-%x", hash[:16]), nil
}
