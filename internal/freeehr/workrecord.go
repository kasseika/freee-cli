// workrecord.go は勤怠の確認に使う API リクエストを組み立てます。
//
//   - GET /api/v1/employees/{employee_id}/work_records/{date}                 … 日ごとの勤怠記録
//   - GET /api/v1/employees/{employee_id}/work_record_summaries/{year}/{month} … 月の集計（労働時間・残業など）
package freeehr

import (
	"fmt"
	"net/http"
)

// WorkRecordRequest は指定日（YYYY-MM-DD）の勤怠記録を取得するリクエストを返します。
func WorkRecordRequest(companyID, employeeID int, date string) Request {
	return Request{
		Method: http.MethodGet,
		Path:   fmt.Sprintf("/api/v1/employees/%d/work_records/%s", employeeID, date),
		Query:  companyQuery(companyID),
	}
}

// WorkRecordSummaryRequest は、給与支払い年月（year・month）を指定して勤怠の月次集計を取得するリクエストを返します。
// 実際の集計期間はレスポンスの start_date・end_date で表されます。
// withRecords が true の場合は、日ごとの勤怠記録も一緒に取得します。
func WorkRecordSummaryRequest(companyID, employeeID, year, month int, withRecords bool) Request {
	q := companyQuery(companyID)
	if withRecords {
		q.Set("work_records", "true")
	}
	return Request{
		Method: http.MethodGet,
		Path:   fmt.Sprintf("/api/v1/employees/%d/work_record_summaries/%d/%d", employeeID, year, month),
		Query:  q,
	}
}

// UpdateWorkRecordRequest は、指定日（YYYY-MM-DD）の勤怠記録を登録・更新するリクエストを返します（PUT）。
//
// body には API の更新の本文（company_id・work_record_segments・break_records・note・holidays など）をそのまま渡します。
// 日時は "YYYY-MM-DD HH:MM:SS" の形式です。振替出勤・振替休日・代休出勤・代休の登録は API ではできません。
func UpdateWorkRecordRequest(employeeID int, date string, body map[string]any) Request {
	return Request{
		Method: http.MethodPut,
		Path:   fmt.Sprintf("/api/v1/employees/%d/work_records/%s", employeeID, date),
		Body:   body,
	}
}
