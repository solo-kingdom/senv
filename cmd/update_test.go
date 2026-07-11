package cmd

import (
	"bytes"
	"strings"
	"testing"
)

func TestUpdateCmdAlreadyUpToDate(t *testing.T) {
	oldVersion := Version
	oldUpdateVersion := updateVersion
	t.Cleanup(func() {
		Version = oldVersion
		updateVersion = oldUpdateVersion
	})
	Version = "v9.9.9"
	updateVersion = "v9.9.9"

	var buf bytes.Buffer
	updateCmd.SetOut(&buf)
	updateCmd.SetErr(&buf)

	if err := updateCmd.RunE(updateCmd, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "already up to date") {
		t.Fatalf("output = %q", buf.String())
	}
}
