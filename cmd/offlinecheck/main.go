package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	hours "planerum"
)

func main() {
	root := "data/offline-check"
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	python := os.Getenv("OFFICE_PYTHON")
	if python == "" {
		python = "python3"
	}
	ctx := context.Background()
	s := hours.Service{Store: hours.NewMemoryStore(), Generator: hours.Generator{Model: hours.DemoModel{}}}
	spec := hours.ProgramSpec{Title: "Демонстрационная программа", Kind: hours.General, Audience: "Начинающие", EntryLevel: "Нулевой", Delivery: "Очно", Requested: 24, Attestation: "Защита проекта"}
	p, _, err := s.Create(ctx, spec)
	if err != nil {
		log.Fatal(err)
	}
	p, err = s.Approve(ctx, p.ID, p.Revision)
	if err != nil {
		log.Fatal(err)
	}
	p, err = s.Generate(ctx, p.ID, p.Revision)
	if err != nil {
		log.Fatal(err)
	}
	files, err := (hours.Exporter{Root: root, Python: python, Script: filepath.Join("office", "export.py")}).Export(ctx, p)
	if err != nil {
		log.Fatal(err)
	}
	for name := range files {
		fmt.Println(filepath.Join(root, p.ID, "revision-3", name))
	}
}
