package main

import (
	"fmt"
	"os"
	"os/exec"
)

func main() {
	args := []string{"test", "-json", "-count=1"}
	if len(os.Args) > 1 {
		args = append(args, os.Args[1:]...)
	} else {
		args = append(args, "./tests/")
	}

	testCmd := exec.Command("go", args...)
	fmtCmd := exec.Command("go", "run", "./tests/fmt")

	pipe, err := testCmd.StdoutPipe()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create pipe: %v\n", err)
		os.Exit(1)
	}
	testCmd.Stderr = os.Stderr
	fmtCmd.Stdin = pipe
	fmtCmd.Stdout = os.Stdout
	fmtCmd.Stderr = os.Stderr

	if err := fmtCmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to start formatter: %v\n", err)
		os.Exit(1)
	}
	if err := testCmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to start tests: %v\n", err)
		os.Exit(1)
	}

	testCmd.Wait()
	if err := fmtCmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		os.Exit(1)
	}
}
