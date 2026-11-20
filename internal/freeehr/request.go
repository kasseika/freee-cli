// request.go は API リクエストを「送る前に内容を確かめられる値」として表す Request 型を定義します。
//
// 書き込み系のコマンドは、送信の前に --dry-run や確認プロンプトで「どのパスに何を送るか」を
// 利用者に示します。そのため、API のパスや本文の組み立ては各リソースの関数（timeclock.go など）が
// Request として返し、送信は Client.Send で行います。API のパスはこのパッケージの中だけで扱います。
package freeehr

import (
	"context"
	"encoding/json"
	"net/url"
)

// Request は1回分の API リクエストの内容です。--dry-run の出力にもそのまま使います。
type Request struct {
	// Method は HTTP メソッドです（GET・POST など）。
	Method string `json:"method"`
	// Path はベースURLからのパスです（例: /api/v1/employees/1/time_clocks）。
	Path string `json:"path"`
	// Query はクエリパラメータです。
	Query url.Values `json:"query,omitempty"`
	// Body は JSON として送る本文です。本文の無いリクエストでは nil です。
	Body any `json:"body,omitempty"`
}

// Send は Request を送信し、成功時のレスポンス本文を返します。
func (c *Client) Send(ctx context.Context, req Request) (json.RawMessage, error) {
	return c.Do(ctx, req.Method, req.Path, req.Query, req.Body)
}
