// user_test.go は `freee-hr user me` コマンドを検証します。
package cli

import (
	"net/http"
	"strings"
	"testing"
)

// TestUserMe は、保存済みのトークンでログインユーザーを取得し、APIのレスポンスをJSONで出力することを検証します。
func TestUserMe(t *testing.T) {
	env := newTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/users/me" || r.Header.Get("Authorization") != "Bearer 保存済みのアクセストークン" {
			t.Errorf("リクエスト = %s, Authorization = %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"id":1,"companies":[{"id":123,"name":"開発用テスト事業所","employee_id":456}]}`))
	})
	env.saveLoggedIn(t, "default", false)

	code := env.app.Execute([]string{"user", "me"})
	if code != 0 {
		t.Fatalf("終了コード = %d, 期待値 = 0, stderr=%s", code, env.stderr.String())
	}
	if !strings.Contains(env.stdout.String(), `"name": "開発用テスト事業所"`) {
		t.Errorf("出力にAPIのレスポンスが含まれていません: %s", env.stdout.String())
	}
}

// TestUserMe_NotLoggedIn は、未ログインのプロファイルでは終了コード3になることを検証します。
func TestUserMe_NotLoggedIn(t *testing.T) {
	env := newTestEnv(t, nil)

	code := env.app.Execute([]string{"--profile", "未ログイン", "user", "me"})
	if code != 3 {
		t.Fatalf("終了コード = %d, 期待値 = 3", code)
	}
	if !strings.Contains(env.stderr.String(), "freee-hr auth login") {
		t.Errorf("ログインの案内が表示されていません: %s", env.stderr.String())
	}
}

// TestUserMe_APIError は、APIがエラーを返した場合に終了コード4になり、エラー内容を標準エラー出力に書くことを検証します。
func TestUserMe_APIError(t *testing.T) {
	env := newTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"status_code":401,"errors":[{"type":"unauthorized","messages":["アクセストークンが無効です"]}]}`))
	})
	env.saveLoggedIn(t, "default", false)

	code := env.app.Execute([]string{"user", "me"})
	if code != 4 {
		t.Fatalf("終了コード = %d, 期待値 = 4", code)
	}
	if !strings.Contains(env.stderr.String(), "エラー: ") || !strings.Contains(env.stderr.String(), "アクセストークンが無効です") {
		t.Errorf("標準エラー出力 = %s", env.stderr.String())
	}
}
