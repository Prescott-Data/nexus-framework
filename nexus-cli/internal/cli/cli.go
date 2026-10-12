// Package cli implements the nexus command: validate, plan and apply for
// provider manifests, with secrets resolved from the environment, a SOPS
// file or Google Secret Manager.
package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"runtime/debug"
	"strings"
)

// Version is stamped at build time (make cli). A binary from
// `go install ...@vX.Y.Z` reports that module version through build info;
// a plain go build in a checkout reports "dev".
var Version = "dev"

func init() {
	if Version != "dev" {
		return
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := strings.TrimPrefix(info.Main.Version, "v"); v != "" && v != "(devel)" {
			Version = v
		}
	}
}

const usageText = `Usage: nexus <command> [flags]

Commands:
  validate   Check a manifest offline: structure, required fields, duplicates
  plan       Show what apply would change on the Broker, without changing it
  apply      Create, update and (with -prune) delete providers on the Broker
  version    Print the CLI version

The manifest (-file) is either one YAML file with a top-level "providers"
list, or a directory holding one provider per <name>.yaml file.

Secrets referenced as ${PLACEHOLDER} in the manifest are resolved from
-secrets (or $NEXUS_SECRETS):
  env                          the process environment (default)
  sops:<path>                  a SOPS-encrypted file mapping placeholder to value
  gcp-secret-manager:<project> Google Secret Manager, one secret per placeholder
A placeholder the source does not hold falls back to the environment.

Run "nexus <command> -h" for the flags of a command.
`

// Main runs the CLI and returns the process exit code.
func Main(args []string) int {
	return run(args, os.Stdin, os.Stdout, os.Stderr)
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usageText)
		return 2
	}
	switch args[0] {
	case "validate":
		return runValidate(args[1:], stdout, stderr)
	case "plan":
		return runApply(args[1:], true, stdin, stdout, stderr)
	case "apply":
		return runApply(args[1:], false, stdin, stdout, stderr)
	case "version", "-version", "--version":
		fmt.Fprintf(stdout, "nexus %s\n", Version)
		return 0
	case "help", "-h", "-help", "--help":
		fmt.Fprint(stdout, usageText)
		return 0
	default:
		fmt.Fprintf(stderr, "nexus: unknown command %q\n\n%s", args[0], usageText)
		return 2
	}
}

// manifestFlags are the flags every command that reads a manifest shares.
type manifestFlags struct {
	file         string
	secrets      string
	secretPrefix string
}

func addManifestFlags(fs *flag.FlagSet, mf *manifestFlags) {
	fs.StringVar(&mf.file, "file", "nexus-providers.yaml", "Manifest file, or a directory of one provider per <name>.yaml")
	fs.StringVar(&mf.secrets, "secrets", os.Getenv("NEXUS_SECRETS"), "Secret source for ${PLACEHOLDER} values: env, sops:<path> or gcp-secret-manager:<project> (default $NEXUS_SECRETS, else env)")
	fs.StringVar(&mf.secretPrefix, "secret-prefix", os.Getenv("NEXUS_SECRET_PREFIX"), "gcp-secret-manager: prefix for derived secret ids (default $NEXUS_SECRET_PREFIX)")
}

// parseFlags parses with ContinueOnError so a bad flag returns an exit code
// instead of ending the process from inside the package.
func parseFlags(fs *flag.FlagSet, args []string, stderr io.Writer) (ok bool, code int) {
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return false, 0
		}
		return false, 2
	}
	return true, 0
}
