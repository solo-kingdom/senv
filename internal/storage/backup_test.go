package storage

import "testing"

func TestListBackupGroupsInitDefault(t *testing.T) {
	mgr, _ := setupTestManager(t)
	groups, err := mgr.ListBackupGroups()
	if err != nil {
		t.Fatalf("ListBackupGroups: %v", err)
	}
	if len(groups) != 1 || groups[0] != "default" {
		t.Fatalf("init backup groups = %v, want [default]", groups)
	}
	exists, err := mgr.BackupGroupExists("default")
	if err != nil || !exists {
		t.Fatalf("backup default exists=%v err=%v", exists, err)
	}
}
