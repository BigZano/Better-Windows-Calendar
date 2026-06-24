package main

import "testing"

func TestFirstICSArg(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"empty", nil, ""},
		{"no ics", []string{"--mode", "tray"}, ""},
		{"plain path", []string{`C:\tmp\cal.ics`}, `C:\tmp\cal.ics`},
		{"uppercase extension", []string{`C:\tmp\CAL.ICS`}, `C:\tmp\CAL.ICS`},
		{"mixed case extension", []string{`event.Ics`}, `event.Ics`},
		{"ignores flags, returns first ics", []string{"--mode", "tray", "a.ics", "b.ics"}, "a.ics"},
		{"flag before ics", []string{"--format", "json", `D:\x.ics`}, `D:\x.ics`},
		{"non-ics extension ignored", []string{"notes.txt", "report.pdf"}, ""},
		{"ics substring not suffix", []string{"my.ics.bak"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstICSArg(tt.args); got != tt.want {
				t.Errorf("firstICSArg(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}
