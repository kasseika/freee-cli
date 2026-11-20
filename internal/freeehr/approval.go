// approval.go は申請（ワークフロー）と申請経路の API リクエストを組み立てます。
//
//   - GET /api/v1/approval_flow_routes      … 申請経路の一覧（申請の作成に必要な approval_flow_route_id を調べる）
//   - GET /api/v1/approval_flow_routes/{id} … 申請経路の詳細（承認のステップ）
//   - GET /api/v1/approval_requests/{種類}       … 申請の一覧
//   - GET /api/v1/approval_requests/{種類}/{id}  … 申請の詳細
//   - POST /api/v1/approval_requests/{種類}      … 申請の作成
//   - GET /api/v1/approval_requests/overtime_works/setting … 残業申請に必要な設定（勤怠に反映するか・所定の出退勤時刻）
//
// 申請の種類は ApprovalRequestKind で表します。承認者（approver_id）は従業員IDではなくユーザーIDです。
//
// 部門役職データ連携を使う申請経路（役職指定など）は、API では参照だけができ、申請の作成には使えません。
package freeehr

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
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

// ApprovalRequestKind は申請の種類で、API のパスの一部（/api/v1/approval_requests/{種類}）です。
type ApprovalRequestKind string

const (
	// WorkTimeRequest は勤務時間修正申請です。
	WorkTimeRequest ApprovalRequestKind = "work_times"
	// PaidHolidayRequest は有給休暇申請です。
	PaidHolidayRequest ApprovalRequestKind = "paid_holidays"
	// SpecialHolidayRequest は特別休暇申請です。
	SpecialHolidayRequest ApprovalRequestKind = "special_holidays"
	// OvertimeWorkRequest は残業申請です。
	OvertimeWorkRequest ApprovalRequestKind = "overtime_works"
	// MonthlyAttendanceRequest は月次勤怠締め申請です。
	MonthlyAttendanceRequest ApprovalRequestKind = "monthly_attendances"
)

// ApprovalRequestStatuses は、申請の一覧の絞り込みに使える申請の状態です
// （draft: 下書き、in_progress: 申請中、approved: 承認済、feedback: 差戻し）。
var ApprovalRequestStatuses = []string{"draft", "in_progress", "approved", "feedback"}

// ListApprovalRequestsOptions は申請の一覧の絞り込み条件です。空文字列・nil の項目は送らず、API の既定値を使います。
type ListApprovalRequestsOptions struct {
	// Status は申請の状態（ApprovalRequestStatuses のいずれか）です。
	Status string
	// StartTargetDate / EndTargetDate は、申請の対象日の期間（YYYY-MM-DD）です。
	StartTargetDate, EndTargetDate string
	// ApplicantID は申請者のユーザーIDです。
	ApplicantID *int
	// Limit は取得件数（1〜100、既定50）、Offset は取得開始位置（既定0）です。
	Limit, Offset *int
}

// approvalRequestsPath は申請の種類ごとのパスを返します。
func approvalRequestsPath(kind ApprovalRequestKind) string {
	return "/api/v1/approval_requests/" + string(kind)
}

// ListApprovalRequestsRequest は申請の一覧を取得するリクエストを返します。
func ListApprovalRequestsRequest(companyID int, kind ApprovalRequestKind, opts ListApprovalRequestsOptions) Request {
	q := companyQuery(companyID)
	setString := func(key, value string) {
		if value != "" {
			q.Set(key, value)
		}
	}
	setInt := func(key string, value *int) {
		if value != nil {
			q.Set(key, strconv.Itoa(*value))
		}
	}
	setString("status", opts.Status)
	setString("start_target_date", opts.StartTargetDate)
	setString("end_target_date", opts.EndTargetDate)
	setInt("applicant_id", opts.ApplicantID)
	setInt("limit", opts.Limit)
	setInt("offset", opts.Offset)
	return Request{Method: http.MethodGet, Path: approvalRequestsPath(kind), Query: q}
}

// ApprovalRequestRequest は申請IDを指定して1件の申請を取得するリクエストを返します。
func ApprovalRequestRequest(companyID int, kind ApprovalRequestKind, id int) Request {
	return Request{Method: http.MethodGet, Path: fmt.Sprintf("%s/%d", approvalRequestsPath(kind), id), Query: companyQuery(companyID)}
}

// CreateApprovalRequestRequest は申請を作成するリクエストを返します（POST）。
//
// body には API の作成の本文（company_id・target_date・approval_flow_route_id・approver_id・comment と種類ごとの項目）を
// そのまま渡します。申請時に承認者を選ぶ型の申請経路（ステップの resource_type が selected_user）では、approver_id が必要です。
func CreateApprovalRequestRequest(kind ApprovalRequestKind, body map[string]any) Request {
	return Request{Method: http.MethodPost, Path: approvalRequestsPath(kind), Body: body}
}

// OvertimeWorkSettingRequest は、指定日（YYYY-MM-DD）の残業申請に必要な設定を取得するリクエストを返します。
// 設定の should_reflect_in_work_record によって、残業申請の作成で指定する時刻の項目が変わります。
func OvertimeWorkSettingRequest(companyID int, date string) Request {
	q := companyQuery(companyID)
	q.Set("date", date)
	return Request{Method: http.MethodGet, Path: approvalRequestsPath(OvertimeWorkRequest) + "/setting", Query: q}
}

// OvertimeWorkSetting は残業申請に必要な設定のうち、CLI が判断に使う項目です。
type OvertimeWorkSetting struct {
	// ShouldReflectInWorkRecord は、残業申請を勤怠に反映する設定かどうかです。
	// true の場合、残業申請は早出・残業の時刻（early_work_*・overtime_work_*）で作ります。
	ShouldReflectInWorkRecord bool
}

// ParseOvertimeWorkSetting は GET /api/v1/approval_requests/overtime_works/setting のレスポンスを解析します。
// 必須の should_reflect_in_work_record が無い場合は、設定を判断できないためエラーにします。
func ParseOvertimeWorkSetting(raw json.RawMessage) (*OvertimeWorkSetting, error) {
	var parsed struct {
		ShouldReflectInWorkRecord *bool `json:"should_reflect_in_work_record"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("残業申請の設定の解析に失敗しました: %w", err)
	}
	if parsed.ShouldReflectInWorkRecord == nil {
		return nil, fmt.Errorf("残業申請の設定に should_reflect_in_work_record がありません: %s", raw)
	}
	return &OvertimeWorkSetting{ShouldReflectInWorkRecord: *parsed.ShouldReflectInWorkRecord}, nil
}
