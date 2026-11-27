package workflow

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// WriteDOT writes a deterministic Graphviz directed graph. Node IDs, event
// names, and transition IDs are encoded as escaped text. Retired elements
// are displayed with dashed outlines. The caller owns and closes w.
func (g Graph) WriteDOT(w io.Writer) error {
	out := bufio.NewWriter(w)
	write := func(format string, args ...any) error {
		_, err := fmt.Fprintf(out, format, args...)
		return err
	}
	if err := write("digraph workflow {\n  rankdir=LR;\n"); err != nil {
		return err
	}
	// Generated identifiers avoid collisions between user-provided state names
	// and the invisible initial marker.
	ids := make(map[string]string, len(g.Nodes))
	for i, node := range g.Nodes {
		if _, exists := ids[node.ID]; exists {
			return fmt.Errorf("%w: duplicate graph state %q", ErrInvalidDefinition, node.ID)
		}
		ids[node.ID] = fmt.Sprintf("n%d", i)
	}
	start, exists := ids[g.Start]
	if !exists {
		return fmt.Errorf("%w: unknown graph start state %q", ErrInvalidDefinition, g.Start)
	}
	if err := write("  start [shape=point];\n  start -> %s;\n", start); err != nil {
		return err
	}
	for _, node := range g.Nodes {
		style := ""
		if node.Retired {
			style = ", style=dashed"
		}
		if err := write("  %s [label=%s%s];\n", ids[node.ID], dotQuote(node.ID), style); err != nil {
			return err
		}
	}
	for _, edge := range g.Edges {
		from, fromOK := ids[edge.From]
		to, toOK := ids[edge.To]
		if !fromOK || !toOK {
			return fmt.Errorf("%w: edge %q has an unknown endpoint", ErrInvalidDefinition, edge.ID)
		}
		label := edge.Event + " [" + edge.ID + "]"
		style := ""
		if edge.Retired {
			style = ", style=dashed"
		}
		if err := write("  %s -> %s [label=%s%s];\n", from, to, dotQuote(label), style); err != nil {
			return err
		}
	}
	if err := write("}\n"); err != nil {
		return err
	}
	return out.Flush()
}

func dotQuote(s string) string {
	// Graphviz interprets backslashes in labels. Escape them before quoting,
	// while turning control characters into visible, printable sequences.
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString("\\\\")
		case '"':
			b.WriteString("\\\"")
		case '\n':
			b.WriteString("\\n")
		case '\r':
			b.WriteString("\\r")
		case '\t':
			b.WriteString("\\t")
		default:
			if r < 32 || r == 127 {
				quoted := strconv.QuoteRuneToASCII(r)
				b.WriteString("\\\\")
				b.WriteString(quoted[2 : len(quoted)-1])
			} else {
				b.WriteRune(r)
			}
		}
	}
	return "\"" + b.String() + "\""
}
