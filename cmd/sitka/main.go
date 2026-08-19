// Command sitka runs the personal AI gateway.
package main

import (
	"os"

	"github.com/1outres/sitka/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
