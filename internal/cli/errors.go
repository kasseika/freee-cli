// errors.go は freee-hr CLI の終了コードを決める規則を実装します。
//
// 終了コードの意味:
//
//	0: 成功
//	1: 一般的な失敗（ネットワーク・ファイルの入出力など）
//	2: 使い方の誤り（不正なフラグの値・必須の指定が無いなど）
//	3: 未認証（プロファイルが無い・トークンの更新に失敗した）
//	4: freee API がエラーを返した（429・403 を含む）
package cli

import (
	"errors"
	"fmt"

	"github.com/kasseika/freee-cli/internal/auth"
	"github.com/kasseika/freee-cli/internal/freeehr"
)

const (
	exitCodeSuccess         = 0
	exitCodeGeneral         = 1
	exitCodeUsageError      = 2
	exitCodeUnauthenticated = 3
	exitCodeAPIError        = 4
)

// exitError は終了コードを明示したエラーです。
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

// usageErrorf は使い方の誤り（終了コード2）を表すエラーを生成します。
func usageErrorf(format string, args ...any) error {
	return &exitError{code: exitCodeUsageError, err: fmt.Errorf(format, args...)}
}

// exitCodeFor はエラーの種類から終了コードを決めます。
func exitCodeFor(err error) int {
	if err == nil {
		return exitCodeSuccess
	}
	var ee *exitError
	if errors.As(err, &ee) {
		return ee.code
	}
	var apiErr *freeehr.APIError
	if errors.As(err, &apiErr) {
		return exitCodeAPIError
	}
	if errors.Is(err, auth.ErrProfileNotFound) || errors.Is(err, auth.ErrTokenRefresh) {
		return exitCodeUnauthenticated
	}
	return exitCodeGeneral
}
