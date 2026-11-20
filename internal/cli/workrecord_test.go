// workrecord_test.go は `freee-hr work-record get / summary`（日ごとの勤怠と月の集計の取得）を検証します。
package cli

import (
	"net/http"
	"strings"
	"testing"
)

// TestWorkRecordGet は、指定日の勤怠記録を取得し、API のレスポンスを出力することを検証します。
func TestWorkRecordGet(t *testing.T) {
	var requests []recordedRequest
	env := newTestEnv(t, recordingAPI(t, http.StatusOK, `{"date":"2026-11-20T00:00:00.000+09:00","normal_work_mins":480}`, &requests))
	env.saveLoggedIn(t, "default", false)

	code := env.app.Execute([]string{"work-record", "get", "2026-11-20"})
	if code != 0 {
		t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
	}
	req := requests[0]
	if req.method != http.MethodGet || req.path != "/api/v1/employees/456/work_records/2026-11-20" || req.query["company_id"] != "123" {
		t.Errorf("リクエスト = %+v", req)
	}
	if !strings.Contains(env.stdout.String(), `"normal_work_mins": 480`) {
		t.Errorf("出力に API のレスポンスが含まれていません: %s", env.stdout.String())
	}
}

// TestWorkRecordSummary は、月を指定して勤怠の月次集計を取得することを検証します。
func TestWorkRecordSummary(t *testing.T) {
	var requests []recordedRequest
	env := newTestEnv(t, recordingAPI(t, http.StatusOK, `{"year":2026,"month":11,"total_work_mins":9600}`, &requests))
	env.saveLoggedIn(t, "default", false)

	code := env.app.Execute([]string{"work-record", "summary", "2026-11", "--employee-id", "789"})
	if code != 0 {
		t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
	}
	req := requests[0]
	if req.method != http.MethodGet || req.path != "/api/v1/employees/789/work_record_summaries/2026/11" || req.query["company_id"] != "123" {
		t.Errorf("リクエスト = %+v", req)
	}
	if _, ok := req.query["work_records"]; ok {
		t.Errorf("--with-records を省略した場合は work_records を送らない必要があります: %v", req.query)
	}
	if !strings.Contains(env.stdout.String(), `"total_work_mins": 9600`) {
		t.Errorf("出力に API のレスポンスが含まれていません: %s", env.stdout.String())
	}
}

// TestWorkRecordSummary_WithRecords は、--with-records で日ごとの勤怠記録も一緒に取得することを検証します。
func TestWorkRecordSummary_WithRecords(t *testing.T) {
	var requests []recordedRequest
	env := newTestEnv(t, recordingAPI(t, http.StatusOK, `{"year":2026,"month":1,"work_records":[]}`, &requests))
	env.saveLoggedIn(t, "default", false)

	code := env.app.Execute([]string{"work-record", "summary", "2026-01", "--with-records"})
	if code != 0 {
		t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
	}
	req := requests[0]
	if req.path != "/api/v1/employees/456/work_record_summaries/2026/1" || req.query["work_records"] != "true" {
		t.Errorf("リクエスト = %+v", req)
	}
}

// TestWorkRecordUsageErrors は、不正な日付・年月・引数の数で API を呼ばずに終了コード2になることを検証します。
func TestWorkRecordUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"日付の形式が不正", []string{"work-record", "get", "20261120"}},
		{"存在しない日付", []string{"work-record", "get", "2026-11-31"}},
		{"日付が無い", []string{"work-record", "get"}},
		{"年月の形式が不正", []string{"work-record", "summary", "2026/11"}},
		{"存在しない月", []string{"work-record", "summary", "2026-13"}},
		{"年月が無い", []string{"work-record", "summary"}},
		{"引数が多すぎる", []string{"work-record", "get", "2026-11-20", "2026-11-21"}},
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

// TestWorkRecordGet_NotLoggedIn は、未ログインのプロファイルで終了コード3になることを検証します。
func TestWorkRecordGet_NotLoggedIn(t *testing.T) {
	env := newTestEnv(t, nil)
	if code := env.app.Execute([]string{"work-record", "get", "2026-11-20"}); code != 3 {
		t.Fatalf("終了コード = %d, 期待値 = 3", code)
	}
}
