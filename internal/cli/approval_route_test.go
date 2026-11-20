// approval_route_test.go は `freee-hr approval route list / get`（申請経路の一覧と詳細）を検証します。
//
// 申請の作成には申請経路ID（approval_flow_route_id）が必須のため、まず使える経路を調べられるようにします。
package cli

import (
	"net/http"
	"strings"
	"testing"
)

// TestApprovalRouteList は、既定で勤怠申請（AttendanceWorkflow）に使える申請経路の一覧を取得することを検証します。
func TestApprovalRouteList(t *testing.T) {
	var requests []recordedRequest
	env := newTestEnv(t, recordingAPI(t, http.StatusOK,
		`{"approval_flow_routes":[{"id":11,"name":"勤怠申請の経路","usages":["AttendanceWorkflow"]}]}`, &requests))
	env.saveLoggedIn(t, "default", false)

	code := env.app.Execute([]string{"approval", "route", "list"})
	if code != 0 {
		t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
	}
	req := requests[0]
	if req.method != http.MethodGet || req.path != "/api/v1/approval_flow_routes" {
		t.Errorf("リクエスト = %s %s", req.method, req.path)
	}
	if req.query["company_id"] != "123" || req.query["usage"] != "AttendanceWorkflow" {
		t.Errorf("クエリ = %v", req.query)
	}
	if !strings.Contains(env.stdout.String(), "勤怠申請の経路") {
		t.Errorf("出力に API のレスポンスが含まれていません: %s", env.stdout.String())
	}
}

// TestApprovalRouteList_AllUsages は、--all で申請種別を絞り込まずに取得することを検証します。
func TestApprovalRouteList_AllUsages(t *testing.T) {
	var requests []recordedRequest
	env := newTestEnv(t, recordingAPI(t, http.StatusOK, `{"approval_flow_routes":[]}`, &requests))
	env.saveLoggedIn(t, "default", false)

	if code := env.app.Execute([]string{"approval", "route", "list", "--all"}); code != 0 {
		t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
	}
	if _, ok := requests[0].query["usage"]; ok {
		t.Errorf("--all では usage を送らない必要があります: %v", requests[0].query)
	}
}

// TestApprovalRouteList_NotAnEmployee は、申請経路の一覧は従業員IDを使わないため、
// 従業員として登録されていないプロファイルでも呼べることを検証します（可否は freee が判断します）。
func TestApprovalRouteList_NotAnEmployee(t *testing.T) {
	var requests []recordedRequest
	env := newTestEnv(t, recordingAPI(t, http.StatusOK, `{"approval_flow_routes":[]}`, &requests))
	env.saveNotEmployee(t, "default")

	if code := env.app.Execute([]string{"approval", "route", "list"}); code != 0 {
		t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
	}
	if len(requests) != 1 {
		t.Errorf("API の呼び出し回数 = %d, 期待値 = 1", len(requests))
	}
}

// TestApprovalRouteGet は、申請経路の詳細（承認のステップ）を取得することを検証します。
func TestApprovalRouteGet(t *testing.T) {
	var requests []recordedRequest
	env := newTestEnv(t, recordingAPI(t, http.StatusOK, `{"approval_flow_route":{"id":11,"name":"勤怠申請の経路","steps":[]}}`, &requests))
	env.saveLoggedIn(t, "default", false)

	if code := env.app.Execute([]string{"approval", "route", "get", "11"}); code != 0 {
		t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
	}
	req := requests[0]
	if req.method != http.MethodGet || req.path != "/api/v1/approval_flow_routes/11" || req.query["company_id"] != "123" {
		t.Errorf("リクエスト = %+v", req)
	}
}

// TestApprovalRouteUsageErrors は、不正なIDで API を呼ばずに終了コード2になることを検証します。
func TestApprovalRouteUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"IDが数値ではない", []string{"approval", "route", "get", "勤怠"}},
		{"IDが0", []string{"approval", "route", "get", "0"}},
		{"IDが無い", []string{"approval", "route", "get"}},
		{"一覧に余分な引数", []string{"approval", "route", "list", "11"}},
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
