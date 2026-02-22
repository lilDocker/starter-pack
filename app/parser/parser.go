package parser

import (
	"bufio"
	"os"
	"strings"
)

type Instruction struct {
	Command string
	Value   string
}

func ParseLilDockfile(path string) ([]Instruction, error) {

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var instructions []Instruction

	scanner := bufio.NewScanner(file)

	for scanner.Scan() {

		line := strings.TrimSpace(scanner.Text())

		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, " ", 2)

		cmd := parts[0]
		val := ""

		if len(parts) > 1 {
			val = parts[1]
		}

		instructions = append(instructions, Instruction{
			Command: cmd,
			Value:   val,
		})
	}

	return instructions, nil
}