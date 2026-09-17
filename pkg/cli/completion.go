// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 Robin Jarry

package cli

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/alecthomas/kong"
)

// Candidate is a single completion suggestion: the value to insert and its
// optional one-line help text.
type Candidate struct {
	Value string
	Help  string
}

// bashComplete implements the "complete -C trexc trexc" protocol: when invoked
// with COMP_LINE set, it prints candidate completions for the current word and
// returns true so the caller can exit. It walks the kong grammar to complete
// subcommands, flags, enum values and static "completion:" tag hints.
func bashComplete(app *kong.Kong) bool {
	compLine := os.Getenv("COMP_LINE")
	if compLine == "" {
		return false
	}
	compPoint, err := strconv.Atoi(os.Getenv("COMP_POINT"))
	if err != nil || compPoint > len(compLine) {
		compPoint = len(compLine)
	}
	line := compLine[:compPoint]

	words := strings.Fields(line)
	// remove the program name
	if len(words) > 0 {
		words = words[1:]
	}
	// if the line ends with a space, we're completing a new word
	trailing := strings.HasSuffix(line, " ")

	candidates, _ := completeArgs(app, words, trailing)

	maxWidth := 0
	for _, c := range candidates {
		if n := len(c.Value); n > maxWidth {
			maxWidth = n
		}
	}
	for _, c := range candidates {
		if len(candidates) > 1 && c.Help != "" {
			fmt.Printf("%-*s    (%s)\n", maxWidth, c.Value, c.Help)
		} else {
			fmt.Println(c.Value)
		}
	}

	return true
}

// Complete returns the completion candidates and the current word prefix for a
// command line typed into the interactive shell, walking grammar's kong model.
// grammar is a fresh instance of the top-level command struct. It is the
// programmatic counterpart of bashComplete, sharing completeArgs.
func Complete(grammar any, line string) ([]Candidate, string) {
	app, err := kong.New(grammar)
	if err != nil {
		return nil, ""
	}
	words := strings.Fields(line)
	trailing := line == "" || strings.HasSuffix(line, " ")
	return completeArgs(app, words, trailing)
}

// completeArgs walks the kong grammar to produce completion candidates for the
// argument words (the program name already stripped). trailing is true when the
// line ends on a word boundary, so a fresh word is being completed rather than
// the last one. It completes subcommands, flags, flag values, and positional
// arguments tagged `completion:"file"` (filesystem paths). It returns the
// matching candidates and the current word prefix.
func completeArgs(app *kong.Kong, words []string, trailing bool) ([]Candidate, string) {
	// separate the token being completed from the fully-typed words
	current := ""
	consumed := words
	if !trailing && len(words) > 0 {
		current = words[len(words)-1]
		consumed = words[:len(words)-1]
	}

	seen := make(map[string]bool)
	for _, w := range consumed {
		seen[w] = true
	}

	// walk the typed words: descend into subcommands, skip flags and their
	// values, and count how many positional arguments have been supplied.
	node := app.Model.Node
	posGiven := 0
	var valueFlag *kong.Flag // non-nil when the current token is its value
	for i := 0; i < len(consumed); i++ {
		w := consumed[i]
		valueFlag = nil
		if strings.HasPrefix(w, "-") {
			f := findFlag(node, w)
			if f != nil && !f.IsBool() && !f.IsCounter() && !strings.Contains(w, "=") {
				if i+1 < len(consumed) {
					i++ // consume the flag's value
				} else {
					valueFlag = f // value is the token being completed
				}
			}
			continue
		}
		if child := findChild(node, w); child != nil {
			node = child
			posGiven = 0
		} else {
			posGiven++
		}
	}

	var candidates []Candidate
	add := func(value, help string) {
		if !strings.HasPrefix(value, current) || seen[value] {
			return
		}
		candidates = append(candidates, Candidate{value, help})
	}

	// complete the value of a flag that expects one
	if valueFlag != nil {
		for _, v := range flagValueHints(valueFlag) {
			add(v, "")
		}
		return candidates, current
	}

	// complete subcommands
	for _, child := range node.Children {
		if child.Hidden {
			continue
		}
		add(child.Name, child.Help)
	}

	// complete flags from current node and all ancestors
	for n := node; n != nil; n = n.Parent {
		for _, flag := range n.Flags {
			if flag.Hidden || flag.Name == "help" {
				continue
			}
			long := "--" + flag.Name
			short := ""
			if flag.Short != 0 {
				short = "-" + string(flag.Short)
			}
			if seen[long] || seen[short] {
				continue
			}
			add(long, flag.Help)
			if flag.Tag.Negatable != "" {
				add("--no-"+flag.Name, flag.Help)
			}
		}
	}

	// complete a positional file argument
	if posGiven < len(node.Positional) && node.Positional[posGiven].Tag.Get("completion") == "file" {
		for _, c := range fileCompletions(current) {
			add(c.Value, c.Help)
		}
	}

	return candidates, current
}

// fileCompletions lists filesystem entries matching prefix, appending "/" to
// directories. Hidden entries are shown only when the prefix targets them.
func fileCompletions(prefix string) []Candidate {
	dir := ""
	base := prefix
	if i := strings.LastIndex(prefix, "/"); i >= 0 {
		dir = prefix[:i+1]
		base = prefix[i+1:]
	}
	readDir := dir
	if readDir == "" {
		readDir = "."
	}
	entries, err := os.ReadDir(readDir)
	if err != nil {
		return nil
	}
	var out []Candidate
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, base) {
			continue
		}
		if base == "" && strings.HasPrefix(name, ".") {
			continue
		}
		value := dir + name
		if e.IsDir() {
			value += "/"
		}
		out = append(out, Candidate{Value: value})
	}
	return out
}

func flagValueHints(flag *kong.Flag) []string {
	if flag.Enum != "" {
		return strings.Split(flag.Enum, ",")
	}
	if c := flag.Tag.Get("completion"); c != "" {
		return strings.Split(c, ",")
	}
	return nil
}

func findFlag(node *kong.Node, name string) *kong.Flag {
	name = strings.TrimLeft(name, "-")
	// also handle --flag=value
	name, _, _ = strings.Cut(name, "=")
	for n := node; n != nil; n = n.Parent {
		for _, f := range n.Flags {
			if f.Name == name || (f.Short != 0 && string(f.Short) == name) {
				return f
			}
		}
	}
	return nil
}

func findChild(node *kong.Node, name string) *kong.Node {
	for _, child := range node.Children {
		if child.Name == name || slices.Contains(child.Aliases, name) {
			return child
		}
	}
	return nil
}
