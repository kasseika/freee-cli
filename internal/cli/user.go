// user.go は `freee-hr user` コマンド群を実装します。
package cli

import (
	"github.com/spf13/cobra"
)

func (a *App) newUserCmd(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "user",
		Short: "ログインユーザーの情報",
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "me",
		Short: "ログインユーザーと所属する事業所（事業所ID・従業員ID）を取得します",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, _, err := a.newClient(flags.profile)
			if err != nil {
				return err
			}
			ctx, cancel := timeoutContext(cmd.Context(), flags)
			defer cancel()
			raw, err := client.GetMe(ctx)
			if err != nil {
				return err
			}
			return writeJSON(a.Stdout, raw)
		},
	})
	return cmd
}
