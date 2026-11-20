// timeclock.go は打刻（タイムレコーダー）の API リクエストを組み立てます。
//
//   - GET  /api/v1/employees/{employee_id}/time_clocks/available_types … いま打刻できる種類
//   - GET  /api/v1/employees/{employee_id}/time_clocks                 … 打刻の一覧
//   - POST /api/v1/employees/{employee_id}/time_clocks                 … 打刻の登録
//
// 打刻時刻（datetime）は管理者だけが指定できるため、v0.1 では扱いません。打刻は常に「いまの時刻」になります。
package freeehr

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// TimeClockTypes は打刻の種類の一覧です（出勤・休憩開始・休憩終了・退勤）。
var TimeClockTypes = []string{"clock_in", "break_begin", "break_end", "clock_out"}

// IsTimeClockType は s が打刻の種類として正しいかを返します。
func IsTimeClockType(s string) bool {
	for _, t := range TimeClockTypes {
		if s == t {
			return true
		}
	}
	return false
}

func timeClocksPath(employeeID int) string {
	return fmt.Sprintf("/api/v1/employees/%d/time_clocks", employeeID)
}

func companyQuery(companyID int) url.Values {
	return url.Values{"company_id": {strconv.Itoa(companyID)}}
}

// AvailableTimeClockTypesRequest は、指定日（空文字列なら当日）に打刻できる種類を取得するリクエストを返します。
func AvailableTimeClockTypesRequest(companyID, employeeID int, date string) Request {
	q := companyQuery(companyID)
	if date != "" {
		q.Set("date", date)
	}
	return Request{Method: http.MethodGet, Path: timeClocksPath(employeeID) + "/available_types", Query: q}
}

// ListTimeClocksOptions は打刻一覧の絞り込み条件です。空文字列・nil の項目は送らず、API の既定値を使います。
type ListTimeClocksOptions struct {
	// FromDate / ToDate は期間の開始日・終了日（YYYY-MM-DD）です。既定は当月の打刻開始日から当日までです。
	FromDate, ToDate string
	// Limit は取得件数（1〜100、既定50）、Offset は取得開始位置（既定0）です。
	Limit, Offset *int
}

// ListTimeClocksRequest は打刻の一覧を取得するリクエストを返します。
func ListTimeClocksRequest(companyID, employeeID int, opts ListTimeClocksOptions) Request {
	q := companyQuery(companyID)
	if opts.FromDate != "" {
		q.Set("from_date", opts.FromDate)
	}
	if opts.ToDate != "" {
		q.Set("to_date", opts.ToDate)
	}
	if opts.Limit != nil {
		q.Set("limit", strconv.Itoa(*opts.Limit))
	}
	if opts.Offset != nil {
		q.Set("offset", strconv.Itoa(*opts.Offset))
	}
	return Request{Method: http.MethodGet, Path: timeClocksPath(employeeID), Query: q}
}

// PunchBody は打刻の登録で送る本文です。
type PunchBody struct {
	CompanyID int    `json:"company_id"`
	Type      string `json:"type"`
	// BaseDate は打刻日（YYYY-MM-DD）です。日をまたぐ打刻では前日を指定します。空文字列の場合は送りません。
	BaseDate string `json:"base_date,omitempty"`
}

// PunchRequest は打刻を登録するリクエストを返します。
func PunchRequest(employeeID int, body PunchBody) Request {
	return Request{Method: http.MethodPost, Path: timeClocksPath(employeeID), Body: body}
}
