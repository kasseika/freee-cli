// login_test.go は OAuth 2.0 認可コードフロー（ループバック方式・手動貼り付け方式）を検証します。
//
// freee の認可サーバー・トークンエンドポイントは httptest のサーバーで置き換え、
// ブラウザでの操作は OpenBrowser の差し替えで再現します。
package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// tokenRequest はモックのトークンエンドポイントが受け取ったフォームの内容です。
type tokenRequest struct {
	form url.Values
}

// newTokenServer は、受け取ったフォームを記録して固定のトークンを返すモックのトークンエンドポイントを起動します。
// freee のトークンレスポンスには、選択された事業所の company_id が含まれます。
func newTokenServer(t *testing.T, received *[]tokenRequest) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("フォームの解析に失敗しました: %v", err)
		}
		*received = append(*received, tokenRequest{form: r.PostForm})
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "新しいアクセストークン",
			"token_type":    "bearer",
			"expires_in":    21600,
			"refresh_token": "新しいリフレッシュトークン",
			"scope":         "read write",
			"company_id":    123,
		})
	}))
	t.Cleanup(server.Close)
	return server
}

// TestLogin_Loopback は、ループバック方式で認可コードを受け取り、トークンを取得できることを検証します。
func TestLogin_Loopback(t *testing.T) {
	var received []tokenRequest
	tokenServer := newTokenServer(t, &received)

	// 前提: ブラウザでの「許可する」を、認可URLのredirect_uriへコードとstateを付けてアクセスすることで再現する
	var authURL *url.URL
	openBrowser := func(raw string) error {
		u, err := url.Parse(raw)
		if err != nil {
			return err
		}
		authURL = u
		q := u.Query()
		callback := q.Get("redirect_uri") + "?code=" + url.QueryEscape("認可コード123") + "&state=" + url.QueryEscape(q.Get("state"))
		go func() {
			resp, err := http.Get(callback)
			if err == nil {
				_ = resp.Body.Close()
			}
		}()
		return nil
	}

	token, err := Login(context.Background(), LoginOptions{
		ClientID:     "テスト用クライアントID",
		ClientSecret: "テスト用シークレット",
		Port:         0,
		AuthURL:      "https://accounts.example.test/public_api/authorize",
		TokenURL:     tokenServer.URL,
		OpenBrowser:  openBrowser,
		Stdin:        strings.NewReader(""),
		Stderr:       &bytes.Buffer{},
		Timeout:      5 * time.Second,
	})
	if err != nil {
		t.Fatalf("ログインに失敗しました: %v", err)
	}

	// 検証: 認可URLに事業所選択（prompt=select_company）とループバックのコールバックURLが含まれる
	q := authURL.Query()
	if q.Get("prompt") != "select_company" {
		t.Errorf("prompt = %q, 期待値 = select_company", q.Get("prompt"))
	}
	if q.Get("response_type") != "code" || q.Get("client_id") != "テスト用クライアントID" {
		t.Errorf("認可URLのパラメータが不正です: %s", authURL)
	}
	if !strings.HasPrefix(q.Get("redirect_uri"), "http://localhost:") || !strings.HasSuffix(q.Get("redirect_uri"), "/callback") {
		t.Errorf("redirect_uri = %q, 期待値 = http://localhost:<port>/callback", q.Get("redirect_uri"))
	}

	// 検証: トークンエンドポイントへ認可コードとクライアント情報をフォームの本文で送っている
	if len(received) != 1 {
		t.Fatalf("トークンエンドポイントの呼び出し回数 = %d, 期待値 = 1", len(received))
	}
	form := received[0].form
	if form.Get("grant_type") != "authorization_code" || form.Get("code") != "認可コード123" {
		t.Errorf("トークン要求のフォームが不正です: %v", form)
	}
	if form.Get("client_id") != "テスト用クライアントID" || form.Get("client_secret") != "テスト用シークレット" {
		t.Errorf("クライアント情報がフォームに含まれていません: %v", form)
	}

	// 検証: 取得したトークンと、トークンレスポンスの company_id
	if token.AccessToken != "新しいアクセストークン" || token.RefreshToken != "新しいリフレッシュトークン" {
		t.Errorf("トークンが一致しません: %+v", token)
	}
	companyID, err := CompanyIDFromToken(token)
	if err != nil || companyID != 123 {
		t.Errorf("company_id = %d (err=%v), 期待値 = 123", companyID, err)
	}
}

// TestLogin_LoopbackStateMismatch は、コールバックのstateが一致しない場合に中断することを検証します。
func TestLogin_LoopbackStateMismatch(t *testing.T) {
	var received []tokenRequest
	tokenServer := newTokenServer(t, &received)

	// 前提: 攻撃者が用意した別のstateでコールバックが呼ばれる
	openBrowser := func(raw string) error {
		u, _ := url.Parse(raw)
		callback := u.Query().Get("redirect_uri") + "?code=" + url.QueryEscape("偽の認可コード") + "&state=" + url.QueryEscape("別のstate")
		go func() {
			resp, err := http.Get(callback)
			if err == nil {
				_ = resp.Body.Close()
			}
		}()
		return nil
	}

	_, err := Login(context.Background(), LoginOptions{
		ClientID: "テスト用クライアントID", ClientSecret: "テスト用シークレット",
		AuthURL: "https://accounts.example.test/public_api/authorize", TokenURL: tokenServer.URL,
		OpenBrowser: openBrowser, Stdin: strings.NewReader(""), Stderr: &bytes.Buffer{}, Timeout: 5 * time.Second,
	})
	if err == nil {
		t.Fatal("stateが一致しないのにログインが成功しました")
	}
	if len(received) != 0 {
		t.Errorf("stateが一致しない場合はトークンを要求してはいけません（呼び出し回数 = %d）", len(received))
	}
}

// TestLogin_Timeout は、コールバックが来ないまま待ち時間を過ぎると失敗することを検証します。
func TestLogin_Timeout(t *testing.T) {
	_, err := Login(context.Background(), LoginOptions{
		ClientID: "テスト用クライアントID", ClientSecret: "テスト用シークレット",
		AuthURL: "https://accounts.example.test/public_api/authorize", TokenURL: "http://127.0.0.1:1/token",
		OpenBrowser: func(string) error { return nil }, Stdin: strings.NewReader(""), Stderr: &bytes.Buffer{},
		Timeout: 100 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("タイムアウトしたのにログインが成功しました")
	}
}

// TestLogin_Manual は、手動貼り付け方式（urn:ietf:wg:oauth:2.0:oob）で、標準入力の認可コードからトークンを取得できることを検証します。
func TestLogin_Manual(t *testing.T) {
	var received []tokenRequest
	tokenServer := newTokenServer(t, &received)

	var authURL *url.URL
	stderr := &bytes.Buffer{}
	// 前提: 利用者がブラウザに表示された認可コードを貼り付けて Enter を押す
	token, err := Login(context.Background(), LoginOptions{
		ClientID: "テスト用クライアントID", ClientSecret: "テスト用シークレット",
		Manual:  true,
		AuthURL: "https://accounts.example.test/public_api/authorize", TokenURL: tokenServer.URL,
		OpenBrowser: func(raw string) error {
			authURL, _ = url.Parse(raw)
			return nil
		},
		Stdin:   strings.NewReader("  貼り付けた認可コード  \n"),
		Stderr:  stderr,
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("ログインに失敗しました: %v", err)
	}
	if got := authURL.Query().Get("redirect_uri"); got != "urn:ietf:wg:oauth:2.0:oob" {
		t.Errorf("redirect_uri = %q, 期待値 = urn:ietf:wg:oauth:2.0:oob", got)
	}
	if authURL.Query().Get("prompt") != "select_company" {
		t.Errorf("手動方式でも prompt=select_company を付ける必要があります: %s", authURL)
	}
	if len(received) != 1 || received[0].form.Get("code") != "貼り付けた認可コード" {
		t.Fatalf("前後の空白を除いた認可コードでトークンを要求する必要があります: %+v", received)
	}
	if received[0].form.Get("redirect_uri") != "urn:ietf:wg:oauth:2.0:oob" {
		t.Errorf("トークン要求の redirect_uri = %q", received[0].form.Get("redirect_uri"))
	}
	if token.AccessToken != "新しいアクセストークン" {
		t.Errorf("トークンが一致しません: %+v", token)
	}
	if !strings.Contains(stderr.String(), "https://accounts.example.test/public_api/authorize") {
		t.Errorf("認可URLを標準エラー出力に表示する必要があります: %s", stderr.String())
	}
}

// TestLogin_ManualEmptyCode は、手動方式で認可コードが空のときにトークンを要求せず失敗することを検証します。
func TestLogin_ManualEmptyCode(t *testing.T) {
	var received []tokenRequest
	tokenServer := newTokenServer(t, &received)

	_, err := Login(context.Background(), LoginOptions{
		ClientID: "テスト用クライアントID", ClientSecret: "テスト用シークレット", Manual: true,
		AuthURL: "https://accounts.example.test/public_api/authorize", TokenURL: tokenServer.URL,
		OpenBrowser: func(string) error { return nil }, Stdin: strings.NewReader("\n"), Stderr: &bytes.Buffer{},
		Timeout: 5 * time.Second,
	})
	if err == nil {
		t.Fatal("認可コードが空なのにログインが成功しました")
	}
	if len(received) != 0 {
		t.Errorf("認可コードが空の場合はトークンを要求してはいけません（呼び出し回数 = %d）", len(received))
	}
}

// TestLogin_MissingClientCredentials は、クライアントIDまたはシークレットが無い場合に失敗することを検証します。
func TestLogin_MissingClientCredentials(t *testing.T) {
	_, err := Login(context.Background(), LoginOptions{ClientID: "テスト用クライアントID", Stderr: &bytes.Buffer{}})
	if err == nil {
		t.Fatal("クライアントシークレットが無いのにログインが成功しました")
	}
}
