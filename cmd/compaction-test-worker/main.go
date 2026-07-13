// compaction-test-worker is a compiled subprocess fixture for cross-process claim tests.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
)

type command struct {
	Phase       string `json:"phase"`
	OperationID string `json:"operation_id"`
	BranchID    string `json:"branch_id"`
}
type event struct {
	Phase       string `json:"phase"`
	OperationID string `json:"operation_id"`
	BranchID    string `json:"branch_id"`
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	enc := json.NewEncoder(os.Stdout)
	for scanner.Scan() {
		var c command
		if err := json.Unmarshal(scanner.Bytes(), &c); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		if err := enc.Encode(event(c)); err != nil {
			os.Exit(3)
		}
	}
	if err := scanner.Err(); err != nil {
		os.Exit(4)
	}
}
