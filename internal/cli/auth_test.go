// auth_test.go は `freee-hr auth login / status / logout` コマンドを検証します。
//
// ブラウザでの「許可する」は、OpenBrowser の差し替えでコールバックURLへアクセスすることで再現します。
package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/kasseika/freee-cli/internal/auth"
)

// meResponse は、事業所123に従業員456として所属するユーザーの users/me レスポンスです。
const meResponse = `{"id":1,"companies":[` +
	`{"id":999,"name":"別の事業所","role":"self_only","employee_id":1,"display_name":"別名"},` +
	`{"id":123,"name":"開発用テスト事業所","role":"self_only","employee_id":456,"display_name":"地活 太郎"}]}`

// setupLogin は、トークンエンドポイントのモックと「許可する」を再現するブラウザを App に設定します。
// トークンレスポンスの company_id は companyID です。受け取ったフォームは forms に記録します。
func setupLogin(t *testing.T, env *testEnv, companyID int, forms *[]url.Values) {
	t.Helper()
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		*forms = append(*forms, r.PostForm)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "ログインで得たアクセストークン", "token_type": "bearer", "expires_in": 21600,
			"refresh_token": "ログインで得たリフレッシュトークン", "company_id": companyID,
		})
	}))
	t.Cleanup(tokenServer.Close)
	env.app.TokenURL = tokenServer.URL
	env.app.OpenBrowser = func(raw string) error {
		u, err := url.Parse(raw)
		if err != nil {
			return err
		}
		q := u.Query()
		callback := q.Get("redirect_uri") + "?code=" + url.QueryEscape("認可コード") + "&state=" + url.QueryEscape(q.Get("state"))
		go func() {
			resp, err := http.Get(callback)
			if err == nil {
				_ = resp.Body.Close()
			}
		}()
		return nil
	}
}

// meHandler は users/me に meResponse を返す API のモックです。
func meHandler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/users/me" || r.Header.Get("Authorization") != "Bearer ログインで得たアクセストークン" {
			t.Errorf("想定外のリクエスト: %s Authorization=%q", r.URL.Path, r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(meResponse))
	}
}

// TestAuthLogin は、ログインで得たトークンと、選んだ事業所での従業員IDをプロファイルに保存することを検証します。
func TestAuthLogin(t *testing.T) {
	env := newTestEnv(t, meHandler(t))
	var forms []url.Values
	setupLogin(t, env, 123, &forms)

	code := env.app.Execute([]string{"--profile", "テスト事業所", "auth", "login",
		"--client-id", "テスト用クライアントID", "--client-secret", "テスト用シークレット", "--port", "0"})
	if code != 0 {
		t.Fatalf("終了コード = %d, 期待値 = 0, stderr=%s", code, env.stderr.String())
	}

	creds, err := env.store.Load("テスト事業所")
	if err != nil {
		t.Fatalf("プロファイルが保存されていません: %v", err)
	}
	if creds.AccessToken != "ログインで得たアクセストークン" || creds.RefreshToken != "ログインで得たリフレッシュトークン" {
		t.Errorf("トークンが保存されていません: %+v", creds)
	}
	if creds.CompanyID != 123 || creds.CompanyName != "開発用テスト事業所" || creds.EmployeeID == nil || *creds.EmployeeID != 456 {
		t.Errorf("事業所・従業員の情報が一致しません: %+v", creds)
	}
	if creds.ClientID != "テスト用クライアントID" || creds.ClientSecret != "テスト用シークレット" || creds.ReadOnly {
		t.Errorf("クライアント情報または読み取り専用フラグが一致しません: %+v", creds)
	}

	// 検証: 結果は auth status と同じ形のJSONで出力し、トークンは出力しない
	var out map[string]any
	if err := json.Unmarshal(env.stdout.Bytes(), &out); err != nil {
		t.Fatalf("出力がJSONではありません: %v\n%s", err, env.stdout.String())
	}
	if out["company_name"] != "開発用テスト事業所" || out["employee_id"] != float64(456) {
		t.Errorf("出力 = %v", out)
	}
	if strings.Contains(env.stdout.String(), "ログインで得たアクセストークン") {
		t.Error("トークンを標準出力に出してはいけません")
	}
}

// TestAuthLogin_ReadOnly は、--read-only を付けると読み取り専用のプロファイルとして保存することを検証します。
func TestAuthLogin_ReadOnly(t *testing.T) {
	env := newTestEnv(t, meHandler(t))
	var forms []url.Values
	setupLogin(t, env, 123, &forms)

	code := env.app.Execute([]string{"auth", "login", "--read-only",
		"--client-id", "テスト用クライアントID", "--client-secret", "テスト用シークレット", "--port", "0"})
	if code != 0 {
		t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
	}
	creds, err := env.store.Load("default")
	if err != nil || !creds.ReadOnly {
		t.Fatalf("読み取り専用として保存されていません: %+v (err=%v)", creds, err)
	}
}

// TestAuthLogin_ClientCredentialsFromEnv は、クライアントID・シークレットを環境変数から読み込むことを検証します。
func TestAuthLogin_ClientCredentialsFromEnv(t *testing.T) {
	t.Setenv("FREEE_HR_CLIENT_ID", "環境変数のクライアントID")
	t.Setenv("FREEE_HR_CLIENT_SECRET", "環境変数のシークレット")
	env := newTestEnv(t, meHandler(t))
	var forms []url.Values
	setupLogin(t, env, 123, &forms)

	code := env.app.Execute([]string{"auth", "login", "--port", "0"})
	if code != 0 {
		t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
	}
	if len(forms) != 1 || forms[0].Get("client_id") != "環境変数のクライアントID" || forms[0].Get("client_secret") != "環境変数のシークレット" {
		t.Errorf("トークン要求のクライアント情報 = %v", forms)
	}
}

// TestAuthLogin_MissingClientCredentials は、クライアント情報がどこからも得られない場合に終了コード2で止まることを検証します。
func TestAuthLogin_MissingClientCredentials(t *testing.T) {
	t.Setenv("FREEE_HR_CLIENT_ID", "")
	t.Setenv("FREEE_HR_CLIENT_SECRET", "")
	env := newTestEnv(t, nil)
	browserOpened := false
	env.app.OpenBrowser = func(string) error { browserOpened = true; return nil }

	// 前提: 対話入力も空（Claude Code の Bash のように標準入力が無い）
	code := env.app.Execute([]string{"auth", "login", "--port", "0"})
	if code != 2 {
		t.Fatalf("終了コード = %d, 期待値 = 2", code)
	}
	if browserOpened {
		t.Error("クライアント情報が無いのにブラウザを開きました")
	}
}

// TestAuthLogin_CompanyNotInMe は、選んだ事業所が users/me に無い場合に失敗し、何も保存しないことを検証します。
func TestAuthLogin_CompanyNotInMe(t *testing.T) {
	env := newTestEnv(t, meHandler(t))
	var forms []url.Values
	setupLogin(t, env, 555, &forms)

	code := env.app.Execute([]string{"auth", "login",
		"--client-id", "テスト用クライアントID", "--client-secret", "テスト用シークレット", "--port", "0"})
	if code == 0 {
		t.Fatal("所属していない事業所なのにログインが成功しました")
	}
	if _, err := env.store.Load("default"); !errors.Is(err, auth.ErrProfileNotFound) {
		t.Errorf("失敗したのにプロファイルが保存されました: %v", err)
	}
}

// TestAuthLogin_NotAnEmployee は、選んだ事業所で従業員として登録されていない場合も保存し、警告を表示することを検証します。
func TestAuthLogin_NotAnEmployee(t *testing.T) {
	env := newTestEnv(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"id":1,"companies":[{"id":123,"name":"開発用テスト事業所","role":"company_admin","employee_id":null}]}`))
	})
	var forms []url.Values
	setupLogin(t, env, 123, &forms)

	code := env.app.Execute([]string{"auth", "login",
		"--client-id", "テスト用クライアントID", "--client-secret", "テスト用シークレット", "--port", "0"})
	if code != 0 {
		t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
	}
	creds, err := env.store.Load("default")
	if err != nil || creds.EmployeeID != nil {
		t.Fatalf("従業員IDが無い状態で保存されていません: %+v (err=%v)", creds, err)
	}
	if !strings.Contains(env.stderr.String(), "従業員") {
		t.Errorf("従業員として登録されていない旨の警告がありません: %s", env.stderr.String())
	}
}

// TestAuthStatus は、保存済みのプロファイルの事業所・従業員・読み取り専用の状態をJSONで出力し、秘密情報を出さないことを検証します。
func TestAuthStatus(t *testing.T) {
	env := newTestEnv(t, nil)
	env.saveLoggedIn(t, "テスト事業所", true)

	code := env.app.Execute([]string{"--profile", "テスト事業所", "auth", "status"})
	if code != 0 {
		t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
	}
	var out map[string]any
	if err := json.Unmarshal(env.stdout.Bytes(), &out); err != nil {
		t.Fatalf("出力がJSONではありません: %v", err)
	}
	if out["profile"] != "テスト事業所" || out["company_id"] != float64(123) || out["company_name"] != "開発用テスト事業所" ||
		out["employee_id"] != float64(456) || out["display_name"] != "地活 太郎" || out["read_only"] != true {
		t.Errorf("出力 = %v", out)
	}
	for _, secret := range []string{"保存済みのアクセストークン", "保存済みのリフレッシュトークン", "テスト用シークレット"} {
		if bytes.Contains(env.stdout.Bytes(), []byte(secret)) {
			t.Errorf("秘密情報 %q を出力してはいけません", secret)
		}
	}
}

// TestAuthStatus_NotLoggedIn は、未ログインのプロファイルで終了コード3になることを検証します。
func TestAuthStatus_NotLoggedIn(t *testing.T) {
	env := newTestEnv(t, nil)
	if code := env.app.Execute([]string{"auth", "status"}); code != 3 {
		t.Fatalf("終了コード = %d, 期待値 = 3", code)
	}
}

// TestAuthLogout は、ログアウトでプロファイルを削除し、未ログインなら終了コード3になることを検証します。
func TestAuthLogout(t *testing.T) {
	env := newTestEnv(t, nil)
	env.saveLoggedIn(t, "default", false)

	if code := env.app.Execute([]string{"auth", "logout"}); code != 0 {
		t.Fatalf("終了コード = %d, stderr=%s", code, env.stderr.String())
	}
	if _, err := env.store.Load("default"); !errors.Is(err, auth.ErrProfileNotFound) {
		t.Errorf("ログアウト後もプロファイルが残っています: %v", err)
	}
	if code := env.app.Execute([]string{"auth", "logout"}); code != 3 {
		t.Errorf("2回目のログアウトの終了コード = %d, 期待値 = 3", code)
	}
}
