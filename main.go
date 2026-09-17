// Command depwhy explains why a dependency is in your project, across npm,
// Cargo, Python (uv, Poetry) and Go.
package main

import (
	"os"

	"github.com/Mr-hunt-007/depwhy/internal/cli"
)

func main() {
	env := cli.Env{NoColorEnv: os.Getenv("NO_COLOR") != ""}
	if st, err := os.Stdout.Stat(); err == nil && st.Mode()&os.ModeCharDevice != 0 {
		env.StdoutIsTTY = true
	}
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr, env))
}
