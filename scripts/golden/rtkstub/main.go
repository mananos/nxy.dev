// Command rtkstub stands in for the real rtk in the golden tests: it answers `rewrite <cmd>`
// from a JSON table ($NXY_GOLDEN_RTK_TABLE: {"<asked>": {"code": N, "stdout": "..."}}) and
// `gain ... --format json` with the file in $NXY_GOLDEN_RTK_GAIN (exit 1 when unset).
package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type answer struct {
	Code   int    `json:"code"`
	Stdout string `json:"stdout"`
}

func main() {
	args := os.Args[1:]
	if len(args) == 1 && args[0] == "--version" {
		fmt.Println("rtk 0.0.0-golden")
		return
	}
	if len(args) >= 3 && args[0] == "gain" && args[len(args)-2] == "--format" && args[len(args)-1] == "json" {
		data, err := os.ReadFile(os.Getenv("NXY_GOLDEN_RTK_GAIN"))
		if err != nil {
			os.Exit(1)
		}
		fmt.Print(string(data))
		return
	}
	if len(args) == 2 && args[0] == "rewrite" {
		data, err := os.ReadFile(os.Getenv("NXY_GOLDEN_RTK_TABLE"))
		if err != nil {
			os.Exit(1)
		}
		var table map[string]answer
		if json.Unmarshal(data, &table) != nil {
			os.Exit(1)
		}
		a, ok := table[args[1]]
		if !ok {
			os.Exit(1)
		}
		fmt.Print(a.Stdout)
		os.Exit(a.Code)
	}
	os.Exit(1)
}
