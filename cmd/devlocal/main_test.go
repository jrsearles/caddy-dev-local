package main

import (
	"strings"
	"testing"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		want    command
		wantArg []string
	}{
		{name: "default", want: commandRunOnce},
		{name: "default flags", args: []string{"--hosts-file=false"}, want: commandRunOnce, wantArg: []string{"--hosts-file=false"}},
		{name: "start", args: []string{"start", "--poll-interval=1m"}, want: commandStart, wantArg: []string{"--poll-interval=1m"}},
		{name: "clean", args: []string{"clean", "--ui=false"}, want: commandClean, wantArg: []string{"--ui=false"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotArgs, err := parseCommand(tt.args)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("command = %v, want %v", got, tt.want)
			}
			if strings.Join(gotArgs, " ") != strings.Join(tt.wantArg, " ") {
				t.Fatalf("args = %q, want %q", gotArgs, tt.wantArg)
			}
		})
	}
}

func TestParseCommandRejectsUnknownCommand(t *testing.T) {
	if _, _, err := parseCommand([]string{"watch"}); err == nil {
		t.Fatal("parseCommand accepted unknown command")
	}
}

func TestRunRejectsCommandAfterFlags(t *testing.T) {
	err := run([]string{"--hosts-file=false", "start"})
	if err == nil || !strings.Contains(err.Error(), "unexpected argument") {
		t.Fatalf("run error = %v, want unexpected argument", err)
	}
}

func TestCommandHelp(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"start", "--help"}, {"clean", "--help"}} {
		if err := run(args); err != nil {
			t.Fatalf("run(%q): %v", args, err)
		}
	}
}
