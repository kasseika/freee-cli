// workrecord_update_test.go は `freee-hr work-record update`（日ごとの勤怠記録の登録・更新）を検証します。
//
// 勤怠の更新は freee の会社データに書き込むため、打刻と同じ安全策（--dry-run・確認・読み取り専用）を通ります。
// また、休憩を省いた更新で既存の休憩が消えるかどうかが API のドキュメントに無いため、
// 出退勤を指定するときは --break か --no-break のどちらかを必ず指定させます。
package cli

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// segment は出退勤・休憩の1区間を、比較しやすい形で表します。
type segment struct{ in, out string }

// segmentsOf は本文の出退勤・休憩のリスト（[{clock_in_at, clock_out_at}]）を segment の配列に変換します。
func segmentsOf(t *testing.T, v any) []segment {
	t.Helper()
	list, ok := v.([]any)
	if !ok {
		t.Fatalf("リストではありません: %#v", v)
	}
	out := []segment{}
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("区間がオブジェクトではありません: %#v", item)
		}
		in, _ := m["clock_in_at"].(string)
		outAt, _ := m["clock_out_at"].(string)
		out = append(out, segment{in: in, out: outAt})
	}
	return out
}

// TestWorkRecordUpdate_Flags は、フラグで指定した出退勤・休憩・メモを freee の形式に変換して PUT することを検証します。
func TestWorkRecordUpdate_Flags(t *testing.T) {
	var requests []recordedRequest
	env := newTestEnv(t, recordingAPI(t, http.StatusOK, `{"date":"2026-11-20T00:00:00.000+09:00","clock_in_at":"2026-11-20T09:00:00.000+09:00"}`, &requests))
	env.saveLoggedIn(t, "default", false)

	code := env.app.Execute([]string{"work-record", "update", "2026-11-20",
		"--clock-in", "09:00", "--clock-out", "18:30", "--break", "12:00-13:00", "--break", "15:00-15:15",
		"--note", "打刻を忘れたため修正", "--yes"})
	if code != 0 {
		t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
	}
	if len(requests) != 1 {
		t.Fatalf("API の呼び出し回数 = %d, 期待値 = 1", len(requests))
	}
	req := requests[0]
	if req.method != http.MethodPut || req.path != "/api/v1/employees/456/work_records/2026-11-20" {
		t.Errorf("リクエスト = %s %s", req.method, req.path)
	}
	if req.body["company_id"] != float64(123) || req.body["note"] != "打刻を忘れたため修正" {
		t.Errorf("本文 = %v", req.body)
	}
	wantWork := []segment{{"2026-11-20 09:00:00", "2026-11-20 18:30:00"}}
	if got := segmentsOf(t, req.body["work_record_segments"]); !reflect.DeepEqual(got, wantWork) {
		t.Errorf("出退勤 = %v, 期待値 = %v", got, wantWork)
	}
	wantBreaks := []segment{{"2026-11-20 12:00:00", "2026-11-20 13:00:00"}, {"2026-11-20 15:00:00", "2026-11-20 15:15:00"}}
	if got := segmentsOf(t, req.body["break_records"]); !reflect.DeepEqual(got, wantBreaks) {
		t.Errorf("休憩 = %v, 期待値 = %v", got, wantBreaks)
	}
	if !strings.Contains(env.stdout.String(), `"clock_in_at"`) {
		t.Errorf("出力に API のレスポンスが含まれていません: %s", env.stdout.String())
	}
}

// TestWorkRecordUpdate_OvernightClockOut は、日をまたぐ退勤を日付つき（YYYY-MM-DD HH:MM）で指定できることを検証します。
func TestWorkRecordUpdate_OvernightClockOut(t *testing.T) {
	env := newTestEnv(t, nil)
	env.saveLoggedIn(t, "default", false)

	code := env.app.Execute([]string{"work-record", "update", "2026-11-20",
		"--clock-in", "18:00", "--clock-out", "2026-11-21 02:00", "--no-break", "--dry-run"})
	if code != 0 {
		t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
	}
	body := dryRunBody(t, env.stdout.Bytes())
	want := []segment{{"2026-11-20 18:00:00", "2026-11-21 02:00:00"}}
	if got := segmentsOf(t, body["work_record_segments"]); !reflect.DeepEqual(got, want) {
		t.Errorf("出退勤 = %v, 期待値 = %v", got, want)
	}
	// 検証: --no-break は「休憩なし」を明示的に送る
	if got := segmentsOf(t, body["break_records"]); len(got) != 0 {
		t.Errorf("--no-break の休憩 = %v, 期待値 = 空のリスト", got)
	}
}

// dryRunBody は --dry-run の出力から本文を取り出し、API を呼ばない出力であることを確かめます。
func dryRunBody(t *testing.T, stdout []byte) map[string]any {
	t.Helper()
	var out struct {
		DryRun bool           `json:"dry_run"`
		Method string         `json:"method"`
		Body   map[string]any `json:"body"`
	}
	if err := json.Unmarshal(stdout, &out); err != nil {
		t.Fatalf("出力が JSON ではありません: %v\n%s", err, stdout)
	}
	if !out.DryRun || out.Method != http.MethodPut {
		t.Fatalf("dry-run の出力ではありません: %s", stdout)
	}
	return out.Body
}

// TestWorkRecordUpdate_File は、--file の JSON（休暇など、フラグに無い項目）をそのまま送り、
// フラグで指定した項目はファイルの値より優先することを検証します。
func TestWorkRecordUpdate_File(t *testing.T) {
	env := newTestEnv(t, nil)
	env.saveLoggedIn(t, "default", false)
	path := filepath.Join(t.TempDir(), "勤怠.json")
	content := `{
	  "break_records": [{"clock_in_at": "2026-11-20 12:00:00", "clock_out_at": "2026-11-20 13:00:00"}],
	  "work_record_segments": [{"clock_in_at": "2026-11-20 14:00:00", "clock_out_at": "2026-11-20 18:00:00"}],
	  "holidays": [{"type": "paid_holiday", "holiday_type": "morning_off"}],
	  "note": "ファイルのメモ"
	}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	code := env.app.Execute([]string{"work-record", "update", "2026-11-20", "--file", path, "--note", "フラグのメモ", "--dry-run"})
	if code != 0 {
		t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
	}
	body := dryRunBody(t, env.stdout.Bytes())
	if body["company_id"] != float64(123) {
		t.Errorf("company_id はプロファイルの事業所にする必要があります: %v", body["company_id"])
	}
	if body["note"] != "フラグのメモ" {
		t.Errorf("フラグの --note がファイルより優先されていません: %v", body["note"])
	}
	if _, ok := body["holidays"]; !ok {
		t.Errorf("ファイルの holidays がそのまま送られていません: %v", body)
	}
	if got := segmentsOf(t, body["work_record_segments"]); !reflect.DeepEqual(got, []segment{{"2026-11-20 14:00:00", "2026-11-20 18:00:00"}}) {
		t.Errorf("ファイルの出退勤がそのまま送られていません: %v", got)
	}
}

// TestWorkRecordUpdate_FileFromStdin は、--file - で標準入力から本文を読み込めることを検証します。
func TestWorkRecordUpdate_FileFromStdin(t *testing.T) {
	env := newTestEnv(t, nil)
	env.saveLoggedIn(t, "default", false)
	env.app.Stdin = strings.NewReader(`{"note": "標準入力のメモ"}`)

	code := env.app.Execute([]string{"work-record", "update", "2026-11-20", "--file", "-", "--dry-run"})
	if code != 0 {
		t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
	}
	if body := dryRunBody(t, env.stdout.Bytes()); body["note"] != "標準入力のメモ" {
		t.Errorf("本文 = %v", body)
	}
}

// TestWorkRecordUpdate_FlagsOverrideFileSegments は、--clock-in/--clock-out と --break が
// ファイルの出退勤・休憩を置き換えることを検証します。
func TestWorkRecordUpdate_FlagsOverrideFileSegments(t *testing.T) {
	env := newTestEnv(t, nil)
	env.saveLoggedIn(t, "default", false)
	env.app.Stdin = strings.NewReader(`{
	  "break_records": [{"clock_in_at": "2026-11-20 12:00:00", "clock_out_at": "2026-11-20 13:00:00"}],
	  "work_record_segments": [{"clock_in_at": "2026-11-20 14:00:00", "clock_out_at": "2026-11-20 18:00:00"}]
	}`)

	code := env.app.Execute([]string{"work-record", "update", "2026-11-20", "--file", "-",
		"--clock-in", "10:00", "--clock-out", "19:00", "--break", "13:00-14:00", "--dry-run"})
	if code != 0 {
		t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
	}
	body := dryRunBody(t, env.stdout.Bytes())
	if got := segmentsOf(t, body["work_record_segments"]); !reflect.DeepEqual(got, []segment{{"2026-11-20 10:00:00", "2026-11-20 19:00:00"}}) {
		t.Errorf("出退勤 = %v", got)
	}
	if got := segmentsOf(t, body["break_records"]); !reflect.DeepEqual(got, []segment{{"2026-11-20 13:00:00", "2026-11-20 14:00:00"}}) {
		t.Errorf("休憩 = %v", got)
	}
}

// TestWorkRecordUpdate_FileWithBreaksNeedsNoBreakFlag は、ファイルに休憩がある場合は、
// --clock-in/--clock-out だけを指定しても休憩の指定漏れにならないことを検証します。
func TestWorkRecordUpdate_FileWithBreaksNeedsNoBreakFlag(t *testing.T) {
	env := newTestEnv(t, nil)
	env.saveLoggedIn(t, "default", false)
	env.app.Stdin = strings.NewReader(`{"break_records": [{"clock_in_at": "2026-11-20 12:00:00", "clock_out_at": "2026-11-20 13:00:00"}]}`)

	code := env.app.Execute([]string{"work-record", "update", "2026-11-20", "--file", "-", "--clock-in", "09:00", "--clock-out", "18:00", "--dry-run"})
	if code != 0 {
		t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
	}
	if got := segmentsOf(t, dryRunBody(t, env.stdout.Bytes())["break_records"]); len(got) != 1 {
		t.Errorf("ファイルの休憩が残っていません: %v", got)
	}
}

// TestWorkRecordUpdate_FileSegmentsWithNoBreakFlag は、ファイルの出退勤に --no-break を組み合わせれば送れることを検証します。
func TestWorkRecordUpdate_FileSegmentsWithNoBreakFlag(t *testing.T) {
	env := newTestEnv(t, nil)
	env.saveLoggedIn(t, "default", false)
	env.app.Stdin = strings.NewReader(`{"work_record_segments": [{"clock_in_at": "2026-11-20 09:00:00", "clock_out_at": "2026-11-20 18:00:00"}]}` + "\n\n")

	code := env.app.Execute([]string{"work-record", "update", "2026-11-20", "--file", "-", "--no-break", "--dry-run"})
	if code != 0 {
		t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
	}
	if got := segmentsOf(t, dryRunBody(t, env.stdout.Bytes())["break_records"]); len(got) != 0 {
		t.Errorf("休憩 = %v, 期待値 = 空のリスト", got)
	}
}

// TestWorkRecordUpdate_SafetyGuards は、打刻と同じ安全策（確認・読み取り専用）を通ることを検証します。
func TestWorkRecordUpdate_SafetyGuards(t *testing.T) {
	args := []string{"work-record", "update", "2026-11-20", "--clock-in", "09:00", "--clock-out", "18:00", "--no-break"}

	t.Run("標準入力が無く --yes も無い場合は送信しない", func(t *testing.T) {
		env := newTestEnv(t, nil)
		env.saveLoggedIn(t, "default", false)
		if code := env.app.Execute(args); code != 2 {
			t.Errorf("終了コード = %d, 期待値 = 2", code)
		}
		if !strings.Contains(env.stderr.String(), "/api/v1/employees/456/work_records/2026-11-20") {
			t.Errorf("送る予定の内容が表示されていません: %s", env.stderr.String())
		}
	})
	t.Run("確認で y と答えると送信する", func(t *testing.T) {
		var requests []recordedRequest
		env := newTestEnv(t, recordingAPI(t, http.StatusOK, `{}`, &requests))
		env.saveLoggedIn(t, "default", false)
		env.app.Stdin = strings.NewReader("y\n")
		if code := env.app.Execute(args); code != 0 || len(requests) != 1 {
			t.Errorf("終了コード = %d, 呼び出し回数 = %d, stderr=%s", code, len(requests), env.stderr.String())
		}
	})
	t.Run("読み取り専用のプロファイルでは送信しない", func(t *testing.T) {
		env := newTestEnv(t, nil)
		env.saveLoggedIn(t, "default", true)
		if code := env.app.Execute(append(args, "--yes")); code != 2 {
			t.Errorf("終了コード = %d, 期待値 = 2", code)
		}
	})
}

// TestWorkRecordUpdate_UsageErrors は、指定の誤りで API を呼ばずに終了コード2になることを検証します。
func TestWorkRecordUpdate_UsageErrors(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	otherCompany := write("別の事業所.json", `{"company_id": 999, "note": "別の事業所のメモ"}`)
	notJSON := write("壊れた.json", `{"note": `)
	notObject := write("配列.json", `[{"note": "配列"}]`)
	twoObjects := write("2つのオブジェクト.json", `{"note": "1つ目"} {"note": "2つ目"}`)
	segmentsWithoutBreaks := write("休憩なしの出退勤.json", `{"work_record_segments": [{"clock_in_at": "2026-11-20 09:00:00", "clock_out_at": "2026-11-20 18:00:00"}]}`)
	legacyTimesWithoutBreaks := write("旧形式の出退勤.json", `{"clock_in_at": "2026-11-20 09:00:00", "clock_out_at": "2026-11-20 18:00:00"}`)

	cases := []struct {
		name string
		args []string
	}{
		{"何も指定しない", []string{}},
		{"出勤だけを指定", []string{"--clock-in", "09:00", "--no-break"}},
		{"退勤だけを指定", []string{"--clock-out", "18:00", "--no-break"}},
		{"休憩の指定が無い", []string{"--clock-in", "09:00", "--clock-out", "18:00"}},
		{"--break と --no-break の同時指定", []string{"--break", "12:00-13:00", "--no-break"}},
		{"退勤が出勤より前（日をまたぐなら日付が必要）", []string{"--clock-in", "18:00", "--clock-out", "02:00", "--no-break"}},
		{"時刻の形式が不正", []string{"--clock-in", "9時", "--clock-out", "18:00", "--no-break"}},
		{"存在しない時刻", []string{"--clock-in", "25:00", "--clock-out", "26:00", "--no-break"}},
		{"休憩の形式が不正", []string{"--break", "12:00"}},
		{"休憩の終わりが始まりより前", []string{"--break", "13:00-12:00"}},
		{"ファイルの事業所がプロファイルと違う", []string{"--file", otherCompany}},
		{"ファイルが JSON ではない", []string{"--file", notJSON}},
		{"ファイルがオブジェクトではない", []string{"--file", notObject}},
		{"ファイルが存在しない", []string{"--file", filepath.Join(dir, "存在しない.json")}},
		{"ファイルの後ろに別の JSON が続く", []string{"--file", twoObjects}},
		{"ファイルの出退勤に休憩の指定が無い", []string{"--file", segmentsWithoutBreaks}},
		{"ファイルの旧形式の出退勤に休憩の指定が無い", []string{"--file", legacyTimesWithoutBreaks}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := newTestEnv(t, nil)
			env.saveLoggedIn(t, "default", false)
			args := append([]string{"work-record", "update", "2026-11-20"}, c.args...)
			if code := env.app.Execute(append(args, "--yes")); code != 2 {
				t.Errorf("終了コード = %d, 期待値 = 2, stderr=%s", code, env.stderr.String())
			}
		})
	}
}
