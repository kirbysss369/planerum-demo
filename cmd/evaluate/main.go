package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	h "planerum"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	output := flag.String("out", "", "output JSON file (default stdout)")
	flag.Parse()
	if flag.NArg() == 0 {
		return errors.New("provide exported program.json files as arguments")
	}
	var programs []h.Program
	seen := map[string]bool{}
	for _, name := range flag.Args() {
		f, err := os.Open(name)
		if err != nil {
			return fmt.Errorf("cannot open input: %w", err)
		}
		data, err := io.ReadAll(io.LimitReader(f, (8<<20)+1))
		f.Close()
		if err != nil {
			return err
		}
		if len(data) > 8<<20 {
			return errors.New("input exceeds 8 MiB")
		}
		var p h.Program
		if err := json.Unmarshal(data, &p); err != nil {
			return err
		}
		if p.ID == "" {
			return errors.New("input is not an exported program")
		}
		if seen[p.ID] {
			return errors.New("repeated program ID: independent runs must have different IDs")
		}
		seen[p.ID] = true
		programs = append(programs, p)
	}
	result, err := h.EvaluatePrograms(programs)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if *output != "" {
		return os.WriteFile(*output, data, 0600)
	}
	_, err = os.Stdout.Write(data)
	return err
}
