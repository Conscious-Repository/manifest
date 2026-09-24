package server

import "strings"

// Commands use the native CLI parser, including installed custom commands.
// A single line prevents an accidental pasted command sequence. Interactive
// output belongs to the terminal, not the model transcript.
func validNativeCommand(text string) bool {
	if !strings.HasPrefix(text, "/") || len(text) > 8192 || strings.ContainsAny(text, "\r\n\x1b") {
		return false
	}
	for _, r := range text {
		if r < 32 || r == 127 {
			return false
		}
	}
	name := strings.Fields(text)
	if len(name) == 0 || len(name[0]) < 2 {
		return false
	}
	for _, r := range name[0][1:] {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == ':') {
			return false
		}
	}
	return true
}
