// Command posctl is the operator CLI: config checks now; migrations,
// preflight, provisioning, bootstrap and exports arrive in later steps.
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/tejas1112001/POS_inventory/backend-go/internal/buildinfo"
	"github.com/tejas1112001/POS_inventory/backend-go/internal/config"
)

const usage = `usage: posctl <command>

commands:
  version        print the build version
  config check   validate the environment (prints variable names only)
`

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch {
	case args[0] == "version":
		fmt.Fprintln(stdout, buildinfo.Version, buildinfo.Commit)
		return 0
	case args[0] == "config" && len(args) == 2 && args[1] == "check":
		if _, err := config.Load(); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintln(stdout, "config ok")
		return 0
	default:
		fmt.Fprint(stderr, usage)
		return 2
	}
}
