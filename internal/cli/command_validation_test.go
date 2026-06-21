package cli

import (
	"strings"
	"testing"
)

func TestNoArgCommandsRejectExtraArgsBeforeConfig(t *testing.T) {
	tests := []struct {
		name string
		run  func() error
		want string
	}{
		{name: "list", run: func() error { return cmdList([]string{"extra"}) }, want: "usage: peek list"},
		{name: "delete-all", run: func() error { return cmdDeleteAll([]string{"extra"}) }, want: "usage: peek delete-all"},
		{name: "config show", run: func() error { return cmdConfig([]string{"show", "extra"}) }, want: "usage: peek config show"},
		{name: "token list", run: func() error { return cmdToken([]string{"list", "extra"}) }, want: "usage: peek token list"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run()
			if err == nil || err.Error() != tt.want {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestTokenValidationRunsBeforeConfigLoad(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "unknown subcommand", args: []string{"bogus"}, want: "unknown token subcommand: bogus"},
		{name: "missing create name", args: []string{"create"}, want: "--name is required"},
		{name: "create name missing value", args: []string{"create", "--name"}, want: "--name requires a value"},
		{name: "create unknown flag", args: []string{"create", "--scope", "all"}, want: "unknown flag: --scope"},
		{name: "create unexpected arg", args: []string{"create", "service", "--name", "ok"}, want: "unexpected argument: service"},
		{name: "revoke missing id value", args: []string{"revoke", "--id"}, want: "--id requires a value"},
		{name: "revoke unknown flag", args: []string{"revoke", "--bad"}, want: "unknown flag: --bad"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cmdToken(tt.args)
			if err == nil || err.Error() != tt.want {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestPasswordRejectsUnexpectedPositionalArg(t *testing.T) {
	err := cmdPassword([]string{"page", "extra", "--clear"})
	if err == nil || !strings.Contains(err.Error(), "unexpected argument: extra") {
		t.Fatalf("error = %v", err)
	}
}

func TestPasswordRejectsUnknownFlagBeforeSlug(t *testing.T) {
	err := cmdPassword([]string{"--bad"})
	if err == nil || err.Error() != "unknown flag: --bad" {
		t.Fatalf("error = %v", err)
	}
}
