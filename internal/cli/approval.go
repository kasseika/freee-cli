// approval.go は `freee-hr approval` コマンド群（申請と申請経路）を実装します。
//
//   - route list: 申請経路の一覧（既定は勤怠申請に使える経路。--all で全種別）
//   - route get <id>: 申請経路の詳細（承認のステップ）
//   - <種類> list / get / create: 申請の一覧・詳細・作成（approval_request.go）
package cli

import (
	"strconv"

	"github.com/spf13/cobra"

	"github.com/kasseika/freee-cli/internal/freeehr"
)

func (a *App) newApprovalCmd(flags *globalFlags) *cobra.Command {
	cmd := newGroupCmd("approval", "申請（勤怠・有給・残業など）と申請経路")
	cmd.AddCommand(a.newApprovalRouteCmd(flags))
	for _, k := range approvalKinds {
		cmd.AddCommand(a.newApprovalKindCmd(flags, k))
	}
	return cmd
}

func (a *App) newApprovalRouteCmd(flags *globalFlags) *cobra.Command {
	cmd := newGroupCmd("route", "申請経路（申請の作成に必要な approval_flow_route_id を調べる）")

	var all bool
	list := &cobra.Command{
		Use:   "list",
		Short: "申請経路の一覧を表示します（既定は勤怠申請に使える経路）",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := a.openCompanySession(flags)
			if err != nil {
				return err
			}
			usage := freeehr.AttendanceWorkflowUsage
			if all {
				usage = ""
			}
			return a.sendRead(cmd, flags, s, freeehr.ApprovalFlowRoutesRequest(s.creds.CompanyID, usage))
		},
	}
	list.Flags().BoolVar(&all, "all", false, "申請種別で絞り込まない（身上変更申請の経路も含める）")

	get := &cobra.Command{
		Use:   "get <id>",
		Short: "申請経路の詳細（承認のステップ）を表示します",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			routeID, err := parseID("申請経路ID", args[0])
			if err != nil {
				return err
			}
			s, err := a.openCompanySession(flags)
			if err != nil {
				return err
			}
			return a.sendRead(cmd, flags, s, freeehr.ApprovalFlowRouteRequest(s.creds.CompanyID, routeID))
		},
	}

	cmd.AddCommand(list, get)
	return cmd
}

// parseID は引数のIDを1以上の整数として解釈します。数値でない・0以下の場合は使い方の誤りにします。
func parseID(name, value string) (int, error) {
	id, err := strconv.Atoi(value)
	if err != nil || id <= 0 {
		return 0, usageErrorf("%s は1以上の数値で指定してください: %q", name, value)
	}
	return id, nil
}
