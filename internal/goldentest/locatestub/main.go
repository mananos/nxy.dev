// Command locatestub stands in for rg and codegraph in the locate golden tests, so the golden does not
// depend on what the machine has installed. The same binary is copied as `rg` and as `codegraph`
// and acts by its own name. Both answer `--version`; everything else comes from a JSON table in
// $NXY_GOLDEN_LOCATE_TABLE:
//
//	{"rg": {"<pattern>": {"code": 0, "stdout": "..."}, "-w:<pattern>": {...}},
//	 "codegraph": {"<explore|callers|callees|impact> <argument>": {"code": 0, "stdout": "..."}}}
//
// rg looks the answer up by the pattern after `--` ("-w:" first when --word-regexp is present);
// an entry that is missing is "no matches" (exit 1).
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type answer struct {
	Code   int    `json:"code"`
	Stdout string `json:"stdout"`
}

func main() {
	name := strings.TrimSuffix(strings.ToLower(filepath.Base(os.Args[0])), ".exe")
	args := os.Args[1:]
	if len(args) == 1 && args[0] == "--version" {
		if name == "rg" {
			fmt.Println("ripgrep 0.0.0-golden")
		} else {
			fmt.Println("codegraph 0.0.0-golden")
		}
		return
	}
	data, err := os.ReadFile(os.Getenv("NXY_GOLDEN_LOCATE_TABLE"))
	if err != nil {
		os.Exit(2)
	}
	var table map[string]map[string]answer
	if json.Unmarshal(data, &table) != nil {
		os.Exit(2)
	}
	var key string
	switch name {
	case "rg":
		word := false
		for i, a := range args {
			if a == "--word-regexp" {
				word = true
			}
			if a == "--" && i+1 < len(args) {
				key = args[i+1]
				break
			}
		}
		if word {
			key = "-w:" + key
		}
	default:
		if len(args) >= 2 {
			key = args[0] + " " + args[1]
		}
	}
	a, ok := table[name][key]
	if !ok {
		os.Exit(1)
	}
	fmt.Print(a.Stdout)
	os.Exit(a.Code)
}
