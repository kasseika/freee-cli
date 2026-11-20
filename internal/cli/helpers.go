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

const (
	// dateLayout は日付の引数・フラグの形式（YYYY-MM-DD）です。
	dateLayout = "2006-01-02"
	// yearMonthLayout は年月の引数の形式（YYYY-MM）です。
	yearMonthLayout = "2006-01"
)

// parseDate は YYYY-MM-DD 形式の日付を検証し、そのまま返します。存在しない日付（2月30日など）も使い方の誤りにします。
func parseDate(name, value string) (string, error) {
	if _, err := time.Parse(dateLayout, value); err != nil {
		return "", usageErrorf("%s は YYYY-MM-DD 形式の正しい日付で指定してください: %q", name, value)
	}
	return value, nil
}

// parseOptionalDate は、空文字列（省略）ならそのまま返し、指定があれば parseDate で検証します。
func parseOptionalDate(name, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	return parseDate(name, value)
}

// parseYearMonth は YYYY-MM 形式の年月を検証し、年と月を返します。
func parseYearMonth(name, value string) (year, month int, err error) {
	t, err := time.Parse(yearMonthLayout, value)
	if err != nil {
		return 0, 0, usageErrorf("%s は YYYY-MM 形式の正しい年月で指定してください: %q", name, value)
	}
	return t.Year(), int(t.Month()), nil
}
