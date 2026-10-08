package construction

import "fmt"

// fmtSscan parses "W w 0 G X0 Y0 m X1 Y1 l S" line segments.
func fmtSscan(line string, w, x0, y0, x1, y1 *float64) (int, error) {
	return fmt.Sscanf(line, "%g w 0 G %g %g m %g %g l S", w, x0, y0, x1, y1)
}
