package cmd

import "testing"

func TestTerminalControlsRemoved(t *testing.T) {
	for _, value := range []string{
		"\x1b[31mred\x1b[0m",
		"\x1b]52;c;secret\a",
		"\x1b]8;;https://bad.example\x1b\\link\x1b]8;;\x1b\\",
		"\u202eevil\x00\r\n",
	} {
		safe := safe(value)
		for _, r := range safe {
			if r < 32 || r == 127 || r == 0x202e {
				t.Fatalf("control survived: %q", safe)
			}
		}
	}

	if safe("A\x1b[31mB\x1b[0mC") != "ABC" {
		t.Fatal("ANSI text changed")
	}
}
