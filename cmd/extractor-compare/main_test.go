package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestNoArgumentEcho(t *testing.T) {
	for _, args := range [][]string{{"-private@example.org"}, {"-ritual", "private@example.org"}, {"PRIVATE"}} {
		var out bytes.Buffer
		if run(args, &out) != 2 || strings.Contains(out.String(), "PRIVATE") || strings.Contains(out.String(), "private@") {
			t.Fatal(out.String())
		}
	}
}
