package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

type testEvent struct {
	Action  string  `json:"Action"`
	Package string  `json:"Package"`
	Test    string  `json:"Test"`
	Output  string  `json:"Output"`
	Elapsed float64 `json:"Elapsed"`
}

const (
	green  = "\033[32m"
	red    = "\033[31m"
	yellow = "\033[33m"
	dim    = "\033[90m"
	bold   = "\033[1m"
	reset  = "\033[0m"
)

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	type result struct {
		name    string
		action  string
		elapsed float64
		output  []string
	}

	results := map[string]*result{}
	var order []string
	var passed, failed, skipped int
	start := time.Now()

	for scanner.Scan() {
		var ev testEvent
		if json.Unmarshal(scanner.Bytes(), &ev) != nil {
			continue
		}

		if ev.Test == "" {
			continue
		}

		if _, exists := results[ev.Test]; !exists {
			results[ev.Test] = &result{name: ev.Test}
			order = append(order, ev.Test)
		}
		r := results[ev.Test]

		switch ev.Action {
		case "pass":
			r.action = "pass"
			r.elapsed = ev.Elapsed
			passed++
		case "fail":
			r.action = "fail"
			r.elapsed = ev.Elapsed
			failed++
		case "skip":
			r.action = "skip"
			r.elapsed = ev.Elapsed
			skipped++
		case "output":
			line := strings.TrimRight(ev.Output, "\n")
			if line != "" &&
				!strings.HasPrefix(line, "=== RUN") &&
				!strings.HasPrefix(line, "=== PAUSE") &&
				!strings.HasPrefix(line, "=== CONT") &&
				!strings.HasPrefix(line, "--- PASS") &&
				!strings.HasPrefix(line, "--- FAIL") &&
				!strings.HasPrefix(line, "--- SKIP") {
				r.output = append(r.output, line)
			}
		}
	}

	elapsed := time.Since(start)

	fmt.Println()
	for _, name := range order {
		r := results[name]
		switch r.action {
		case "pass":
			fmt.Printf("  %s✓%s %s %s%.2fs%s\n", green, reset, r.name, dim, r.elapsed, reset)
		case "fail":
			fmt.Printf("  %s✗%s %s %s%.2fs%s\n", red, reset, r.name, dim, r.elapsed, reset)
			for _, line := range r.output {
				fmt.Printf("    %s%s%s\n", red, line, reset)
			}
		case "skip":
			fmt.Printf("  %s○%s %s %s(skipped)%s\n", yellow, reset, r.name, dim, reset)
			for _, line := range r.output {
				fmt.Printf("    %s%s%s\n", dim, line, reset)
			}
		}
	}

	total := passed + failed + skipped
	fmt.Println()
	if failed > 0 {
		fmt.Printf("  %s%s%d failed%s", bold, red, failed, reset)
		fmt.Printf(", %d passed", passed)
	} else {
		fmt.Printf("  %s%s%d passed%s", bold, green, passed, reset)
	}
	if skipped > 0 {
		fmt.Printf(", %s%d skipped%s", yellow, skipped, reset)
	}
	fmt.Printf(" %s(%d tests in %.1fs)%s\n\n", dim, total, elapsed.Seconds(), reset)

	if failed > 0 {
		os.Exit(1)
	}
}
