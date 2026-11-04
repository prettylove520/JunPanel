package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIEntryLinks(t *testing.T) {
	dir := t.TempDir()
	file := "# JunPanel public HTTPS access - managed by public-access.sh\nhttps://panel.example.org {\n}\n\nhttps://203.0.113.5 {\n}\n"
	if e := os.WriteFile(filepath.Join(dir, "panel-access.caddy"), []byte(file), 0644); e != nil {
		t.Fatal(e)
	}
	entries, ours := panelEntries(dir)
	links := entryLinks(entries)
	if !ours || len(links) != 2 || links[0] != "https://panel.example.org" || links[1] != "https://203.0.113.5" {
		t.Fatalf("links: %v ours=%v", links, ours)
	}
	if got, ours := panelEntries(t.TempDir()); len(got) != 0 || ours {
		t.Fatalf("missing file: %v %v", got, ours)
	}
}

func TestCLICommandsAreDocumented(t *testing.T) {
	for name := range cliCommands {
		if name != "menu" && name != "help" && !strings.Contains(cliHelp, "junpanel "+name) {
			t.Errorf("%s is missing from the help text", name)
		}
	}
	if hasFlag([]string{"-y"}, "-y") != true || hasFlag(nil, "-y") {
		t.Fatal("hasFlag")
	}
}
