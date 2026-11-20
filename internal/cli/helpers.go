// helpers.go は各コマンドで共通して使う小さなユーティリティです。
package cli

import (
	"context"
	"encoding/json"
	"io"
	"time"
)

// defaultTimeoutSeconds は --timeout の既定値（秒）です。
const defaultTimeoutSeconds = 30

// timeoutContext は --timeout の値でタイムアウト付きのコンテキストを生成します。
func timeoutContext(parent context.Context, flags *globalFlags) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, time.Duration(flags.timeout)*time.Second)
}

// writeJSON は v をインデント付きのJSONで書き込みます。
// 日本語や記号をそのまま読めるよう、HTMLエスケープはしません。
// json.RawMessage（APIのレスポンス）を渡した場合も、同じ形式に整形して出力します。
func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
