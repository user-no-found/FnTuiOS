// Command localize extracts and applies context-bound Go literal translations.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

type entry struct {
	Format      bool   `json:"format,omitempty"`
	ID          string `json:"id"`
	Source      string `json:"source"`
	Hash        string `json:"source_hash"`
	File        string `json:"file"`
	Context     string `json:"context"`
	Kind        string `json:"kind"`
	Reason      string `json:"reason"`
	Line        int    `json:"line"`
	Start       int    `json:"start"`
	End         int    `json:"end"`
	Translation string `json:"translation,omitempty"`
	Decision    string `json:"decision,omitempty"`
}

func main() {
	root := flag.String("root", "", "upstream source directory")
	output := flag.String("out", "", "candidate JSON path")
	catalog := flag.String("catalog", "", "reviewed catalog to apply")
	projectionReport := flag.String("projection-report", "", "generated native display API report")
	flag.Parse()
	entries, err := scan(*root)
	if err == nil {
		var extra []entry
		extra, err = registries(*root)
		entries = append(entries, extra...)
	}
	if err == nil && *catalog != "" {
		err = apply(*root, *catalog, entries)
		if err == nil {
			err = projectDisplay(*root, *catalog, *projectionReport)
		}
	}
	if err == nil && *output != "" {
		var data []byte
		data, err = json.MarshalIndent(entries, "", "  ")
		if err == nil {
			err = os.WriteFile(*output, append(data, '\n'), 0644)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
