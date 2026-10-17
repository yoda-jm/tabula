// Package scene reads and writes console scene files (.scn): a header line
// followed by one node line per parameter group.
package scene

import (
	"bufio"
	"fmt"
	"io"
	"strings"

	"github.com/yoda-jm/tabula/pkg/state"
)

// headerWidth is the length scene headers are padded to by the console.
const headerWidth = 127

// Header builds a scene header line: #4.0# "name" "note" %000000000 1
func Header(name, note string) string {
	h := fmt.Sprintf("#4.0# %q %q %%000000000 1", name, note)
	if len(h) < headerWidth {
		h += strings.Repeat(" ", headerWidth-len(h))
	}
	return h
}

// Load applies a scene file to the store and returns its header line.
// Lines that cannot be applied are reported as errors without stopping.
func Load(r io.Reader, st *state.Store, origin string) (header string, errs []error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	var lines []string
	n := 0
	for sc.Scan() {
		n++
		line := strings.TrimRight(sc.Text(), "\r")
		switch {
		case strings.HasPrefix(line, "#"):
			if header == "" {
				header = line
			}
		case strings.HasPrefix(line, "/"):
			lines = append(lines, line)
		case strings.TrimSpace(line) == "":
		default:
			errs = append(errs, fmt.Errorf("line %d: not a node line: %q", n, line))
		}
	}
	if err := sc.Err(); err != nil {
		errs = append(errs, err)
	}
	return header, append(errs, st.ApplyLines(lines, origin)...)
}

// Write writes the header and every scene node of the store.
func Write(w io.Writer, st *state.Store, header string) error {
	bw := bufio.NewWriter(w)
	bw.WriteString(header)
	bw.WriteByte('\n')
	for _, n := range st.Schema().Nodes {
		if !n.InScene {
			continue
		}
		bw.WriteString(st.NodeLine(n))
		bw.WriteByte('\n')
	}
	return bw.Flush()
}
