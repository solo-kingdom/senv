package errors

import (
	"fmt"
)

// SenvError represents a structured error with code and message
type SenvError struct {
	Code    string
	Message string
	Cause   error
}

func (e *SenvError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

func (e *SenvError) Unwrap() error {
	return e.Cause
}

// New creates a new SenvError
func New(code, message string) *SenvError {
	return &SenvError{Code: code, Message: message}
}

// Wrap wraps an error with code and message
func Wrap(code, message string, cause error) *SenvError {
	return &SenvError{Code: code, Message: message, Cause: cause}
}

// Predefined error codes
const (
	CodeNotInitialized  = "E001"
	CodeInvalidPassword = "E002"
	CodeGroupNotFound   = "E003"
	CodeGroupExists     = "E004"
	CodeVariableNotFound = "E005"
	CodeConfigNotFound  = "E006"
	CodeConfigExists    = "E007"
	CodeSessionExpired  = "E008"
	CodeSessionNotFound = "E009"
	CodeGitNotRepo      = "E010"
	CodeGitNotRoot      = "E011"
	CodeGitNoChanges    = "E012"
	CodeGitNoRemote     = "E013"
	CodeGitConflict     = "E014"
	CodeEncryption      = "E015"
	CodeDecryption      = "E016"
	CodeFileNotFound    = "E017"
	CodeInvalidInput    = "E018"
)

// Predefined errors
var (
	ErrNotInitialized   = New(CodeNotInitialized, "项目未初始化")
	ErrInvalidPassword  = New(CodeInvalidPassword, "密码错误")
	ErrGroupNotFound    = New(CodeGroupNotFound, "分组不存在")
	ErrGroupExists      = New(CodeGroupExists, "分组已存在")
	ErrVariableNotFound = New(CodeVariableNotFound, "变量不存在")
	ErrConfigNotFound   = New(CodeConfigNotFound, "配置不存在")
	ErrConfigExists     = New(CodeConfigExists, "配置已存在")
	ErrSessionExpired   = New(CodeSessionExpired, "会话已过期")
	ErrSessionNotFound  = New(CodeSessionNotFound, "没有活动会话")
	ErrGitNotRepo       = New(CodeGitNotRepo, "不是 git 仓库")
	ErrGitNotRoot       = New(CodeGitNotRoot, "不是 git 仓库根目录")
	ErrGitNoChanges     = New(CodeGitNoChanges, "没有需要提交的更改")
	ErrGitNoRemote      = New(CodeGitNoRemote, "没有配置远程仓库")
	ErrGitConflict      = New(CodeGitConflict, "存在合并冲突")
	ErrEncryption       = New(CodeEncryption, "加密失败")
	ErrDecryption       = New(CodeDecryption, "解密失败")
	ErrFileNotFound     = New(CodeFileNotFound, "文件不存在")
	ErrInvalidInput     = New(CodeInvalidInput, "输入无效")
)

// IsSenvError checks if an error is a SenvError
func IsSenvError(err error) bool {
	_, ok := err.(*SenvError)
	return ok
}

// GetCode extracts the error code from an error
func GetCode(err error) string {
	var senvErr *SenvError
	if As(err, &senvErr) {
		return senvErr.Code
	}
	return ""
}

// As is a helper for errors.As
func As(err error, target interface{}) bool {
	return errorsAs(err, target)
}

func errorsAs(err error, target interface{}) bool {
	for {
		if e, ok := err.(*SenvError); ok {
			return setTarget(target, e)
		}
		if unwrapper, ok := err.(interface{ Unwrap() error }); ok {
			err = unwrapper.Unwrap()
			if err == nil {
				return false
			}
		} else {
			return false
		}
	}
}

func setTarget(target, value interface{}) bool {
	switch t := target.(type) {
	case **SenvError:
		*t = value.(*SenvError)
		return true
	}
	return false
}

// GroupNotFound creates a group not found error with group name
func GroupNotFound(group string) *SenvError {
	return Wrap(CodeGroupNotFound, fmt.Sprintf("分组 '%s' 不存在", group), nil)
}

// VariableNotFound creates a variable not found error
func VariableNotFound(group, key string) *SenvError {
	return Wrap(CodeVariableNotFound, fmt.Sprintf("变量 '%s' 在分组 '%s' 中不存在", key, group), nil)
}

// ConfigNotFound creates a config not found error with config name
func ConfigNotFound(name string) *SenvError {
	return Wrap(CodeConfigNotFound, fmt.Sprintf("配置 '%s' 不存在", name), nil)
}
