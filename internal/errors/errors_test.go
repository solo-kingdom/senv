package errors

import (
	"errors"
	"testing"
)

func TestSenvError(t *testing.T) {
	err := New("E001", "test error")

	if err.Code != "E001" {
		t.Errorf("Expected code E001, got %s", err.Code)
	}

	if err.Message != "test error" {
		t.Errorf("Expected message 'test error', got %s", err.Message)
	}

	expected := "[E001] test error"
	if err.Error() != expected {
		t.Errorf("Expected error string '%s', got '%s'", expected, err.Error())
	}
}

func TestSenvErrorWithCause(t *testing.T) {
	cause := errors.New("original error")
	err := Wrap("E002", "wrapped error", cause)

	if err.Cause != cause {
		t.Error("Cause should be set")
	}

	expected := "[E002] wrapped error: original error"
	if err.Error() != expected {
		t.Errorf("Expected error string '%s', got '%s'", expected, err.Error())
	}
}

func TestSenvErrorUnwrap(t *testing.T) {
	cause := errors.New("original error")
	err := Wrap("E002", "wrapped error", cause)

	unwrapped := err.Unwrap()
	if unwrapped != cause {
		t.Error("Unwrap should return the cause")
	}
}

func TestIsSenvError(t *testing.T) {
	senvErr := New("E001", "test")
	stdErr := errors.New("standard error")

	if !IsSenvError(senvErr) {
		t.Error("SenvError should be recognized")
	}

	if IsSenvError(stdErr) {
		t.Error("Standard error should not be recognized as SenvError")
	}
}

func TestGetCode(t *testing.T) {
	err := New("E001", "test")

	if GetCode(err) != "E001" {
		t.Errorf("Expected code E001, got %s", GetCode(err))
	}

	stdErr := errors.New("standard error")
	if GetCode(stdErr) != "" {
		t.Error("Standard error should return empty code")
	}
}

func TestAs(t *testing.T) {
	cause := errors.New("original")
	err := Wrap("E002", "wrapped", cause)

	var senvErr *SenvError
	if !As(err, &senvErr) {
		t.Error("As should return true for SenvError")
	}

	if senvErr.Code != "E002" {
		t.Errorf("Expected code E002, got %s", senvErr.Code)
	}
}

func TestPredefinedErrors(t *testing.T) {
	testCases := []struct {
		err  *SenvError
		code string
	}{
		{ErrNotInitialized, CodeNotInitialized},
		{ErrInvalidPassword, CodeInvalidPassword},
		{ErrGroupNotFound, CodeGroupNotFound},
		{ErrGroupExists, CodeGroupExists},
		{ErrVariableNotFound, CodeVariableNotFound},
		{ErrConfigNotFound, CodeConfigNotFound},
		{ErrConfigExists, CodeConfigExists},
		{ErrSessionExpired, CodeSessionExpired},
		{ErrSessionNotFound, CodeSessionNotFound},
		{ErrGitNotRepo, CodeGitNotRepo},
		{ErrGitNotRoot, CodeGitNotRoot},
		{ErrGitNoChanges, CodeGitNoChanges},
		{ErrGitNoRemote, CodeGitNoRemote},
		{ErrGitConflict, CodeGitConflict},
		{ErrEncryption, CodeEncryption},
		{ErrDecryption, CodeDecryption},
		{ErrFileNotFound, CodeFileNotFound},
		{ErrInvalidInput, CodeInvalidInput},
	}

	for _, tc := range testCases {
		if tc.err.Code != tc.code {
			t.Errorf("Error %s has wrong code: expected %s, got %s", tc.err.Message, tc.code, tc.err.Code)
		}
	}
}

func TestGroupNotFound(t *testing.T) {
	err := GroupNotFound("test-group")

	if err.Code != CodeGroupNotFound {
		t.Errorf("Expected code %s, got %s", CodeGroupNotFound, err.Code)
	}

	if err.Message != "分组 'test-group' 不存在" {
		t.Errorf("Unexpected message: %s", err.Message)
	}
}

func TestVariableNotFound(t *testing.T) {
	err := VariableNotFound("prod", "API_KEY")

	if err.Code != CodeVariableNotFound {
		t.Errorf("Expected code %s, got %s", CodeVariableNotFound, err.Code)
	}

	if err.Message != "变量 'API_KEY' 在分组 'prod' 中不存在" {
		t.Errorf("Unexpected message: %s", err.Message)
	}
}

func TestConfigNotFound(t *testing.T) {
	err := ConfigNotFound("database")

	if err.Code != CodeConfigNotFound {
		t.Errorf("Expected code %s, got %s", CodeConfigNotFound, err.Code)
	}

	if err.Message != "配置 'database' 不存在" {
		t.Errorf("Unexpected message: %s", err.Message)
	}
}
