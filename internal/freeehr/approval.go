// approval.go は申請（ワークフロー）と申請経路の API リクエストを組み立てます。
//
//   - GET /api/v1/approval_flow_routes      … 申請経路の一覧（申請の作成に必要な approval_flow_route_id を調べる）
//   - GET /api/v1/approval_flow_routes/{id} … 申請経路の詳細（承認のステップ）
//
// 部門役職データ連携を使う申請経路（役職指定など）は、API では参照だけができ、申請の作成には使えません。
package freeehr

import (
	"fmt"
	"net/http"
)

// AttendanceWorkflowUsage は、勤怠申請に使える申請経路を表す申請種別です。
const AttendanceWorkflowUsage = "AttendanceWorkflow"

// ApprovalFlowRoutesRequest は申請経路の一覧を取得するリクエストを返します。usage が空文字列の場合は申請種別で絞り込みません。
func ApprovalFlowRoutesRequest(companyID int, usage string) Request {
	q := companyQuery(companyID)
	if usage != "" {
		q.Set("usage", usage)
	}
	return Request{Method: http.MethodGet, Path: "/api/v1/approval_flow_routes", Query: q}
}

// ApprovalFlowRouteRequest は申請経路の詳細を取得するリクエストを返します。
func ApprovalFlowRouteRequest(companyID, routeID int) Request {
	return Request{Method: http.MethodGet, Path: fmt.Sprintf("/api/v1/approval_flow_routes/%d", routeID), Query: companyQuery(companyID)}
}
