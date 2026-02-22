// Package parser parses lilDockfile files into a list of instructions.
//
// lilDockfile Syntax Reference:
//
//   FROM <base>              - Set the base image (alpine, ubuntu, scratch)
//   WORKDIR <path>           - Set working directory inside the container
//   COPY <src> <dst>         - Copy files from build context into the image
//   ENV <KEY>=<VALUE>        - Set environment variables
//   RUN <command>            - Execute a shell command during build
//   EXPOSE <port>[/tcp|udp]  - Document which port the container listens on
//   CMD ["exec", "args"]     - Default command to run when container starts
//   LABEL <key>=<value>      - Add metadata labels to the image
//   ARG <name>[=<default>]   - Define a build-time variable
//   VOLUME <path>            - Declare a mount point
//   USER <name>              - Set the user to run as

package parser

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"unicode"
)

// InstructionType represents a lilDockfile instruction keyword
type InstructionType string

const (
	FROM    InstructionType = "FROM"
	WORKDIR InstructionType = "WORKDIR"
	COPY    InstructionType = "COPY"
	ENV     InstructionType = "ENV"
	RUN     InstructionType = "RUN"
	EXPOSE  InstructionType = "EXPOSE"
	CMD     InstructionType = "CMD"
	LABEL   InstructionType = "LABEL"
	ARG     InstructionType = "ARG"
	VOLUME  InstructionType = "VOLUME"
	USER    InstructionType = "USER"
)

// Instruction represents a single parsed line from a lilDockfile
type Instruction struct {
	Type    InstructionType
	Args    []string      // tokenized arguments
	Raw     string        // original raw argument string
	LineNum int
}

// Dockerfile holds all parsed instructions
type Dockerfile struct {
	Instructions []Instruction
	Path         string
}

// Parse reads a lilDockfile at the given path and returns a Dockerfile
func Parse(path string) (*Dockerfile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot open lilDockfile: %w", err)
	}
	defer f.Close()

	df := &Dockerfile{Path: path}
	scanner := bufio.NewScanner(f)
	lineNum := 0
	var continuationLine string

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		// Strip inline comments (not inside quotes)
		line = stripComment(line)
		line = strings.TrimSpace(line)

		// Skip blank lines and full-line comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Handle line continuation with backslash
		if strings.HasSuffix(line, "\\") {
			continuationLine += strings.TrimSuffix(line, "\\") + " "
			continue
		}
		if continuationLine != "" {
			line = continuationLine + line
			continuationLine = ""
		}

		instr, err := parseLine(line, lineNum)
		if err != nil {
			return nil, fmt.Errorf("lilDockfile:%d: %w", lineNum, err)
		}
		df.Instructions = append(df.Instructions, instr)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading lilDockfile: %w", err)
	}

	if err := validate(df); err != nil {
		return nil, err
	}

	return df, nil
}

func parseLine(line string, lineNum int) (Instruction, error) {
	// Split into keyword and the rest
	parts := strings.SplitN(line, " ", 2)
	if len(parts) == 0 {
		return Instruction{}, fmt.Errorf("empty line")
	}

	keyword := strings.ToUpper(strings.TrimSpace(parts[0]))
	var raw string
	if len(parts) > 1 {
		raw = strings.TrimSpace(parts[1])
	}

	instrType := InstructionType(keyword)
	switch instrType {
	case FROM, WORKDIR, COPY, ENV, RUN, EXPOSE, CMD, LABEL, ARG, VOLUME, USER:
		// valid
	default:
		return Instruction{}, fmt.Errorf("unknown instruction %q", keyword)
	}

	args, err := tokenize(raw, instrType)
	if err != nil {
		return Instruction{}, fmt.Errorf("bad args for %s: %w", keyword, err)
	}

	return Instruction{
		Type:    instrType,
		Args:    args,
		Raw:     raw,
		LineNum: lineNum,
	}, nil
}

// tokenize splits the raw argument string for an instruction into tokens.
// CMD supports JSON array syntax: CMD ["prog", "arg1"]
// ENV supports KEY=VALUE pairs
// Everything else is shell-split
func tokenize(raw string, t InstructionType) ([]string, error) {
	if raw == "" {
		return nil, nil
	}

	switch t {
	case CMD:
		// JSON-style array: ["prog", "arg"]
		if strings.HasPrefix(raw, "[") {
			return parseJSONArray(raw)
		}
		// shell form: CMD prog arg1 arg2
		return shellSplit(raw), nil

	case ENV:
		// KEY=VALUE or KEY VALUE
		return parseKeyValue(raw), nil

	case LABEL:
		return parseKeyValue(raw), nil

	default:
		return shellSplit(raw), nil
	}
}

// parseJSONArray parses ["a","b","c"] style arrays (simplified, no escaping)
func parseJSONArray(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return nil, fmt.Errorf("expected JSON array like [\"cmd\", \"arg\"]")
	}
	inner := s[1 : len(s)-1]
	var result []string
	for _, part := range strings.Split(inner, ",") {
		part = strings.TrimSpace(part)
		part = strings.Trim(part, "\"")
		if part != "" {
			result = append(result, part)
		}
	}
	return result, nil
}

// parseKeyValue handles KEY=VALUE or KEY VALUE pairs
func parseKeyValue(s string) []string {
	// Could have multiple: KEY1=val1 KEY2=val2
	return shellSplit(s)
}

// shellSplit splits a string by whitespace, respecting quoted strings
func shellSplit(s string) []string {
	var tokens []string
	var current strings.Builder
	inQuote := false
	quoteChar := rune(0)

	for _, ch := range s {
		switch {
		case inQuote:
			if ch == quoteChar {
				inQuote = false
			} else {
				current.WriteRune(ch)
			}
		case ch == '"' || ch == '\'':
			inQuote = true
			quoteChar = ch
		case unicode.IsSpace(ch):
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(ch)
		}
	}
	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}
	return tokens
}

// stripComment removes inline # comments that are not inside quotes
func stripComment(line string) string {
	inQuote := false
	quoteChar := rune(0)
	for i, ch := range line {
		if inQuote {
			if ch == quoteChar {
				inQuote = false
			}
		} else if ch == '"' || ch == '\'' {
			inQuote = true
			quoteChar = ch
		} else if ch == '#' {
			return line[:i]
		}
	}
	return line
}

// validate checks that the Dockerfile has required instructions in the right order
func validate(df *Dockerfile) error {
	if len(df.Instructions) == 0 {
		return fmt.Errorf("lilDockfile is empty")
	}
	if df.Instructions[0].Type != FROM {
		return fmt.Errorf("lilDockfile must start with FROM")
	}
	return nil
}

// Print pretty-prints the parsed Dockerfile for debugging
func (df *Dockerfile) Print() {
	fmt.Printf("=== Parsed lilDockfile (%s) ===\n", df.Path)
	for _, instr := range df.Instructions {
		fmt.Printf("  [%d] %-8s %v\n", instr.LineNum, instr.Type, instr.Args)
	}
}
