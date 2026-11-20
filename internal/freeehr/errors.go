// errors.go は freee人事労務 API のエラーレスポンスを Go のエラー型に変換します。
//
// freee の共通エラーレスポンスは {"status_code":400,"errors":[{"type":"...","messages":["..."]}]} の形です。
// 一部のエラーは {"message":"..."} の形で返るため、どちらにも対応し、JSON でない本文はそのまま残します。
package freeehr

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// APIError は freee API がエラーのステータスコードを返したときのエラーです。
type APIError struct {
	// StatusCode は HTTP のステータスコードです。
	StatusCode int
	// Messages は API が返したエラーの説明です。
	Messages []string
	// RateLimitReset は 429 のときの X-RateLimit-Reset ヘッダー（回数制限が解除される時刻）です。
	RateLimitReset string
}

// Error は error インタフェースを満たします。
func (e *APIError) Error() string {
	msg := fmt.Sprintf("freee API がエラーを返しました (status=%d)", e.StatusCode)
	if len(e.Messages) > 0 {
		msg += ": " + strings.Join(e.Messages, "; ")
	}
	if e.RateLimitReset != "" {
		msg += fmt.Sprintf("（リクエスト回数の制限は %s に解除されます）", e.RateLimitReset)
	}
	return msg
}

// errorBody は freee のエラーレスポンスの形です。
type errorBody struct {
	Errors []struct {
		Messages []string `json:"messages"`
	} `json:"errors"`
	Message string `json:"message"`
}

// newAPIError はエラーレスポンスを APIError に変換します。
func newAPIError(resp *http.Response, body []byte) *APIError {
	apiErr := &APIError{StatusCode: resp.StatusCode}
	if resp.StatusCode == http.StatusTooManyRequests {
		apiErr.RateLimitReset = resp.Header.Get("X-RateLimit-Reset")
	}

	var parsed errorBody
	if err := json.Unmarshal(body, &parsed); err != nil {
		if text := strings.TrimSpace(string(body)); text != "" {
			apiErr.Messages = []string{text}
		}
		return apiErr
	}
	for _, e := range parsed.Errors {
		apiErr.Messages = append(apiErr.Messages, e.Messages...)
	}
	if parsed.Message != "" {
		apiErr.Messages = append(apiErr.Messages, parsed.Message)
	}
	return apiErr
}
