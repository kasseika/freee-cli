// workrecord_update.go は `freee-hr work-record update`（日ごとの勤怠記録の登録・更新）を実装します。
//
// よく使う項目はフラグで指定します。
//   - --clock-in / --clock-out: 出勤・退勤の時刻（HH:MM。日をまたぐ場合は YYYY-MM-DD HH:MM）
//   - --break HH:MM-HH:MM:      休憩（複数指定できます）。--no-break で「休憩なし」を明示します
//   - --note:                   勤怠メモ
//
// 休暇などフラグに無い項目は、--file の JSON（API の本文の形）で指定します。フラグで指定した項目は
// ファイルの値より優先します。company_id は常にプロファイルの事業所にします。
//
// 休憩を省いて更新した場合に既存の休憩が消えるかどうかは API のドキュメントに無いため、
// 出退勤を指定するときは、休憩（--break・--no-break・ファイルの break_records）の指定を必須にします。
// 書き込みの安全策（--dry-run・確認・読み取り専用）は打刻と同じ仕組み（write.go）を通ります。
package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/kasseika/freee-cli/internal/freeehr"
)

const (
	// clockTimeLayout は時刻だけの指定（HH:MM）の形式です。
	clockTimeLayout = "15:04"
	// clockDateTimeLayout は日付つきの時刻の指定（YYYY-MM-DD HH:MM）の形式です。
	clockDateTimeLayout = "2006-01-02 15:04"
	// apiDateTimeLayout は freee の勤怠の更新で使う日時の形式です。
	apiDateTimeLayout = "2006-01-02 15:04:05"
)

// workRecordUpdateFlags は `work-record update` のフラグの値です。
type workRecordUpdateFlags struct {
	employeeID int
	clockIn    string
	clockOut   string
	breaks     []string
	noBreak    bool
	note       string
	file       string
	write      writeFlags
}

func (a *App) newWorkRecordUpdateCmd(flags *globalFlags) *cobra.Command {
	f := &workRecordUpdateFlags{}
	cmd := &cobra.Command{
		Use:   "update <YYYY-MM-DD>",
		Short: "指定日の勤怠記録を登録・更新します（出退勤・休憩・メモ。休暇などは --file）",
		Long: "指定日の勤怠記録を登録・更新します。\n" +
			"出退勤を指定するときは、休憩を --break（複数可）か --no-break で必ず指定してください。\n" +
			"日をまたぐ退勤は --clock-out \"2026-11-21 02:00\" のように日付つきで指定します。\n" +
			"休暇など、フラグに無い項目は --file に API の本文の形の JSON を指定します（\"-\" で標準入力）。\n" +
			"送信の前に確認を求めます。--dry-run で送る予定の内容だけを表示し、--yes で確認を省略します。",
		Args: usageArgs(cobra.ExactArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := f.write.validate(); err != nil {
				return err
			}
			date, err := parseDate("日付", args[0])
			if err != nil {
				return err
			}
			body, err := a.buildWorkRecordUpdateBody(cmd, date, f)
			if err != nil {
				return err
			}
			s, err := a.openEmployeeSession(cmd, flags, f.employeeID)
			if err != nil {
				return err
			}
			if err := setCompanyID(body, s.creds.CompanyID); err != nil {
				return err
			}
			return a.sendWrite(cmd.Context(), flags, s, &f.write, freeehr.UpdateWorkRecordRequest(s.employeeID, date, body))
		},
	}
	addEmployeeIDFlag(cmd, &f.employeeID)
	addWriteFlags(cmd, &f.write)
	cmd.Flags().StringVar(&f.clockIn, "clock-in", "", "出勤の時刻（HH:MM、または YYYY-MM-DD HH:MM）")
	cmd.Flags().StringVar(&f.clockOut, "clock-out", "", "退勤の時刻（HH:MM。日をまたぐ場合は YYYY-MM-DD HH:MM）")
	cmd.Flags().StringArrayVar(&f.breaks, "break", nil, "休憩（HH:MM-HH:MM）。複数回指定できる")
	cmd.Flags().BoolVar(&f.noBreak, "no-break", false, "休憩なしとして登録する")
	cmd.Flags().StringVar(&f.note, "note", "", "勤怠メモ")
	cmd.Flags().StringVar(&f.file, "file", "", "API の本文の形の JSON ファイル（\"-\" で標準入力）。フラグの値が優先される")
	return cmd
}

// buildWorkRecordUpdateBody は、--file の JSON とフラグから更新の本文を組み立てます（company_id は含めません）。
func (a *App) buildWorkRecordUpdateBody(cmd *cobra.Command, date string, f *workRecordUpdateFlags) (map[string]any, error) {
	if f.noBreak && len(f.breaks) > 0 {
		return nil, usageErrorf("--break と --no-break は同時に指定できません")
	}
	if (f.clockIn == "") != (f.clockOut == "") {
		return nil, usageErrorf("--clock-in と --clock-out は両方を指定してください")
	}

	body := map[string]any{}
	if f.file != "" {
		var err error
		if body, err = readJSONObject(a.Stdin, f.file); err != nil {
			return nil, err
		}
	}

	if f.clockIn != "" {
		in, err := parseClockTime("--clock-in", date, f.clockIn)
		if err != nil {
			return nil, err
		}
		out, err := parseClockTime("--clock-out", date, f.clockOut)
		if err != nil {
			return nil, err
		}
		if !out.After(in) {
			return nil, usageErrorf("--clock-out は --clock-in より後にしてください。日をまたぐ退勤は \"YYYY-MM-DD HH:MM\" の形で日付も指定してください")
		}
		body["work_record_segments"] = []timeRange{{ClockInAt: in.Format(apiDateTimeLayout), ClockOutAt: out.Format(apiDateTimeLayout)}}
		_, fileHasBreaks := body["break_records"]
		if len(f.breaks) == 0 && !f.noBreak && !fileHasBreaks {
			return nil, usageErrorf("出退勤を指定するときは、休憩を --break HH:MM-HH:MM か --no-break で指定してください（省略すると既存の休憩がどうなるかが API で定められていないため）")
		}
	}

	if len(f.breaks) > 0 {
		breaks := make([]timeRange, 0, len(f.breaks))
		for _, b := range f.breaks {
			r, err := parseBreak(date, b)
			if err != nil {
				return nil, err
			}
			breaks = append(breaks, r)
		}
		body["break_records"] = breaks
	}
	if f.noBreak {
		body["break_records"] = []timeRange{}
	}
	if cmd.Flags().Changed("note") {
		body["note"] = f.note
	}

	if len(body) == 0 {
		return nil, usageErrorf("更新する内容を、--clock-in/--clock-out・--break・--no-break・--note・--file のいずれかで指定してください")
	}
	return body, nil
}

// timeRange は出退勤・休憩の1区間です（API の work_record_segments・break_records の要素）。
type timeRange struct {
	ClockInAt  string `json:"clock_in_at"`
	ClockOutAt string `json:"clock_out_at"`
}

// parseClockTime は HH:MM（date の日）または YYYY-MM-DD HH:MM の時刻を解釈します。
func parseClockTime(name, date, value string) (time.Time, error) {
	if t, err := time.Parse(clockDateTimeLayout, value); err == nil {
		return t, nil
	}
	if _, err := time.Parse(clockTimeLayout, value); err == nil {
		if t, err := time.Parse(clockDateTimeLayout, date+" "+value); err == nil {
			return t, nil
		}
	}
	return time.Time{}, usageErrorf("%s は HH:MM か YYYY-MM-DD HH:MM の形式で指定してください: %q", name, value)
}

// parseBreak は HH:MM-HH:MM の休憩を、date の日の区間として解釈します。
func parseBreak(date, value string) (timeRange, error) {
	parts := strings.Split(value, "-")
	if len(parts) != 2 {
		return timeRange{}, usageErrorf("--break は HH:MM-HH:MM の形式で指定してください（日をまたぐ休憩は --file で指定してください）: %q", value)
	}
	start, err := parseBreakTime(date, parts[0], value)
	if err != nil {
		return timeRange{}, err
	}
	end, err := parseBreakTime(date, parts[1], value)
	if err != nil {
		return timeRange{}, err
	}
	if !end.After(start) {
		return timeRange{}, usageErrorf("--break の終わりは始まりより後にしてください: %q", value)
	}
	return timeRange{ClockInAt: start.Format(apiDateTimeLayout), ClockOutAt: end.Format(apiDateTimeLayout)}, nil
}

func parseBreakTime(date, hhmm, original string) (time.Time, error) {
	if _, err := time.Parse(clockTimeLayout, hhmm); err != nil {
		return time.Time{}, usageErrorf("--break は HH:MM-HH:MM の形式で指定してください: %q", original)
	}
	t, err := time.Parse(clockDateTimeLayout, date+" "+hhmm)
	if err != nil {
		return time.Time{}, usageErrorf("--break は HH:MM-HH:MM の形式で指定してください: %q", original)
	}
	return t, nil
}

// readJSONObject は path（"-" の場合は標準入力）から JSON オブジェクトを読み込みます。
// 数値は桁を失わないよう json.Number のまま保持します。読めない・JSON オブジェクトでない場合は使い方の誤りにします。
func readJSONObject(stdin io.Reader, path string) (map[string]any, error) {
	var data []byte
	var err error
	if path == "-" {
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, usageErrorf("--file を読み込めませんでした: %v", err)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil || obj == nil {
		return nil, usageErrorf("--file は JSON オブジェクトにしてください（%s）", describeJSONError(err))
	}
	return obj, nil
}

func describeJSONError(err error) string {
	if err == nil {
		return "null は指定できません"
	}
	return err.Error()
}

// setCompanyID は本文の company_id をプロファイルの事業所にします。
// ファイルに別の事業所の company_id が書かれている場合は、どちらに送るべきか曖昧なため使い方の誤りにします。
func setCompanyID(body map[string]any, companyID int) error {
	if v, ok := body["company_id"]; ok {
		if fmt.Sprint(v) != strconv.Itoa(companyID) {
			return usageErrorf("--file の company_id（%v）が、プロファイルの事業所（%d）と違います", v, companyID)
		}
	}
	body["company_id"] = companyID
	return nil
}
