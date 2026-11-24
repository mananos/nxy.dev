package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/mananos/nxy.dev/v2/internal/platform"
)

const actionUsage = "uso: nxy action decide --ask <id> --answer <etiqueta> [--answer <etiqueta> ...] | decide --ask <id> --release"

// runAction is `nxy action decide`: the user's answer, from the TUI, to a question the hook holds.
// It is not for the model: the PreToolUse hook refuses the command.
func runAction(args []string, _ io.Reader, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "pick" {
		return runActionPick(args[1:], stdout, stderr)
	}
	if len(args) == 0 || args[0] != "decide" {
		fmt.Fprintln(stderr, actionUsage)
		return 1
	}
	var askID string
	var answers []string
	release := false
	rest := args[1:]
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case "--release":
			release = true
		case "--ask", "--answer":
			if i+1 >= len(rest) {
				fmt.Fprintln(stderr, actionUsage)
				return 1
			}
			i++
			if rest[i-1] == "--ask" {
				askID = rest[i]
			} else {
				answers = append(answers, rest[i])
			}
		default:
			fmt.Fprintln(stderr, actionUsage)
			return 1
		}
	}
	if askID == "" || (release && len(answers) > 0) || (!release && len(answers) == 0) {
		fmt.Fprintln(stderr, actionUsage)
		return 1
	}
	if !tuiProof(os.Getenv(platform.NonceEnv)) {
		fmt.Fprintln(stderr, "nxy action decide: sólo lo corre la TUI de nxy (abrila con nxy tui y contestá ahí)")
		return 1
	}
	h, st := newHooksStore()
	defer st.Close()
	if err := h.Decide(context.Background(), askID, answers, release); err != nil {
		fmt.Fprintf(stderr, "nxy action decide: %s\n", oneLine(err.Error()))
		return 1
	}
	if release {
		fmt.Fprintln(stdout, "la pregunta sigue en la terminal de Claude Code")
	} else {
		fmt.Fprintln(stdout, "contestada")
	}
	return 0
}

const pickUsage = "uso: nxy action pick --feature <id> --fork <B> --option <etiqueta>"

// runActionPick is `nxy action pick`: the option the user chose in the TUI for the open fork; Claude
// gets it in the next turn. Like decide, only the TUI runs it.
func runActionPick(rest []string, stdout, stderr io.Writer) int {
	var feature, fork, option string
	for i := 0; i < len(rest); i += 2 {
		if i+1 >= len(rest) {
			fmt.Fprintln(stderr, pickUsage)
			return 1
		}
		switch rest[i] {
		case "--feature":
			feature = rest[i+1]
		case "--fork":
			fork = rest[i+1]
		case "--option":
			option = rest[i+1]
		default:
			fmt.Fprintln(stderr, pickUsage)
			return 1
		}
	}
	if feature == "" || fork == "" || option == "" {
		fmt.Fprintln(stderr, pickUsage)
		return 1
	}
	if !tuiProof(os.Getenv(platform.NonceEnv)) {
		fmt.Fprintln(stderr, "nxy action pick: sólo lo corre la TUI de nxy (abrila con nxy tui y elegí ahí)")
		return 1
	}
	h, st := newHooksStore()
	defer st.Close()
	res, err := h.PickFork(context.Background(), feature, fork, option)
	if err != nil {
		fmt.Fprintf(stderr, "nxy action pick: %s\n", oneLine(err.Error()))
		return 1
	}
	if len(res.Problems) > 0 {
		fmt.Fprintf(stderr, "nxy action pick: %s: %s\n", res.Problems[0].Where, oneLine(res.Problems[0].Msg))
		return 1
	}
	fmt.Fprintln(stdout, "guardada")
	return 0
}

// tuiProof: the nonce of a live TUI, which only its subprocesses carry.
func tuiProof(nonce string) bool {
	dir, err := platform.RunDir()
	return err == nil && platform.TUINonceValid(dir, nonce, time.Now())
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
