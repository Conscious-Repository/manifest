package record

import "regexp"

// THE checkbox-line grammar: `- [ ] text` / `* [x] text`, indent captured.
// Every outline/task parser imports this — no local copies. Depth POLICY
// (relative frame-stack, absolute threshold, or flat) is the domain's
// declaration; the kernel owns the line grammar, the indent rule, and the
// stack machinery.
var CheckboxRe = regexp.MustCompile(`^([ \t]*)[-*]\s*\[([ xX])\]\s?(.*)$`)

// ParseCheckboxLine reads one line: indent width (tab = 4), checked state,
// and the rest (text + inline fields). ok = false when not a checkbox line.
func ParseCheckboxLine(line string) (indent int, checked bool, rest string, ok bool) {
	m := CheckboxRe.FindStringSubmatch(line)
	if m == nil {
		return 0, false, "", false
	}
	return IndentWidth(m[1]), m[2] == "x" || m[2] == "X", m[3], true
}

// IndentWidth is the one indent rule: tab = 4 columns, space = 1.
func IndentWidth(s string) int {
	w := 0
	for _, r := range s {
		if r == '\t' {
			w += 4
		} else {
			w++
		}
	}
	return w
}

// IndentUnit is the canonical emission indent: four spaces per level.
const IndentUnit = "    "

// Frames is the relative-nesting stack (goals-lineage outlines): each parsed
// checkbox line pops frames at >= its indent, attaches under the surviving
// top (or as a root), and pushes itself. Depth caps + freeze-beyond are the
// caller's policy — Depth() tells it where the line would attach.
type Frames[T any] struct {
	stack []frame[T]
}

type frame[T any] struct {
	indent int
	value  T
}

// Descend pops frames at indent >= w and returns the resulting depth (0 =
// root) plus the surviving parent value (zero T when attaching at root).
func (f *Frames[T]) Descend(w int) (depth int, parent T) {
	for len(f.stack) > 0 && f.stack[len(f.stack)-1].indent >= w {
		f.stack = f.stack[:len(f.stack)-1]
	}
	var zero T
	if len(f.stack) == 0 {
		return 0, zero
	}
	return len(f.stack), f.stack[len(f.stack)-1].value
}

// Top returns the current deepest frame's value (ok=false when empty).
func (f *Frames[T]) Top() (T, bool) {
	var zero T
	if len(f.stack) == 0 {
		return zero, false
	}
	return f.stack[len(f.stack)-1].value, true
}

// Push records the just-attached node so deeper lines nest under it.
func (f *Frames[T]) Push(indent int, v T) { f.stack = append(f.stack, frame[T]{indent, v}) }

// Reset clears the stack (section boundaries).
func (f *Frames[T]) Reset() { f.stack = f.stack[:0] }
