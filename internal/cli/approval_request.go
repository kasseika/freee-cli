// approval_request.go は `freee-hr approval <種類> list / get / create` と `approval overtime-work setting` を実装します。
//
// 申請の種類（コマンド名）は次の5つです。
//   - work-time:          勤務時間修正申請（create <YYYY-MM-DD>）
//   - paid-holiday:       有給休暇申請（create <YYYY-MM-DD>）
//   - special-holiday:    特別休暇申請（create <YYYY-MM-DD>）
//   - overtime-work:      残業申請（create <YYYY-MM-DD>。設定の確認は setting --date）
//   - monthly-attendance: 月次勤怠締め申請（create <YYYY-MM>）
//
// 申請の作成では、申請経路ID（--route-id。`approval route list` で調べる）が必須です。
// 申請時に承認者を選ぶ型の経路（ステップの resource_type が selected_user）では、承認者のユーザーID（--approver-id）も必要です。
// 承認者のIDは従業員IDではなくユーザーIDです（自分のユーザーIDは `freee-hr user me` の id）。
//
// よく使う項目はフラグで指定し、それ以外は --file の JSON（API の本文の形）で指定します。フラグの値はファイルの値より優先し、
// company_id は常にプロファイルの事業所にします。送信は打刻と同じ安全策（write.go の --dry-run・確認・読み取り専用）を通ります。
package cli

import (
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/kasseika/freee-cli/internal/freeehr"
)

// approvalListMaxLimit は申請の一覧で一度に取得できる件数の上限です（API の仕様）。
const approvalListMaxLimit = 100

// approvalCreateFlags は申請の作成のフラグの値です。種類ごとに使うフラグだけをコマンドに登録します。
type approvalCreateFlags struct {
	routeID    int
	approverID int
	comment    string
	file       string
	write      writeFlags

	// 勤務時間修正申請
	clockIn  string
	clockOut string
	breaks   []string
	clear    bool
	// 有給休暇・特別休暇・残業（反映なし）の開始・終了の時刻
	start string
	end   string
	// 有給休暇・特別休暇の取得単位
	holidayType string
	// 特別休暇の設定ID
	settingID int
	// 残業（勤怠に反映する設定）の早出・残業の時刻
	earlyStart    string
	earlyEnd      string
	overtimeStart string
	overtimeEnd   string
}

// approvalKind は、申請の種類ごとのコマンド名・API の種類と、作成の本文の組み立て方です。
type approvalKind struct {
	use   string
	kind  freeehr.ApprovalRequestKind
	label string
	// monthly は、対象を日（YYYY-MM-DD）ではなく年月（YYYY-MM）で指定する種類であることを表します。
	monthly bool
	// addFlags は種類に固有のフラグを登録します。
	addFlags func(cmd *cobra.Command, f *approvalCreateFlags)
	// build は種類に固有の項目をフラグから本文に書き込みます。hasFile は --file を指定したかどうかです。
	build func(cmd *cobra.Command, f *approvalCreateFlags, body map[string]any, hasFile bool) error
}

// approvalKinds は、CLI で扱う申請の種類の一覧です。
var approvalKinds = []approvalKind{
	{use: "work-time", kind: freeehr.WorkTimeRequest, label: "勤務時間修正申請", addFlags: addWorkTimeFlags, build: buildWorkTimeBody},
	{use: "paid-holiday", kind: freeehr.PaidHolidayRequest, label: "有給休暇申請", addFlags: addPaidHolidayFlags, build: buildPaidHolidayBody},
	{use: "special-holiday", kind: freeehr.SpecialHolidayRequest, label: "特別休暇申請", addFlags: addSpecialHolidayFlags, build: buildSpecialHolidayBody},
	{use: "overtime-work", kind: freeehr.OvertimeWorkRequest, label: "残業申請", addFlags: addOvertimeWorkFlags, build: buildOvertimeWorkBody},
	{use: "monthly-attendance", kind: freeehr.MonthlyAttendanceRequest, label: "月次勤怠締め申請", monthly: true,
		addFlags: func(*cobra.Command, *approvalCreateFlags) {}, build: func(*cobra.Command, *approvalCreateFlags, map[string]any, bool) error { return nil }},
}

// newApprovalKindCmd は、申請の種類ごとのコマンド群（list・get・create）を返します。
func (a *App) newApprovalKindCmd(flags *globalFlags, k approvalKind) *cobra.Command {
	cmd := newGroupCmd(k.use, k.label)
	cmd.AddCommand(a.newApprovalListCmd(flags, k), a.newApprovalGetCmd(flags, k), a.newApprovalCreateCmd(flags, k))
	if k.kind == freeehr.OvertimeWorkRequest {
		cmd.AddCommand(a.newOvertimeWorkSettingCmd(flags))
	}
	return cmd
}

func (a *App) newApprovalListCmd(flags *globalFlags, k approvalKind) *cobra.Command {
	var status, from, to string
	var applicantID, limit, offset int
	cmd := &cobra.Command{
		Use:   "list",
		Short: k.label + "の一覧を表示します",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts := freeehr.ListApprovalRequestsOptions{}
			if status != "" && !slices.Contains(freeehr.ApprovalRequestStatuses, status) {
				return usageErrorf("--status は %s のいずれかで指定してください: %q", strings.Join(freeehr.ApprovalRequestStatuses, "・"), status)
			}
			opts.Status = status
			var err error
			if opts.StartTargetDate, err = parseOptionalDate("--from", from); err != nil {
				return err
			}
			if opts.EndTargetDate, err = parseOptionalDate("--to", to); err != nil {
				return err
			}
			if cmd.Flags().Changed("applicant-id") {
				if applicantID <= 0 {
					return usageErrorf("--applicant-id には1以上のユーザーIDを指定してください: %d", applicantID)
				}
				opts.ApplicantID = &applicantID
			}
			if cmd.Flags().Changed("limit") {
				if limit < 1 || limit > approvalListMaxLimit {
					return usageErrorf("--limit は1〜%dで指定してください: %d", approvalListMaxLimit, limit)
				}
				opts.Limit = &limit
			}
			if cmd.Flags().Changed("offset") {
				if offset < 0 {
					return usageErrorf("--offset は0以上で指定してください: %d", offset)
				}
				opts.Offset = &offset
			}
			s, err := a.openCompanySession(flags)
			if err != nil {
				return err
			}
			return a.sendRead(cmd, flags, s, freeehr.ListApprovalRequestsRequest(s.creds.CompanyID, k.kind, opts))
		},
	}
	cmd.Flags().StringVar(&status, "status", "", "申請の状態（draft: 下書き、in_progress: 申請中、approved: 承認済、feedback: 差戻し）")
	cmd.Flags().StringVar(&from, "from", "", "対象日の期間の開始日（YYYY-MM-DD）")
	cmd.Flags().StringVar(&to, "to", "", "対象日の期間の終了日（YYYY-MM-DD）")
	cmd.Flags().IntVar(&applicantID, "applicant-id", 0, "申請者のユーザーID（従業員IDではない。自分のユーザーIDは `freee-hr user me` の id）")
	cmd.Flags().IntVar(&limit, "limit", 0, "取得件数（1〜100。省略時は50）")
	cmd.Flags().IntVar(&offset, "offset", 0, "取得開始位置（省略時は0）")
	return cmd
}

func (a *App) newApprovalGetCmd(flags *globalFlags, k approvalKind) *cobra.Command {
	return &cobra.Command{
		Use:   "get <id>",
		Short: k.label + "の詳細を表示します",
		Args:  usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := parseID("申請ID", args[0])
			if err != nil {
				return err
			}
			s, err := a.openCompanySession(flags)
			if err != nil {
				return err
			}
			return a.sendRead(cmd, flags, s, freeehr.ApprovalRequestRequest(s.creds.CompanyID, k.kind, id))
		},
	}
}

func (a *App) newApprovalCreateCmd(flags *globalFlags, k approvalKind) *cobra.Command {
	f := &approvalCreateFlags{}
	target := "<YYYY-MM-DD>"
	if k.monthly {
		target = "<YYYY-MM>"
	}
	cmd := &cobra.Command{
		Use:   "create " + target,
		Short: k.label + "を作成します（--route-id が必須）",
		Long: k.label + "を作成します。\n" +
			"--route-id には `freee-hr approval route list` で調べた申請経路IDを指定します。\n" +
			"申請時に承認者を選ぶ型の経路（`approval route get` のステップの resource_type が selected_user）では、\n" +
			"--approver-id に承認者のユーザーID（従業員IDではない）を指定します。\n" +
			"フラグに無い項目は --file に API の本文の形の JSON を指定します（\"-\" で標準入力）。フラグの値が優先されます。\n" +
			"送信の前に確認を求めます。--dry-run で送る予定の内容だけを表示し、--yes で確認を省略します。",
		Args: usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := f.write.validate(); err != nil {
				return err
			}
			body, err := a.buildApprovalCreateBody(cmd, k, args[0], f)
			if err != nil {
				return err
			}
			s, err := a.openCompanySession(flags)
			if err != nil {
				return err
			}
			if err := setCompanyID(body, s.creds.CompanyID); err != nil {
				return err
			}
			return a.sendWrite(cmd.Context(), flags, s, &f.write, freeehr.CreateApprovalRequestRequest(k.kind, body))
		},
	}
	addWriteFlags(cmd, &f.write)
	cmd.Flags().IntVar(&f.routeID, "route-id", 0, "申請経路ID（必須。`freee-hr approval route list` で調べる。--file の approval_flow_route_id でもよい）")
	cmd.Flags().IntVar(&f.approverID, "approver-id", 0, "承認者のユーザーID（従業員IDではない。申請時に承認者を選ぶ型の経路で必要）")
	cmd.Flags().StringVar(&f.comment, "comment", "", "申請理由")
	cmd.Flags().StringVar(&f.file, "file", "", "API の本文の形の JSON ファイル（\"-\" で標準入力）。フラグの値が優先される")
	k.addFlags(cmd, f)
	return cmd
}

// buildApprovalCreateBody は、--file の JSON とフラグから申請の作成の本文を組み立てます（company_id は含めません）。
func (a *App) buildApprovalCreateBody(cmd *cobra.Command, k approvalKind, target string, f *approvalCreateFlags) (map[string]any, error) {
	body := map[string]any{}
	if f.file != "" {
		var err error
		if body, err = readJSONObject(a.Stdin, f.file); err != nil {
			return nil, err
		}
	}

	if k.monthly {
		year, month, err := parseYearMonth("年月", target)
		if err != nil {
			return nil, err
		}
		body["target_year"] = year
		body["target_month"] = month
	} else {
		date, err := parseDate("対象日", target)
		if err != nil {
			return nil, err
		}
		body["target_date"] = date
	}

	if err := k.build(cmd, f, body, f.file != ""); err != nil {
		return nil, err
	}

	if cmd.Flags().Changed("route-id") {
		if f.routeID <= 0 {
			return nil, usageErrorf("--route-id には1以上の申請経路IDを指定してください: %d", f.routeID)
		}
		body["approval_flow_route_id"] = f.routeID
	}
	if _, ok := body["approval_flow_route_id"]; !ok {
		return nil, usageErrorf("--route-id で申請経路IDを指定してください（`freee-hr approval route list` で調べられます）")
	}
	if cmd.Flags().Changed("approver-id") {
		if f.approverID <= 0 {
			return nil, usageErrorf("--approver-id には1以上のユーザーIDを指定してください: %d", f.approverID)
		}
		body["approver_id"] = f.approverID
	}
	if cmd.Flags().Changed("comment") {
		body["comment"] = f.comment
	}
	return body, nil
}

func addWorkTimeFlags(cmd *cobra.Command, f *approvalCreateFlags) {
	cmd.Flags().StringVar(&f.clockIn, "clock-in", "", "出勤の時刻（HH:MM）")
	cmd.Flags().StringVar(&f.clockOut, "clock-out", "", "退勤の時刻（HH:MM）")
	cmd.Flags().StringArrayVar(&f.breaks, "break", nil, "休憩（HH:MM-HH:MM）。複数回指定できる")
	cmd.Flags().BoolVar(&f.clear, "clear", false, "勤務時間を削除する申請にする（出退勤・休憩とは同時に指定できない）")
}

// buildWorkTimeBody は勤務時間修正申請の出退勤・休憩、または勤務時間の削除を本文に書き込みます。
func buildWorkTimeBody(cmd *cobra.Command, f *approvalCreateFlags, body map[string]any, hasFile bool) error {
	if (f.clockIn == "") != (f.clockOut == "") {
		return usageErrorf("--clock-in と --clock-out は両方を指定してください")
	}
	if f.clear && (f.clockIn != "" || len(f.breaks) > 0) {
		return usageErrorf("--clear は --clock-in・--clock-out・--break と同時に指定できません")
	}
	if f.clear {
		body["clear_work_time"] = true
		return nil
	}
	if f.clockIn != "" {
		in, err := parseHHMM("--clock-in", f.clockIn)
		if err != nil {
			return err
		}
		out, err := parseHHMM("--clock-out", f.clockOut)
		if err != nil {
			return err
		}
		body["work_records"] = []timeRange{{ClockInAt: in, ClockOutAt: out}}
	}
	if len(f.breaks) > 0 {
		breaks := make([]timeRange, 0, len(f.breaks))
		for _, b := range f.breaks {
			start, end, ok := strings.Cut(b, "-")
			if !ok || !isHHMM(start) || !isHHMM(end) {
				return usageErrorf("--break は HH:MM-HH:MM の形式で指定してください: %q", b)
			}
			breaks = append(breaks, timeRange{ClockInAt: start, ClockOutAt: end})
		}
		body["break_records"] = breaks
	}
	if !hasFile && f.clockIn == "" && len(f.breaks) == 0 {
		return usageErrorf("修正後の勤務時間を --clock-in・--clock-out（と --break）で指定するか、削除する場合は --clear を指定してください")
	}
	return nil
}

// paidHolidayTypes は有給休暇申請の取得単位です（full: 全休、half: 半休、morning: 午前休、afternoon: 午後休、hourly: 時間休）。
var paidHolidayTypes = []string{"full", "half", "morning", "afternoon", "hourly"}

func addPaidHolidayFlags(cmd *cobra.Command, f *approvalCreateFlags) {
	cmd.Flags().StringVar(&f.holidayType, "type", "", "取得単位（full: 全休、half: 半休、morning: 午前休、afternoon: 午後休、hourly: 時間休）")
	addStartEndFlags(cmd, f, "取得予定の開始時刻（HH:MM。時間休では必須）", "取得予定の終了時刻（HH:MM。時間休では必須）")
}

// buildPaidHolidayBody は有給休暇申請の取得単位と時刻を、本文の values（1件）に書き込みます。
func buildPaidHolidayBody(cmd *cobra.Command, f *approvalCreateFlags, body map[string]any, hasFile bool) error {
	if f.holidayType == "" {
		if hasFile {
			return nil
		}
		return usageErrorf("--type で取得単位（%s）を指定してください", strings.Join(paidHolidayTypes, "・"))
	}
	if !slices.Contains(paidHolidayTypes, f.holidayType) {
		return usageErrorf("--type は %s のいずれかで指定してください: %q", strings.Join(paidHolidayTypes, "・"), f.holidayType)
	}
	value := map[string]any{"type": f.holidayType}
	if err := setStartEnd(f, value, "start_at", "end_at", f.holidayType == "hourly"); err != nil {
		return err
	}
	body["values"] = []map[string]any{value}
	return nil
}

// specialHolidayTypes は特別休暇申請の取得単位です（full: 全休、half: 半休、morning: 午前休、afternoon: 午後休、hour: 時間休）。
var specialHolidayTypes = []string{"full", "half", "morning", "afternoon", "hour"}

func addSpecialHolidayFlags(cmd *cobra.Command, f *approvalCreateFlags) {
	cmd.Flags().IntVar(&f.settingID, "setting-id", 0, "特別休暇設定ID（必須）")
	cmd.Flags().StringVar(&f.holidayType, "type", "", "取得単位（full: 全休、half: 半休、morning: 午前休、afternoon: 午後休、hour: 時間休）")
	addStartEndFlags(cmd, f, "取得予定の開始時刻（HH:MM。半休・時間休では必須）", "取得予定の終了時刻（HH:MM。半休・時間休では必須）")
}

// buildSpecialHolidayBody は特別休暇申請の設定ID・取得単位・時刻を本文に書き込みます。
func buildSpecialHolidayBody(cmd *cobra.Command, f *approvalCreateFlags, body map[string]any, hasFile bool) error {
	if cmd.Flags().Changed("setting-id") {
		if f.settingID <= 0 {
			return usageErrorf("--setting-id には1以上の特別休暇設定IDを指定してください: %d", f.settingID)
		}
		body["special_holiday_setting_id"] = f.settingID
	}
	if f.holidayType != "" {
		if !slices.Contains(specialHolidayTypes, f.holidayType) {
			return usageErrorf("--type は %s のいずれかで指定してください: %q", strings.Join(specialHolidayTypes, "・"), f.holidayType)
		}
		body["holiday_type"] = f.holidayType
	}
	needsTime := f.holidayType == "half" || f.holidayType == "hour"
	if err := setStartEnd(f, body, "start_at", "end_at", needsTime); err != nil {
		return err
	}
	if _, ok := body["special_holiday_setting_id"]; !ok {
		return usageErrorf("--setting-id で特別休暇設定IDを指定してください")
	}
	if _, ok := body["holiday_type"]; !ok {
		return usageErrorf("--type で取得単位（%s）を指定してください", strings.Join(specialHolidayTypes, "・"))
	}
	return nil
}

func addOvertimeWorkFlags(cmd *cobra.Command, f *approvalCreateFlags) {
	addStartEndFlags(cmd, f, "残業の開始時刻（HH:MM。勤怠に反映しない設定の場合）", "残業の終了時刻（HH:MM。勤怠に反映しない設定の場合）")
	cmd.Flags().StringVar(&f.earlyStart, "early-start", "", "早出の開始時刻（HH:MM。勤怠に反映する設定の場合）")
	cmd.Flags().StringVar(&f.earlyEnd, "early-end", "", "早出の終了時刻（HH:MM。所定の出勤時刻と同じにする）")
	cmd.Flags().StringVar(&f.overtimeStart, "overtime-start", "", "残業の開始時刻（HH:MM。所定の退勤時刻と同じにする。勤怠に反映する設定の場合）")
	cmd.Flags().StringVar(&f.overtimeEnd, "overtime-end", "", "残業の終了時刻（HH:MM。勤怠に反映する設定の場合）")
}

// buildOvertimeWorkBody は残業申請の時刻を本文に書き込みます。
// どの時刻を使うかは事業所の設定（`approval overtime-work setting` の should_reflect_in_work_record）で決まります。
// 勤怠に反映する設定で早出・残業をどちらも省くと、対象日の承認済み残業申請をすべて取り消す申請になるため、
// フラグだけで作る場合はいずれかの時刻を必須にします（取り消しの申請は --file で明示的に作ります）。
func buildOvertimeWorkBody(cmd *cobra.Command, f *approvalCreateFlags, body map[string]any, hasFile bool) error {
	pairs := []struct {
		startFlag, endFlag, startKey, endKey, start, end string
	}{
		{"--start", "--end", "start_at", "end_at", f.start, f.end},
		{"--early-start", "--early-end", "early_work_start_at", "early_work_end_at", f.earlyStart, f.earlyEnd},
		{"--overtime-start", "--overtime-end", "overtime_work_start_at", "overtime_work_end_at", f.overtimeStart, f.overtimeEnd},
	}
	specified := false
	for _, p := range pairs {
		if p.start == "" && p.end == "" {
			continue
		}
		if p.start == "" || p.end == "" {
			return usageErrorf("%s と %s は両方を指定してください", p.startFlag, p.endFlag)
		}
		start, err := parseHHMM(p.startFlag, p.start)
		if err != nil {
			return err
		}
		end, err := parseHHMM(p.endFlag, p.end)
		if err != nil {
			return err
		}
		body[p.startKey] = start
		body[p.endKey] = end
		specified = true
	}
	if !specified && !hasFile {
		return usageErrorf("残業の時刻を --overtime-start/--overtime-end・--early-start/--early-end（勤怠に反映する設定）か --start/--end（反映しない設定）で指定してください。どちらの設定かは `freee-hr approval overtime-work setting --date <日付>` で確認できます")
	}
	return nil
}

func (a *App) newOvertimeWorkSettingCmd(flags *globalFlags) *cobra.Command {
	var date string
	cmd := &cobra.Command{
		Use:   "setting",
		Short: "指定日の残業申請に必要な設定（勤怠に反映するか・所定の出退勤時刻・分の単位）を表示します",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if date == "" {
				return usageErrorf("--date で対象日（YYYY-MM-DD）を指定してください")
			}
			d, err := parseDate("--date", date)
			if err != nil {
				return err
			}
			s, err := a.openCompanySession(flags)
			if err != nil {
				return err
			}
			return a.sendRead(cmd, flags, s, freeehr.OvertimeWorkSettingRequest(s.creds.CompanyID, d))
		},
	}
	cmd.Flags().StringVar(&date, "date", "", "対象日（YYYY-MM-DD。必須）")
	return cmd
}

// addStartEndFlags は --start と --end を登録します。
func addStartEndFlags(cmd *cobra.Command, f *approvalCreateFlags, startUsage, endUsage string) {
	cmd.Flags().StringVar(&f.start, "start", "", startUsage)
	cmd.Flags().StringVar(&f.end, "end", "", endUsage)
}

// setStartEnd は --start と --end の時刻を dst に書き込みます。required が true の場合は両方を必須にします。
func setStartEnd(f *approvalCreateFlags, dst map[string]any, startKey, endKey string, required bool) error {
	if (f.start == "") != (f.end == "") || (required && f.start == "") {
		return usageErrorf("--start と --end は両方を指定してください（半休・時間休では必須です）")
	}
	if f.start == "" {
		return nil
	}
	start, err := parseHHMM("--start", f.start)
	if err != nil {
		return err
	}
	end, err := parseHHMM("--end", f.end)
	if err != nil {
		return err
	}
	dst[startKey] = start
	dst[endKey] = end
	return nil
}

// parseHHMM は HH:MM の時刻を検証し、そのまま返します。
func parseHHMM(name, value string) (string, error) {
	if !isHHMM(value) {
		return "", usageErrorf("%s は HH:MM の形式で指定してください: %q", name, value)
	}
	return value, nil
}

// isHHMM は value が2桁の時と分による HH:MM の時刻（00:00〜23:59）であるかを返します。
func isHHMM(value string) bool {
	if len(value) != len(clockTimeLayout) {
		return false
	}
	_, err := time.Parse(clockTimeLayout, value)
	return err == nil
}
