package utils

import (
	"strings"
	"testing"
)

func TestValidatePassword(t *testing.T) {
	valid := "安全Passphrase-2026!"
	if err := ValidatePassword(valid); err != nil {
		t.Fatalf("valid password rejected: %v", err)
	}
	for _, value := range []string{
		"short",
		"            ",
		" leading-space-password",
		"trailing-space-password ",
		strings.Repeat("a", 73),
	} {
		if err := ValidatePassword(value); err == nil {
			t.Fatalf("invalid password accepted: %q", value)
		}
	}
}
