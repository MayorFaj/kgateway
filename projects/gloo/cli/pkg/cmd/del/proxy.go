package del

import (
	"fmt"

	"github.com/solo-io/gloo/projects/gloo/cli/pkg/common"
	"github.com/solo-io/go-utils/cliutils"

	"github.com/solo-io/gloo/projects/gloo/cli/pkg/cmd/options"
	"github.com/solo-io/gloo/projects/gloo/cli/pkg/helpers"
	"github.com/solo-io/solo-kit/pkg/api/v1/clients"
	"github.com/spf13/cobra"
)

func Proxy(opts *options.Options, optionsFunc ...cliutils.OptionsFunc) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "proxy",
		Aliases: []string{"p", "px", "proxies"},
		Short:   "delete a proxy",
		Long:    "usage: glooctl delete proxy [NAME] [--namespace=namespace]",
		RunE: func(cmd *cobra.Command, args []string) error {
			name := common.GetName(args, opts)
			if err := helpers.MustProxyClient().Delete(opts.Metadata.Namespace, name,
				clients.DeleteOpts{Ctx: opts.Top.Ctx}); err != nil {
				return err
			}
			fmt.Printf("proxy %v deleted", name)
			return nil
		},
	}
	cliutils.ApplyOptions(cmd, optionsFunc)
	return cmd
}
