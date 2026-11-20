// testhelpers_test.go は internal/cli の各テストで共有するヘルパーです。
//
// テストでは、認証情報ファイルを一時ディレクトリに置き、freee の API・トークンエンドポイントを
// httptest のサーバーで置き換えます。
package cli

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/kasseika/freee-cli/internal/auth"
)

// testEnv はテスト用の App と、その入出力・認証情報ファイルをまとめたものです。
type testEnv struct {
	app    *App
	stdout *bytes.Buffer
	stderr *bytes.Buffer
	store  *auth.Store
}

// newTestEnv は、api を freee人事労務 API のモックとして使う App を生成します。
// api が nil の場合、API が呼ばれるとテストを失敗させます。
func newTestEnv(t *testing.T, api http.HandlerFunc) *testEnv {
	t.Helper()
	if api == nil {
		api = func(w http.ResponseWriter, r *http.Request) {
			t.Errorf("API は呼ばれるべきではありません: %s %s", r.Method, r.URL)
		}
	}
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)

	path := filepath.Join(t.TempDir(), "credentials.json")
	env := &testEnv{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}, store: auth.NewStore(path)}
	env.app = &App{
		Stdout:          env.stdout,
		Stderr:          env.stderr,
		Stdin:           bytes.NewReader(nil),
		CredentialsPath: path,
		APIBaseURL:      server.URL,
		AuthURL:         "https://accounts.example.test/public_api/authorize",
		TokenURL:        "http://127.0.0.1:1/token",
		OpenBrowser:     func(string) error { return nil },
	}
	return env
}

// saveLoggedIn は、有効なアクセストークンを持つログイン済みのプロファイルを保存します。
func (e *testEnv) saveLoggedIn(t *testing.T, profile string, readOnly bool) {
	t.Helper()
	employeeID := 456
	err := e.store.Save(context.Background(), profile, &auth.Credentials{
		ClientID:     "テスト用クライアントID",
		ClientSecret: "テスト用シークレット",
		AccessToken:  "保存済みのアクセストークン",
		RefreshToken: "保存済みのリフレッシュトークン",
		Expiry:       time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC),
		CompanyID:    123,
		CompanyName:  "開発用テスト事業所",
		EmployeeID:   &employeeID,
		DisplayName:  "地活 太郎",
		ReadOnly:     readOnly,
	})
	if err != nil {
		t.Fatal(err)
	}
}
