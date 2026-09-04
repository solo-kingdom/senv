package config

import "github.com/wii/senv/internal/storage"

// PlanConfigRepair returns the repair plan for quarantined legacy config
// entries: deterministic portable suggestions plus ciphertext existence.
func (m *Manager) PlanConfigRepair() ([]storage.ConfigRepairItem, error) {
	return m.storage.PlanConfigRepair()
}

// ExecuteConfigRepair applies an explicit set of renames (old name -> new
// name). Missing-ciphertext entries are dropped only when dropMissing is true;
// every quarantined entry must be covered by one of the two decisions.
func (m *Manager) ExecuteConfigRepair(renames map[string]string, dropMissing bool) error {
	return m.mutate(func(locked *Manager) error {
		return locked.storage.ExecuteConfigRepair(renames, dropMissing)
	})
}
