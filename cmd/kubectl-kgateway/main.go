package main

import (
	"os"

	"github.com/kgateway-dev/kgateway/v2/pkg/cli/diagnose"
)

func main() {
	cmd := diagnose.NewDiagnoseCommand()
	cmd.Use = "kubectl-kgateway"
	cmd.Short = "kgateway diagnostics plugin for kubectl"
	cmd.Long = `kgateway diagnostics plugin for kubectl.

This plugin provides diagnostic capabilities for kgateway policies and configurations.
Install this plugin to diagnose policy attachment issues from any machine with kubectl access.

Examples:
  kubectl kgateway policy --all-namespaces
  kubectl kgateway policy gw-policy -n kgateway-test
  kubectl kgateway policy --help
`

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
