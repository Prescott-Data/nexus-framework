// Command nexus manages Nexus provider configuration declaratively:
// validate a manifest, plan the diff against a Broker, apply it.
//
//	go install github.com/Prescott-Data/nexus-framework/nexus-cli/cmd/nexus@latest
package main

import (
	"os"

	"github.com/Prescott-Data/nexus-framework/nexus-cli/internal/cli"
)

func main() {
	os.Exit(cli.Main(os.Args[1:]))
}
