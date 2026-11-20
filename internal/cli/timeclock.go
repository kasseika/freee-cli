// timeclock.go は `freee-hr timeclock` コマンド群（打刻）を実装します。
//
//   - types: いま（または指定日に）打刻できる種類を表示します
//   - list:  打刻の一覧を表示します
//   - punch: 打刻を登録します（書き込み。--dry-run・確認・読み取り専用の安全策を通します）
package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/kasseika/freee-cli/internal/freeehr"
)

const (
	// timeClockListMaxLimit は打刻一覧で一度に取得できる件数の上限です（API の仕様）。
	timeClockListMaxLimit = 100
)

func (a *App) newTimeclockCmd(flags *globalFlags) *cobra.Command {
	cmd := newGroupCmd("timeclock", "打刻（出勤・退勤・休憩開始・休憩終了）")
	cmd.AddCommand(a.newTimeclockTypesCmd(flags))
	cmd.AddCommand(a.newTimeclockListCmd(flags))
	cmd.AddCommand(a.newTimeclockPunchCmd(flags))
	return cmd
}

func (a *App) newTimeclockTypesCmd(flags *globalFlags) *cobra.Command {
	var employeeID int
	var date string
	cmd := &cobra.Command{
		Use:   "types",
		Short: "いま打刻できる種類（clock_in / break_begin / break_end / clock_out）を表示します",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			date, err := parseOptionalDate("--date", date)
			if err != nil {
				return err
			}
			s, err := a.openEmployeeSession(cmd, flags, employeeID)
			if err != nil {
				return err
			}
			return a.sendRead(cmd, flags, s, freeehr.AvailableTimeClockTypesRequest(s.creds.CompanyID, s.employeeID, date))
		},
	}
	addEmployeeIDFlag(cmd, &employeeID)
	cmd.Flags().StringVar(&date, "date", "", "対象日（YYYY-MM-DD。省略時は当日）")
	return cmd
}

func (a *App) newTimeclockListCmd(flags *globalFlags) *cobra.Command {
	var employeeID, limit, offset int
	var from, to string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "打刻の一覧を表示します（既定は当月の打刻開始日から当日まで）",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := freeehr.ListTimeClocksOptions{}
			var err error
			if opts.FromDate, err = parseOptionalDate("--from", from); err != nil {
				return err
			}
			if opts.ToDate, err = parseOptionalDate("--to", to); err != nil {
				return err
			}
			if cmd.Flags().Changed("limit") {
				if limit < 1 || limit > timeClockListMaxLimit {
					return usageErrorf("--limit は1〜%dで指定してください: %d", timeClockListMaxLimit, limit)
				}
				opts.Limit = &limit
			}
			if cmd.Flags().Changed("offset") {
				if offset < 0 {
					return usageErrorf("--offset は0以上で指定してください: %d", offset)
				}
				opts.Offset = &offset
			}
			s, err := a.openEmployeeSession(cmd, flags, employeeID)
			if err != nil {
				return err
			}
			return a.sendRead(cmd, flags, s, freeehr.ListTimeClocksRequest(s.creds.CompanyID, s.employeeID, opts))
		},
	}
	addEmployeeIDFlag(cmd, &employeeID)
	cmd.Flags().StringVar(&from, "from", "", "期間の開始日（YYYY-MM-DD）")
	cmd.Flags().StringVar(&to, "to", "", "期間の終了日（YYYY-MM-DD）")
	cmd.Flags().IntVar(&limit, "limit", 0, "取得件数（1〜100。省略時は50）")
	cmd.Flags().IntVar(&offset, "offset", 0, "取得開始位置（省略時は0）")
	return cmd
}

func (a *App) newTimeclockPunchCmd(flags *globalFlags) *cobra.Command {
	var employeeID int
	var baseDate string
	var wf writeFlags
	types := strings.Join(freeehr.TimeClockTypes, " | ")
	cmd := &cobra.Command{
		Use:   "punch <" + types + ">",
		Short: "打刻を登録します（いまの時刻で打刻されます）",
		Long: "打刻を登録します。打刻の時刻は常に「いま」です。\n" +
			"日をまたぐ打刻（深夜の退勤など）では、--base-date に前日の日付を指定してください。\n" +
			"送信の前に確認を求めます。--dry-run で送る予定の内容だけを表示し、--yes で確認を省略します。",
		Args: usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := wf.validate(); err != nil {
				return err
			}
			punchType := args[0]
			if !freeehr.IsTimeClockType(punchType) {
				return usageErrorf("打刻の種類は %s のいずれかを指定してください: %q", types, punchType)
			}
			baseDate, err := parseOptionalDate("--base-date", baseDate)
			if err != nil {
				return err
			}
			s, err := a.openEmployeeSession(cmd, flags, employeeID)
			if err != nil {
				return err
			}
			req := freeehr.PunchRequest(s.employeeID, freeehr.PunchBody{CompanyID: s.creds.CompanyID, Type: punchType, BaseDate: baseDate})
			return a.sendWrite(cmd.Context(), flags, s, &wf, req)
		},
	}
	addEmployeeIDFlag(cmd, &employeeID)
	addWriteFlags(cmd, &wf)
	cmd.Flags().StringVar(&baseDate, "base-date", "", "打刻日（YYYY-MM-DD）。日をまたぐ打刻では前日を指定する")
	return cmd
}
