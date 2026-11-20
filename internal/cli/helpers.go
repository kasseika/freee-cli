// helpers.go は各コマンドで共通して使う小さなユーティリティです。
package cli

import (
	"context"
	"encoding/json"
	"io"
	"time"

	"github.com/spf13/cobra"
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

// usageArgs は cobra の引数検証を包み、検証の失敗を使い方の誤り（終了コード2）にします。
func usageArgs(validate cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := validate(cmd, args); err != nil {
			return &exitError{code: exitCodeUsageError, err: err}
		}
		return nil
	}
}

// noArgs は引数を受け付けないコマンドの引数検証です。余分な引数は使い方の誤り（終了コード2）にします。
var noArgs = usageArgs(cobra.NoArgs)

// newGroupCmd はサブコマンドをまとめるだけのコマンド（auth・user など）を生成します。
// サブコマンドを省略した場合はヘルプを表示し、存在しないサブコマンドは使い方の誤り（終了コード2）にします。
func newGroupCmd(use, short string) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
}
