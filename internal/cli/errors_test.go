// errors_test.go は、エラーの種類から終了コードを決める規則を検証します。
package cli

import (
	"errors"
	"fmt"
	"testing"

	"github.com/kasseika/freee-cli/internal/auth"
	"github.com/kasseika/freee-cli/internal/freeehr"
)

// TestExitCodeFor は、各エラーが設計どおりの終了コードになることを検証します。
func TestExitCodeFor(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"成功", nil, 0},
		{"ネットワークなどの一般的な失敗", errors.New("接続できませんでした"), 1},
		{"使い方の誤り", usageErrorf("日付の形式が不正です"), 2},
		{"未ログイン", fmt.Errorf("包んだエラー: %w", auth.ErrProfileNotFound), 3},
		{"トークンの更新に失敗", fmt.Errorf("包んだエラー: %w", auth.ErrTokenRefresh), 3},
		{"APIのエラー", fmt.Errorf("包んだエラー: %w", &freeehr.APIError{StatusCode: 403}), 4},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := exitCodeFor(c.err); got != c.want {
				t.Errorf("終了コード = %d, 期待値 = %d", got, c.want)
			}
		})
	}
}
