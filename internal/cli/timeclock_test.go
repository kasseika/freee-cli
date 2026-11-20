// timeclock_test.go は `freee-hr timeclock` コマンド（打刻可能種別・打刻一覧・打刻）を検証します。
//
// 打刻（punch）は freee の会社データに書き込むため、次の安全策を検証します。
//   - --dry-run では API を呼ばず、送る予定のリクエストを出力する
//   - --yes が無い場合は確認を求め、y 以外（標準入力が無い場合を含む）では送信しない
//   - 読み取り専用のプロファイルでは送信を拒否する
package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// recordedRequest はモックの API が受け取ったリクエストの内容です。
type recordedRequest struct {
	method string
	path   string
	query  map[string]string
	body   map[string]any
}

// recordingAPI は、受け取ったリクエストを記録して response を返すモックの API です。
func recordingAPI(t *testing.T, status int, response string, requests *[]recordedRequest) func(http.ResponseWriter, *http.Request) {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		rec := recordedRequest{method: r.Method, path: r.URL.Path, query: map[string]string{}}
		for k := range r.URL.Query() {
			rec.query[k] = r.URL.Query().Get(k)
		}
		if data, _ := io.ReadAll(r.Body); len(data) > 0 {
			if err := json.Unmarshal(data, &rec.body); err != nil {
				t.Errorf("リクエスト本文がJSONではありません: %s", data)
			}
		}
		*requests = append(*requests, rec)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(response))
	}
}

// TestTimeclockTypes は、ログイン時に保存した事業所・従業員について、指定日の打刻可能種別を取得することを検証します。
func TestTimeclockTypes(t *testing.T) {
	var requests []recordedRequest
	env := newTestEnv(t, recordingAPI(t, http.StatusOK, `{"available_types":["clock_in"],"base_date":"2026-11-20"}`, &requests))
	env.saveLoggedIn(t, "default", false)

	code := env.app.Execute([]string{"timeclock", "types", "--date", "2026-11-20"})
	if code != 0 {
		t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
	}
	if len(requests) != 1 {
		t.Fatalf("API の呼び出し回数 = %d, 期待値 = 1", len(requests))
	}
	req := requests[0]
	if req.method != http.MethodGet || req.path != "/api/v1/employees/456/time_clocks/available_types" {
		t.Errorf("リクエスト = %s %s", req.method, req.path)
	}
	if req.query["company_id"] != "123" || req.query["date"] != "2026-11-20" {
		t.Errorf("クエリ = %v", req.query)
	}
	if !strings.Contains(env.stdout.String(), `"clock_in"`) {
		t.Errorf("出力に API のレスポンスが含まれていません: %s", env.stdout.String())
	}
}

// TestTimeclockTypes_EmployeeIDOverride は、--employee-id で対象の従業員を変えられることを検証します（管理者の操作を想定）。
func TestTimeclockTypes_EmployeeIDOverride(t *testing.T) {
	var requests []recordedRequest
	env := newTestEnv(t, recordingAPI(t, http.StatusOK, `{"available_types":[]}`, &requests))
	env.saveLoggedIn(t, "default", false)

	code := env.app.Execute([]string{"timeclock", "types", "--employee-id", "789"})
	if code != 0 {
		t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
	}
	if len(requests) != 1 || requests[0].path != "/api/v1/employees/789/time_clocks/available_types" {
		t.Errorf("リクエスト = %+v", requests)
	}
	if _, ok := requests[0].query["date"]; ok {
		t.Errorf("--date を省略した場合は date を送らない（API の既定値の当日を使う）必要があります: %v", requests[0].query)
	}
}

// TestTimeclockTypes_NoEmployee は、従業員として登録されていないプロファイルで --employee-id も無い場合、
// API を呼ばずに終了コード2になることを検証します。
func TestTimeclockTypes_NoEmployee(t *testing.T) {
	env := newTestEnv(t, nil)
	env.saveNotEmployee(t, "default")

	code := env.app.Execute([]string{"timeclock", "types"})
	if code != 2 {
		t.Fatalf("終了コード = %d, 期待値 = 2", code)
	}
	if !strings.Contains(env.stderr.String(), "--employee-id") {
		t.Errorf("--employee-id の案内がありません: %s", env.stderr.String())
	}
}

// TestTimeclockList は、期間・件数・オフセットを指定して打刻一覧を取得することを検証します。
func TestTimeclockList(t *testing.T) {
	var requests []recordedRequest
	env := newTestEnv(t, recordingAPI(t, http.StatusOK, `[{"id":1,"date":"2026-11-20","type":"clock_in","datetime":"2026-11-20T09:00:00.000+09:00"}]`, &requests))
	env.saveLoggedIn(t, "default", false)

	code := env.app.Execute([]string{"timeclock", "list", "--from", "2026-11-01", "--to", "2026-11-20", "--limit", "100", "--offset", "50"})
	if code != 0 {
		t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
	}
	req := requests[0]
	if req.method != http.MethodGet || req.path != "/api/v1/employees/456/time_clocks" {
		t.Errorf("リクエスト = %s %s", req.method, req.path)
	}
	want := map[string]string{"company_id": "123", "from_date": "2026-11-01", "to_date": "2026-11-20", "limit": "100", "offset": "50"}
	for k, v := range want {
		if req.query[k] != v {
			t.Errorf("クエリ %s = %q, 期待値 = %q", k, req.query[k], v)
		}
	}
}

// TestTimeclockUsageErrors は、不正な日付・件数・打刻種別で API を呼ばずに終了コード2になることを検証します。
func TestTimeclockUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"日付の形式が不正", []string{"timeclock", "types", "--date", "2026/11/20"}},
		{"存在しない日付", []string{"timeclock", "list", "--from", "2026-02-30"}},
		{"件数が上限を超える", []string{"timeclock", "list", "--limit", "101"}},
		{"件数が0", []string{"timeclock", "list", "--limit", "0"}},
		{"オフセットが負", []string{"timeclock", "list", "--offset", "-1"}},
		{"不正な従業員ID", []string{"timeclock", "types", "--employee-id", "0"}},
		{"存在しない打刻種別", []string{"timeclock", "punch", "出勤", "--yes"}},
		{"打刻種別が無い", []string{"timeclock", "punch", "--yes"}},
		{"打刻日の形式が不正", []string{"timeclock", "punch", "clock_out", "--base-date", "昨日", "--yes"}},
		{"--dry-run と --yes の同時指定", []string{"timeclock", "punch", "clock_in", "--dry-run", "--yes"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := newTestEnv(t, nil)
			env.saveLoggedIn(t, "default", false)
			if code := env.app.Execute(c.args); code != 2 {
				t.Errorf("終了コード = %d, 期待値 = 2, stderr=%s", code, env.stderr.String())
			}
		})
	}
}

// TestTimeclockPunch_Yes は、--yes を付けると確認なしで打刻を登録し、API のレスポンスを出力することを検証します。
func TestTimeclockPunch_Yes(t *testing.T) {
	var requests []recordedRequest
	env := newTestEnv(t, recordingAPI(t, http.StatusCreated, `{"employee_time_clock":{"id":10,"type":"clock_out","date":"2026-11-19"}}`, &requests))
	env.saveLoggedIn(t, "default", false)

	// 前提: 日をまたいだ退勤のため、打刻日（base_date）に前日を指定する
	code := env.app.Execute([]string{"timeclock", "punch", "clock_out", "--base-date", "2026-11-19", "--yes"})
	if code != 0 {
		t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
	}
	if len(requests) != 1 {
		t.Fatalf("API の呼び出し回数 = %d, 期待値 = 1", len(requests))
	}
	req := requests[0]
	if req.method != http.MethodPost || req.path != "/api/v1/employees/456/time_clocks" {
		t.Errorf("リクエスト = %s %s", req.method, req.path)
	}
	if req.body["company_id"] != float64(123) || req.body["type"] != "clock_out" || req.body["base_date"] != "2026-11-19" {
		t.Errorf("本文 = %v", req.body)
	}
	if _, ok := req.body["datetime"]; ok {
		t.Errorf("打刻時刻（datetime）は送らない必要があります: %v", req.body)
	}
	if !strings.Contains(env.stdout.String(), `"employee_time_clock"`) {
		t.Errorf("出力に API のレスポンスが含まれていません: %s", env.stdout.String())
	}
}

// TestTimeclockPunch_DryRun は、--dry-run では API を呼ばず、送る予定のリクエストを JSON で出力することを検証します。
func TestTimeclockPunch_DryRun(t *testing.T) {
	env := newTestEnv(t, nil)
	env.saveLoggedIn(t, "default", false)

	code := env.app.Execute([]string{"timeclock", "punch", "clock_in", "--dry-run"})
	if code != 0 {
		t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
	}
	var out struct {
		DryRun  bool           `json:"dry_run"`
		Profile string         `json:"profile"`
		Company string         `json:"company_name"`
		Method  string         `json:"method"`
		Path    string         `json:"path"`
		Body    map[string]any `json:"body"`
	}
	if err := json.Unmarshal(env.stdout.Bytes(), &out); err != nil {
		t.Fatalf("出力が JSON ではありません: %v\n%s", err, env.stdout.String())
	}
	if !out.DryRun || out.Method != http.MethodPost || out.Path != "/api/v1/employees/456/time_clocks" {
		t.Errorf("出力 = %+v", out)
	}
	if out.Profile != "default" || out.Company != "開発用テスト事業所" {
		t.Errorf("どのプロファイル・事業所に送るかが出力されていません: %+v", out)
	}
	if out.Body["company_id"] != float64(123) || out.Body["type"] != "clock_in" {
		t.Errorf("本文 = %v", out.Body)
	}
	if _, ok := out.Body["base_date"]; ok {
		t.Errorf("--base-date を省略した場合は base_date を送らない必要があります: %v", out.Body)
	}
}

// TestTimeclockPunch_NoStdinAborts は、--yes が無く標準入力も無い場合（Claude Code の Bash など）、
// 確認できないため送信せずに終了コード2になり、送る予定の内容を標準エラー出力に表示することを検証します。
func TestTimeclockPunch_NoStdinAborts(t *testing.T) {
	env := newTestEnv(t, nil)
	env.saveLoggedIn(t, "default", false)

	code := env.app.Execute([]string{"timeclock", "punch", "clock_in"})
	if code != 2 {
		t.Fatalf("終了コード = %d, 期待値 = 2", code)
	}
	stderr := env.stderr.String()
	if !strings.Contains(stderr, "/api/v1/employees/456/time_clocks") || !strings.Contains(stderr, "[y/N]") {
		t.Errorf("送る予定の内容と確認のプロンプトが表示されていません: %s", stderr)
	}
}

// TestTimeclockPunch_Confirm は、確認に y と答えた場合は送信し、n と答えた場合は送信しないことを検証します。
func TestTimeclockPunch_Confirm(t *testing.T) {
	cases := []struct {
		name      string
		input     string
		wantCode  int
		wantCalls int
	}{
		{"y と答える", "y\n", 0, 1},
		{"yes と答える", "yes\n", 0, 1},
		{"n と答える", "n\n", 2, 0},
		{"何も入力せず Enter", "\n", 2, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var requests []recordedRequest
			env := newTestEnv(t, recordingAPI(t, http.StatusCreated, `{"employee_time_clock":{"id":10}}`, &requests))
			env.saveLoggedIn(t, "default", false)
			env.app.Stdin = strings.NewReader(c.input)

			code := env.app.Execute([]string{"timeclock", "punch", "break_begin"})
			if code != c.wantCode {
				t.Errorf("終了コード = %d, 期待値 = %d, stderr=%s", code, c.wantCode, env.stderr.String())
			}
			if len(requests) != c.wantCalls {
				t.Errorf("API の呼び出し回数 = %d, 期待値 = %d", len(requests), c.wantCalls)
			}
		})
	}
}

// TestTimeclockPunch_ReadOnly は、読み取り専用のプロファイルでは --yes を付けても送信しないこと、
// ただし --dry-run（送信しない）は使えることを検証します。
func TestTimeclockPunch_ReadOnly(t *testing.T) {
	env := newTestEnv(t, nil)
	env.saveLoggedIn(t, "default", true)

	if code := env.app.Execute([]string{"timeclock", "punch", "clock_in", "--yes"}); code != 2 {
		t.Errorf("--yes の終了コード = %d, 期待値 = 2", code)
	}
	if !strings.Contains(env.stderr.String(), "読み取り専用") {
		t.Errorf("読み取り専用である旨が表示されていません: %s", env.stderr.String())
	}
	if code := env.app.Execute([]string{"timeclock", "punch", "clock_in", "--dry-run"}); code != 0 {
		t.Errorf("--dry-run の終了コード = %d, 期待値 = 0", code)
	}
}

// TestTimeclockPunch_APIError は、freee が打刻を拒否した場合（整合性の取れない打刻など）に終了コード4になることを検証します。
func TestTimeclockPunch_APIError(t *testing.T) {
	var requests []recordedRequest
	env := newTestEnv(t, recordingAPI(t, http.StatusBadRequest,
		`{"status_code":400,"errors":[{"type":"bad_request","messages":["打刻の種類が正しくありません。"]}]}`, &requests))
	env.saveLoggedIn(t, "default", false)

	code := env.app.Execute([]string{"timeclock", "punch", "break_end", "--yes"})
	if code != 4 {
		t.Fatalf("終了コード = %d, 期待値 = 4", code)
	}
	if !strings.Contains(env.stderr.String(), "打刻の種類が正しくありません。") {
		t.Errorf("API のエラー内容が表示されていません: %s", env.stderr.String())
	}
}
