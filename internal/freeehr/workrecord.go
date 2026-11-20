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

// WorkRecordSummaryRequest は指定した年月の勤怠の月次集計を取得するリクエストを返します。
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
