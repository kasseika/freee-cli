// approval_request_test.go は `freee-hr approval <種類> list / get / create` と
// `freee-hr approval overtime-work setting`（申請の一覧・詳細・作成と残業申請の設定）を検証します。
//
// 申請の種類は、勤務時間修正（work-time）・有給休暇（paid-holiday）・特別休暇（special-holiday）・
// 残業（overtime-work）・月次締め（monthly-attendance）の5つです。
package cli

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testRouteID は、テストで使う申請経路ID（申請時に承認者を選ぶ型の経路）です。
const testRouteID = "1582563"

// approvalDryRunBody は、申請の作成の --dry-run の出力から、送る予定の本文を取り出します。
// 出力が POST の dry-run でない場合はテストを失敗させます。path も確認できるよう一緒に返します。
func approvalDryRunBody(t *testing.T, stdout []byte) (path string, body map[string]any) {
	t.Helper()
	var out struct {
		DryRun bool           `json:"dry_run"`
		Method string         `json:"method"`
		Path   string         `json:"path"`
		Body   map[string]any `json:"body"`
	}
	if err := json.Unmarshal(stdout, &out); err != nil {
		t.Fatalf("出力が JSON ではありません: %v\n%s", err, stdout)
	}
	if !out.DryRun || out.Method != http.MethodPost {
		t.Fatalf("POST の dry-run の出力ではありません: %s", stdout)
	}
	return out.Path, out.Body
}

// assertNumber は、JSON の本文の数値の項目が want であることを確かめます。
func assertNumber(t *testing.T, body map[string]any, key string, want float64) {
	t.Helper()
	if got, ok := body[key].(float64); !ok || got != want {
		t.Errorf("%s = %#v, 期待値 = %v（本文: %v）", key, body[key], want, body)
	}
}

// TestApprovalRequestList は、申請の種類ごとに正しい一覧の API を呼び、絞り込みの条件を渡すことを検証します。
func TestApprovalRequestList(t *testing.T) {
	cases := []struct {
		command string
		path    string
	}{
		{"work-time", "/api/v1/approval_requests/work_times"},
		{"paid-holiday", "/api/v1/approval_requests/paid_holidays"},
		{"special-holiday", "/api/v1/approval_requests/special_holidays"},
		{"overtime-work", "/api/v1/approval_requests/overtime_works"},
		{"monthly-attendance", "/api/v1/approval_requests/monthly_attendances"},
	}
	for _, c := range cases {
		t.Run(c.command, func(t *testing.T) {
			var requests []recordedRequest
			env := newTestEnv(t, recordingAPI(t, http.StatusOK, `{"total_count":0}`, &requests))
			env.saveLoggedIn(t, "default", false)

			code := env.app.Execute([]string{"approval", c.command, "list",
				"--status", "in_progress", "--from", "2026-11-01", "--to", "2026-11-30",
				"--applicant-id", "321", "--limit", "10", "--offset", "20"})
			if code != 0 {
				t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
			}
			req := requests[0]
			if req.method != http.MethodGet || req.path != c.path {
				t.Errorf("リクエスト = %s %s, 期待値 = GET %s", req.method, req.path, c.path)
			}
			want := map[string]string{
				"company_id": "123", "status": "in_progress",
				"start_target_date": "2026-11-01", "end_target_date": "2026-11-30",
				"applicant_id": "321", "limit": "10", "offset": "20",
			}
			for k, v := range want {
				if req.query[k] != v {
					t.Errorf("クエリ %s = %q, 期待値 = %q（クエリ全体: %v）", k, req.query[k], v, req.query)
				}
			}
		})
	}
}

// TestApprovalRequestList_NoFilters は、絞り込みを省略した場合は company_id だけを送ることを検証します。
func TestApprovalRequestList_NoFilters(t *testing.T) {
	var requests []recordedRequest
	env := newTestEnv(t, recordingAPI(t, http.StatusOK, `{"paid_holidays":[],"total_count":0}`, &requests))
	env.saveLoggedIn(t, "default", false)

	if code := env.app.Execute([]string{"approval", "paid-holiday", "list"}); code != 0 {
		t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
	}
	if len(requests[0].query) != 1 || requests[0].query["company_id"] != "123" {
		t.Errorf("クエリ = %v, 期待値 = company_id だけ", requests[0].query)
	}
}

// TestApprovalRequestGet は、申請IDを指定して1件の申請を取得することを検証します。
func TestApprovalRequestGet(t *testing.T) {
	var requests []recordedRequest
	env := newTestEnv(t, recordingAPI(t, http.StatusOK, `{"paid_holiday":{"id":77,"status":"approved"}}`, &requests))
	env.saveLoggedIn(t, "default", false)

	code := env.app.Execute([]string{"approval", "paid-holiday", "get", "77"})
	if code != 0 {
		t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
	}
	req := requests[0]
	if req.method != http.MethodGet || req.path != "/api/v1/approval_requests/paid_holidays/77" || req.query["company_id"] != "123" {
		t.Errorf("リクエスト = %+v", req)
	}
	if !strings.Contains(env.stdout.String(), `"approved"`) {
		t.Errorf("出力に API のレスポンスが含まれていません: %s", env.stdout.String())
	}
}

// TestApprovalWorkTimeCreate_DryRun は、勤務時間修正申請の本文を、フラグから API の形に組み立てることを検証します。
func TestApprovalWorkTimeCreate_DryRun(t *testing.T) {
	env := newTestEnv(t, nil)
	env.saveLoggedIn(t, "default", false)

	code := env.app.Execute([]string{"approval", "work-time", "create", "2026-11-19",
		"--clock-in", "09:00", "--clock-out", "18:00", "--break", "12:00-13:00",
		"--route-id", testRouteID, "--approver-id", "999", "--comment", "打刻を忘れたため", "--dry-run"})
	if code != 0 {
		t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
	}
	path, body := approvalDryRunBody(t, env.stdout.Bytes())
	if path != "/api/v1/approval_requests/work_times" {
		t.Errorf("path = %s", path)
	}
	assertNumber(t, body, "company_id", 123)
	assertNumber(t, body, "approval_flow_route_id", 1582563)
	assertNumber(t, body, "approver_id", 999)
	if body["target_date"] != "2026-11-19" || body["comment"] != "打刻を忘れたため" {
		t.Errorf("本文 = %v", body)
	}
	if got := segmentsOf(t, body["work_records"]); len(got) != 1 || got[0] != (segment{in: "09:00", out: "18:00"}) {
		t.Errorf("work_records = %v", got)
	}
	if got := segmentsOf(t, body["break_records"]); len(got) != 1 || got[0] != (segment{in: "12:00", out: "13:00"}) {
		t.Errorf("break_records = %v", got)
	}
}

// TestApprovalWorkTimeCreate_Clear は、--clear で勤務時間を削除する申請になることを検証します。
func TestApprovalWorkTimeCreate_Clear(t *testing.T) {
	env := newTestEnv(t, nil)
	env.saveLoggedIn(t, "default", false)

	code := env.app.Execute([]string{"approval", "work-time", "create", "2026-11-19", "--clear", "--route-id", testRouteID, "--dry-run"})
	if code != 0 {
		t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
	}
	_, body := approvalDryRunBody(t, env.stdout.Bytes())
	if body["clear_work_time"] != true {
		t.Errorf("clear_work_time = %#v", body["clear_work_time"])
	}
	if _, ok := body["work_records"]; ok {
		t.Errorf("--clear では work_records を送らない必要があります: %v", body)
	}
	if _, ok := body["approver_id"]; ok {
		t.Errorf("--approver-id を省略した場合は approver_id を送らない必要があります: %v", body)
	}
}

// TestApprovalPaidHolidayCreate_DryRun は、有給休暇申請の取得単位と時間を values の形で送ることを検証します。
func TestApprovalPaidHolidayCreate_DryRun(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		value map[string]any
	}{
		{"全休", []string{"--type", "full"}, map[string]any{"type": "full"}},
		{"時間休", []string{"--type", "hourly", "--start", "10:00", "--end", "12:00"}, map[string]any{"type": "hourly", "start_at": "10:00", "end_at": "12:00"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := newTestEnv(t, nil)
			env.saveLoggedIn(t, "default", false)

			args := append([]string{"approval", "paid-holiday", "create", "2026-12-01", "--route-id", testRouteID, "--dry-run"}, c.args...)
			if code := env.app.Execute(args); code != 0 {
				t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
			}
			path, body := approvalDryRunBody(t, env.stdout.Bytes())
			if path != "/api/v1/approval_requests/paid_holidays" || body["target_date"] != "2026-12-01" {
				t.Errorf("path = %s, 本文 = %v", path, body)
			}
			values, ok := body["values"].([]any)
			if !ok || len(values) != 1 {
				t.Fatalf("values = %#v", body["values"])
			}
			got, _ := values[0].(map[string]any)
			if len(got) != len(c.value) {
				t.Errorf("values[0] = %v, 期待値 = %v", got, c.value)
			}
			for k, v := range c.value {
				if got[k] != v {
					t.Errorf("values[0].%s = %#v, 期待値 = %#v", k, got[k], v)
				}
			}
		})
	}
}

// TestApprovalSpecialHolidayCreate_DryRun は、特別休暇申請の設定ID・取得単位・時間を送ることを検証します。
func TestApprovalSpecialHolidayCreate_DryRun(t *testing.T) {
	env := newTestEnv(t, nil)
	env.saveLoggedIn(t, "default", false)

	code := env.app.Execute([]string{"approval", "special-holiday", "create", "2026-12-02",
		"--setting-id", "55", "--type", "half", "--start", "13:00", "--end", "18:00", "--route-id", testRouteID, "--dry-run"})
	if code != 0 {
		t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
	}
	path, body := approvalDryRunBody(t, env.stdout.Bytes())
	if path != "/api/v1/approval_requests/special_holidays" {
		t.Errorf("path = %s", path)
	}
	assertNumber(t, body, "special_holiday_setting_id", 55)
	if body["holiday_type"] != "half" || body["start_at"] != "13:00" || body["end_at"] != "18:00" || body["target_date"] != "2026-12-02" {
		t.Errorf("本文 = %v", body)
	}
}

// TestApprovalOvertimeWorkCreate_DryRun は、残業申請の時刻（反映あり・反映なしの両方の形）を送ることを検証します。
func TestApprovalOvertimeWorkCreate_DryRun(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want map[string]any
	}{
		{"勤怠に反映する残業", []string{"--overtime-start", "18:00", "--overtime-end", "20:00"},
			map[string]any{"overtime_work_start_at": "18:00", "overtime_work_end_at": "20:00"}},
		{"勤怠に反映する早出", []string{"--early-start", "07:00", "--early-end", "09:00"},
			map[string]any{"early_work_start_at": "07:00", "early_work_end_at": "09:00"}},
		{"勤怠に反映しない残業", []string{"--start", "18:00", "--end", "21:00"},
			map[string]any{"start_at": "18:00", "end_at": "21:00"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := newTestEnv(t, nil)
			env.saveLoggedIn(t, "default", false)

			args := append([]string{"approval", "overtime-work", "create", "2026-11-20", "--route-id", testRouteID, "--dry-run"}, c.args...)
			if code := env.app.Execute(args); code != 0 {
				t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
			}
			path, body := approvalDryRunBody(t, env.stdout.Bytes())
			if path != "/api/v1/approval_requests/overtime_works" {
				t.Errorf("path = %s", path)
			}
			for k, v := range c.want {
				if body[k] != v {
					t.Errorf("%s = %#v, 期待値 = %#v（本文: %v）", k, body[k], v, body)
				}
			}
		})
	}
}

// TestApprovalMonthlyAttendanceCreate_DryRun は、月次締め申請で対象の年と月を数値で送ることを検証します。
func TestApprovalMonthlyAttendanceCreate_DryRun(t *testing.T) {
	env := newTestEnv(t, nil)
	env.saveLoggedIn(t, "default", false)

	code := env.app.Execute([]string{"approval", "monthly-attendance", "create", "2026-11", "--route-id", testRouteID, "--dry-run"})
	if code != 0 {
		t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
	}
	path, body := approvalDryRunBody(t, env.stdout.Bytes())
	if path != "/api/v1/approval_requests/monthly_attendances" {
		t.Errorf("path = %s", path)
	}
	assertNumber(t, body, "target_year", 2026)
	assertNumber(t, body, "target_month", 11)
	if _, ok := body["target_date"]; ok {
		t.Errorf("月次締め申請では target_date を送らない必要があります: %v", body)
	}
}

// TestApprovalCreate_Yes は、--yes で確認なしに POST を送り、API のレスポンスを出力することを検証します。
func TestApprovalCreate_Yes(t *testing.T) {
	var requests []recordedRequest
	env := newTestEnv(t, recordingAPI(t, http.StatusCreated, `{"monthly_attendance":{"id":501,"status":"in_progress"}}`, &requests))
	env.saveLoggedIn(t, "default", false)

	code := env.app.Execute([]string{"approval", "monthly-attendance", "create", "2026-11", "--route-id", testRouteID, "--yes"})
	if code != 0 {
		t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
	}
	if len(requests) != 1 || requests[0].method != http.MethodPost || requests[0].path != "/api/v1/approval_requests/monthly_attendances" {
		t.Fatalf("リクエスト = %+v", requests)
	}
	if !strings.Contains(env.stdout.String(), `"id": 501`) {
		t.Errorf("出力に API のレスポンスが含まれていません: %s", env.stdout.String())
	}
}

// TestApprovalCreate_NoStdinAborts は、--yes が無く確認に答えられない場合（標準入力が無い）は送信しないことを検証します。
func TestApprovalCreate_NoStdinAborts(t *testing.T) {
	var requests []recordedRequest
	env := newTestEnv(t, recordingAPI(t, http.StatusCreated, `{}`, &requests))
	env.saveLoggedIn(t, "default", false)

	code := env.app.Execute([]string{"approval", "monthly-attendance", "create", "2026-11", "--route-id", testRouteID})
	if code != 2 {
		t.Fatalf("終了コード = %d, 期待値 = 2", code)
	}
	if len(requests) != 0 {
		t.Errorf("確認されていないのに API を呼びました: %+v", requests)
	}
}

// TestApprovalCreate_ReadOnly は、読み取り専用のプロファイルでは申請を送信しないことを検証します。
func TestApprovalCreate_ReadOnly(t *testing.T) {
	env := newTestEnv(t, nil)
	env.saveLoggedIn(t, "default", true)

	code := env.app.Execute([]string{"approval", "monthly-attendance", "create", "2026-11", "--route-id", testRouteID, "--yes"})
	if code != 2 {
		t.Fatalf("終了コード = %d, 期待値 = 2", code)
	}
}

// TestApprovalCreate_File は、--file の JSON にフラグの値を上書きし、申請経路IDをファイルから受け取れることを検証します。
func TestApprovalCreate_File(t *testing.T) {
	env := newTestEnv(t, nil)
	env.saveLoggedIn(t, "default", false)
	path := filepath.Join(t.TempDir(), "request.json")
	content := `{"approval_flow_route_id": 1582563, "comment": "ファイルの申請理由", "lateness_mins": 15}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	code := env.app.Execute([]string{"approval", "work-time", "create", "2026-11-19", "--file", path, "--comment", "フラグの申請理由", "--dry-run"})
	if code != 0 {
		t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
	}
	_, body := approvalDryRunBody(t, env.stdout.Bytes())
	assertNumber(t, body, "approval_flow_route_id", 1582563)
	assertNumber(t, body, "lateness_mins", 15)
	assertNumber(t, body, "company_id", 123)
	if body["comment"] != "フラグの申請理由" || body["target_date"] != "2026-11-19" {
		t.Errorf("本文 = %v", body)
	}
}

// TestOvertimeWorkSetting は、指定日の残業申請の設定（勤怠に反映するか・所定の出退勤時刻）を取得することを検証します。
func TestOvertimeWorkSetting(t *testing.T) {
	var requests []recordedRequest
	env := newTestEnv(t, recordingAPI(t, http.StatusOK, `{"should_reflect_in_work_record":true}`, &requests))
	env.saveLoggedIn(t, "default", false)

	code := env.app.Execute([]string{"approval", "overtime-work", "setting", "--date", "2026-11-20"})
	if code != 0 {
		t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
	}
	req := requests[0]
	if req.method != http.MethodGet || req.path != "/api/v1/approval_requests/overtime_works/setting" ||
		req.query["company_id"] != "123" || req.query["date"] != "2026-11-20" {
		t.Errorf("リクエスト = %+v", req)
	}
}

// TestApprovalUsageErrors は、申請のコマンドの使い方の誤りで、API を呼ばずに終了コード2になることを検証します。
func TestApprovalUsageErrors(t *testing.T) {
	route := []string{"--route-id", testRouteID, "--dry-run"}
	cases := []struct {
		name string
		args []string
	}{
		{"申請経路IDが無い", []string{"approval", "monthly-attendance", "create", "2026-11", "--dry-run"}},
		{"申請経路IDが0", []string{"approval", "monthly-attendance", "create", "2026-11", "--route-id", "0", "--dry-run"}},
		{"承認者IDが0", append([]string{"approval", "monthly-attendance", "create", "2026-11", "--approver-id", "0"}, route...)},
		{"年月の形式が不正", append([]string{"approval", "monthly-attendance", "create", "2026-11-01"}, route...)},
		{"対象日が無い", append([]string{"approval", "work-time", "create"}, route...)},
		{"対象日の形式が不正", append([]string{"approval", "work-time", "create", "2026/11/19", "--clear"}, route...)},
		{"勤務時間修正の内容が無い", append([]string{"approval", "work-time", "create", "2026-11-19"}, route...)},
		{"出勤だけを指定", append([]string{"approval", "work-time", "create", "2026-11-19", "--clock-in", "09:00"}, route...)},
		{"時刻の形式が不正", append([]string{"approval", "work-time", "create", "2026-11-19", "--clock-in", "9時", "--clock-out", "18:00"}, route...)},
		{"休憩の形式が不正", append([]string{"approval", "work-time", "create", "2026-11-19", "--clock-in", "09:00", "--clock-out", "18:00", "--break", "12:00"}, route...)},
		{"削除と時刻を同時に指定", append([]string{"approval", "work-time", "create", "2026-11-19", "--clear", "--clock-in", "09:00", "--clock-out", "18:00"}, route...)},
		{"有給の取得単位が無い", append([]string{"approval", "paid-holiday", "create", "2026-12-01"}, route...)},
		{"有給の取得単位が不正", append([]string{"approval", "paid-holiday", "create", "2026-12-01", "--type", "hour"}, route...)},
		{"時間休なのに時間が無い", append([]string{"approval", "paid-holiday", "create", "2026-12-01", "--type", "hourly"}, route...)},
		{"特別休暇の設定IDが無い", append([]string{"approval", "special-holiday", "create", "2026-12-02", "--type", "full"}, route...)},
		{"特別休暇の半休に時間が無い", append([]string{"approval", "special-holiday", "create", "2026-12-02", "--setting-id", "55", "--type", "half"}, route...)},
		{"残業の時刻が無い", append([]string{"approval", "overtime-work", "create", "2026-11-20"}, route...)},
		{"残業の終了だけを指定", append([]string{"approval", "overtime-work", "create", "2026-11-20", "--overtime-end", "20:00"}, route...)},
		{"一覧の状態が不正", []string{"approval", "work-time", "list", "--status", "done"}},
		{"一覧の件数が範囲外", []string{"approval", "work-time", "list", "--limit", "101"}},
		{"一覧の開始日の形式が不正", []string{"approval", "work-time", "list", "--from", "2026/11/01"}},
		{"申請IDが数値でない", []string{"approval", "work-time", "get", "abc"}},
		{"残業設定の日付が無い", []string{"approval", "overtime-work", "setting"}},
		{"dry-run と yes を同時に指定", []string{"approval", "monthly-attendance", "create", "2026-11", "--route-id", testRouteID, "--dry-run", "--yes"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var requests []recordedRequest
			env := newTestEnv(t, recordingAPI(t, http.StatusOK, `{}`, &requests))
			env.saveLoggedIn(t, "default", false)
			if code := env.app.Execute(c.args); code != 2 {
				t.Errorf("終了コード = %d, 期待値 = 2, stdout=%s, stderr=%s", code, env.stdout.String(), env.stderr.String())
			}
			if len(requests) != 0 {
				t.Errorf("使い方の誤りなのに API を呼びました: %+v", requests)
			}
		})
	}
}

// TestApprovalCreate_FileCompanyMismatch は、--file に別の事業所の company_id が書かれている場合は送らないことを検証します。
func TestApprovalCreate_FileCompanyMismatch(t *testing.T) {
	env := newTestEnv(t, nil)
	env.saveLoggedIn(t, "default", false)
	path := filepath.Join(t.TempDir(), "request.json")
	if err := os.WriteFile(path, []byte(`{"company_id": 999}`), 0o600); err != nil {
		t.Fatal(err)
	}
	code := env.app.Execute([]string{"approval", "monthly-attendance", "create", "2026-11", "--route-id", testRouteID, "--file", path, "--dry-run"})
	if code != 2 {
		t.Fatalf("終了コード = %d, 期待値 = 2", code)
	}
}
