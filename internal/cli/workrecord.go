// workrecord.go は `freee-hr work-record` コマンド群（勤怠の確認）を実装します。
//
//   - get <YYYY-MM-DD>:  日ごとの勤怠記録を表示します
//   - summary <YYYY-MM>: 月の集計（労働時間・残業・有給の残りなど）を表示します。年月は給与支払い年月です
//   - update <YYYY-MM-DD>: 日ごとの勤怠記録を登録・更新します（workrecord_update.go）
package cli

import (
	"github.com/spf13/cobra"

	"github.com/kasseika/freee-cli/internal/freeehr"
)

func (a *App) newWorkRecordCmd(flags *globalFlags) *cobra.Command {
	cmd := newGroupCmd("work-record", "勤怠の記録（日ごとの勤怠・月の集計・勤怠の修正）")
	cmd.AddCommand(a.newWorkRecordGetCmd(flags))
	cmd.AddCommand(a.newWorkRecordSummaryCmd(flags))
	cmd.AddCommand(a.newWorkRecordUpdateCmd(flags))
	return cmd
}

func (a *App) newWorkRecordGetCmd(flags *globalFlags) *cobra.Command {
	var employeeID int
	cmd := &cobra.Command{
		Use:   "get <YYYY-MM-DD>",
		Short: "指定日の勤怠記録を表示します",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			date, err := parseDate("日付", args[0])
			if err != nil {
				return err
			}
			s, err := a.openEmployeeSession(cmd, flags, employeeID)
			if err != nil {
				return err
			}
			return a.sendRead(cmd, flags, s, freeehr.WorkRecordRequest(s.creds.CompanyID, s.employeeID, date))
		},
	}
	addEmployeeIDFlag(cmd, &employeeID)
	return cmd
}

func (a *App) newWorkRecordSummaryCmd(flags *globalFlags) *cobra.Command {
	var employeeID int
	var withRecords bool
	cmd := &cobra.Command{
		Use:   "summary <YYYY-MM>",
		Short: "給与支払い年月を指定して、勤怠の集計（労働時間・残業・有給の残りなど）を表示します",
		Long: "勤怠の月次集計を表示します。引数の年月は給与支払い年月です（カレンダーの月ではありません）。\n" +
			"給与を翌月に支払う事業所では、前月分の勤怠になることがあります。実際の集計期間はレスポンスの start_date・end_date で確認してください。",
		Args: usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			year, month, err := parseYearMonth("年月", args[0])
			if err != nil {
				return err
			}
			s, err := a.openEmployeeSession(cmd, flags, employeeID)
			if err != nil {
				return err
			}
			return a.sendRead(cmd, flags, s, freeehr.WorkRecordSummaryRequest(s.creds.CompanyID, s.employeeID, year, month, withRecords))
		},
	}
	addEmployeeIDFlag(cmd, &employeeID)
	cmd.Flags().BoolVar(&withRecords, "with-records", false, "日ごとの勤怠記録も一緒に表示する")
	return cmd
}
