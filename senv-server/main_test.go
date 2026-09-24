package main

import "testing"

type pepperSpy struct {
	set bool
	got []byte
}

func (s *pepperSpy) SetTokenPepper(p []byte) { s.set, s.got = true, p }

func TestApplyTokenPepperReadsEnv(t *testing.T) {
	t.Setenv("SENV_SERVER_TOKEN_PEPPER", "p3pper")
	var spy pepperSpy
	applyTokenPepper(&spy)
	if !spy.set || string(spy.got) != "p3pper" {
		t.Errorf("配置 pepper 时应透传: set=%v got=%q", spy.set, spy.got)
	}

	t.Setenv("SENV_SERVER_TOKEN_PEPPER", "")
	var off pepperSpy
	applyTokenPepper(&off)
	if off.set {
		t.Error("pepper 为空时不应配置（保持原 SHA-256 行为）")
	}
}
