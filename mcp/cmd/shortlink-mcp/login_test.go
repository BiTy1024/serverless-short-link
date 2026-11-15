package main

import (
	"context"
	"errors"
	"testing"
)

func TestLoginRefusesTerminalStdout(t *testing.T) {
	err := runLogin(context.Background(), []string{"-region", "eu-central-1", "-client-id", "c"}, func() bool { return true })
	if !errors.Is(err, errStdoutIsTerminal) {
		t.Fatalf("err = %v, want errStdoutIsTerminal", err)
	}
}

func TestLoginChecksConfigWhenStdoutIsPiped(t *testing.T) {
	err := runLogin(context.Background(), []string{"-region", "", "-client-id", ""}, func() bool { return false })
	if err == nil || errors.Is(err, errStdoutIsTerminal) {
		t.Fatalf("err = %v, want a configuration error", err)
	}
}
