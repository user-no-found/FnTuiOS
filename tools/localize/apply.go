package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

func apply(root, catalog string, entries []entry) error {
	raw, err := os.ReadFile(catalog)
	if err != nil {
		return err
	}
	var reviewed []entry
	if err = json.Unmarshal(raw, &reviewed); err != nil {
		return err
	}
	byID := map[string]entry{}
	for _, e := range reviewed {
		if _, ok := byID[e.ID]; ok {
			return fmt.Errorf("duplicate catalog id %s", e.ID)
		}
		byID[e.ID] = e
	}
	changes := map[string][]entry{}
	for _, e := range entries {
		if e.Kind != "candidate" && e.Kind != "config-display" {
			continue
		}
		t, ok := byID[e.ID]
		if !ok {
			return fmt.Errorf("unclassified %s", e.ID)
		}
		if t.Source != e.Source || t.Hash != e.Hash || t.Kind != e.Kind {
			return fmt.Errorf("source mismatch %s", e.ID)
		}
		if t.Decision == "keep" {
			continue
		}
		if t.Decision != "translate" || t.Translation == "" {
			return fmt.Errorf("missing reviewed translation %s", e.ID)
		}
		if e.Kind == "config-display" {
			continue
		}
		e.Translation = t.Translation
		changes[e.File] = append(changes[e.File], e)
	}
	for file, list := range changes {
		path := filepath.Join(root, file)
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sort.Slice(list, func(i, j int) bool { return list[i].Start > list[j].Start })
		for _, e := range list {
			content = append(append(append([]byte{}, content[:e.Start]...), []byte(strconv.Quote(e.Translation))...), content[e.End:]...)
		}
		if err = os.WriteFile(path, content, 0644); err != nil {
			return err
		}
	}
	return nil
}
