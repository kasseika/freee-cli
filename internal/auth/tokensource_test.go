// tokensource_test.go は、保存済みトークンの自動更新（リフレッシュ）を検証します。
//
// freee のリフレッシュトークンは1回しか使えないため、更新後の新しいリフレッシュトークンを
// 必ず保存すること、複数のプロセスが同時に更新しても更新が1回で済むことを確かめます。
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// newRefreshServer は、リフレッシュ要求を数えながら新しいトークンを返すモックのトークンエンドポイントを起動します。
func newRefreshServer(t *testing.T, calls *atomic.Int32, lastRefreshToken *atomic.Value) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_ = r.ParseForm()
		lastRefreshToken.Store(r.PostForm.Get("refresh_token"))
		// 同時実行の検証で競合が起きやすいよう、少し待ってから応答する
		time.Sleep(50 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "更新後のアクセストークン",
			"token_type":    "bearer",
			"expires_in":    21600,
			"refresh_token": "更新後のリフレッシュトークン",
			"company_id":    123,
		})
	}))
	t.Cleanup(server.Close)
	return server
}

// newTestConfig はモックのトークンエンドポイントを使う OAuth 2.0 の設定を返します。
func newTestConfig(tokenURL string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     "テスト用クライアントID",
		ClientSecret: "テスト用シークレット",
		Endpoint:     oauth2.Endpoint{AuthURL: AuthURL, TokenURL: tokenURL, AuthStyle: oauth2.AuthStyleInParams},
	}
}

// saveWithExpiry は、指定した有効期限のアクセストークンを持つ認証情報を保存します。
func saveWithExpiry(t *testing.T, store *Store, expiry time.Time) {
	t.Helper()
	creds := sampleCredentials()
	creds.AccessToken = "保存済みのアクセストークン"
	creds.RefreshToken = "保存済みのリフレッシュトークン"
	creds.Expiry = expiry
	if err := store.Save("default", creds); err != nil {
		t.Fatal(err)
	}
}

// TestTokenSource_ValidTokenIsReused は、有効期限内のアクセストークンはリフレッシュせずに使うことを検証します。
func TestTokenSource_ValidTokenIsReused(t *testing.T) {
	var calls atomic.Int32
	var last atomic.Value
	server := newRefreshServer(t, &calls, &last)
	store := NewStore(filepath.Join(t.TempDir(), "credentials.json"))
	saveWithExpiry(t, store, time.Now().Add(time.Hour))

	token, err := NewTokenSource(store, "default", newTestConfig(server.URL)).Token(context.Background())
	if err != nil {
		t.Fatalf("トークンの取得に失敗しました: %v", err)
	}
	if token != "保存済みのアクセストークン" {
		t.Errorf("トークン = %q, 期待値 = 保存済みのアクセストークン", token)
	}
	if calls.Load() != 0 {
		t.Errorf("有効期限内なのにリフレッシュしました（呼び出し回数 = %d）", calls.Load())
	}
}

// TestTokenSource_ExpiredTokenIsRefreshedAndSaved は、期限切れのときにリフレッシュし、
// 新しいアクセストークンとリフレッシュトークンを保存することを検証します。
func TestTokenSource_ExpiredTokenIsRefreshedAndSaved(t *testing.T) {
	var calls atomic.Int32
	var last atomic.Value
	server := newRefreshServer(t, &calls, &last)
	store := NewStore(filepath.Join(t.TempDir(), "credentials.json"))
	saveWithExpiry(t, store, time.Now().Add(-time.Minute))

	token, err := NewTokenSource(store, "default", newTestConfig(server.URL)).Token(context.Background())
	if err != nil {
		t.Fatalf("トークンの取得に失敗しました: %v", err)
	}
	if token != "更新後のアクセストークン" {
		t.Errorf("トークン = %q, 期待値 = 更新後のアクセストークン", token)
	}
	if got := last.Load(); got != "保存済みのリフレッシュトークン" {
		t.Errorf("リフレッシュに使ったトークン = %v, 期待値 = 保存済みのリフレッシュトークン", got)
	}

	// 検証: 1回しか使えないリフレッシュトークンを、更新後の値で保存し直している
	saved, err := store.Load("default")
	if err != nil {
		t.Fatal(err)
	}
	if saved.RefreshToken != "更新後のリフレッシュトークン" || saved.AccessToken != "更新後のアクセストークン" {
		t.Errorf("更新後のトークンが保存されていません: %+v", saved)
	}
	if saved.CompanyID != 123 || saved.CompanyName != "テスト事業所" {
		t.Errorf("トークン以外の情報が失われました: %+v", saved)
	}
}

// TestTokenSource_ConcurrentRefreshHappensOnce は、別々のプロセスを想定した2つの TokenSource が
// 同時に期限切れのトークンを使おうとしても、リフレッシュは1回だけ行われることを検証します。
func TestTokenSource_ConcurrentRefreshHappensOnce(t *testing.T) {
	var calls atomic.Int32
	var last atomic.Value
	server := newRefreshServer(t, &calls, &last)
	path := filepath.Join(t.TempDir(), "credentials.json")
	saveWithExpiry(t, NewStore(path), time.Now().Add(-time.Minute))

	// 前提: 同じ認証情報ファイルを、独立した Store と TokenSource から使う
	sources := []*TokenSource{
		NewTokenSource(NewStore(path), "default", newTestConfig(server.URL)),
		NewTokenSource(NewStore(path), "default", newTestConfig(server.URL)),
	}
	var wg sync.WaitGroup
	results := make([]string, len(sources))
	errs := make([]error, len(sources))
	for i, source := range sources {
		wg.Add(1)
		go func(i int, source *TokenSource) {
			defer wg.Done()
			results[i], errs[i] = source.Token(context.Background())
		}(i, source)
	}
	wg.Wait()

	for i := range sources {
		if errs[i] != nil {
			t.Fatalf("%d番目の取得に失敗しました: %v", i, errs[i])
		}
		if results[i] != "更新後のアクセストークン" {
			t.Errorf("%d番目のトークン = %q, 期待値 = 更新後のアクセストークン", i, results[i])
		}
	}
	if calls.Load() != 1 {
		t.Errorf("リフレッシュの回数 = %d, 期待値 = 1（同じリフレッシュトークンを2回使うと失効するため）", calls.Load())
	}
}

// TestTokenSource_RefreshFailure は、リフレッシュに失敗したとき ErrTokenRefresh を返すことを検証します。
func TestTokenSource_RefreshFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"リフレッシュトークンが無効です"}`))
	}))
	t.Cleanup(server.Close)
	store := NewStore(filepath.Join(t.TempDir(), "credentials.json"))
	saveWithExpiry(t, store, time.Now().Add(-time.Minute))

	_, err := NewTokenSource(store, "default", newTestConfig(server.URL)).Token(context.Background())
	if !errors.Is(err, ErrTokenRefresh) {
		t.Fatalf("エラー = %v, 期待値 = ErrTokenRefresh", err)
	}
}

// TestTokenSource_ProfileNotFound は、未保存のプロファイルで ErrProfileNotFound を返すことを検証します。
func TestTokenSource_ProfileNotFound(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "credentials.json"))
	_, err := NewTokenSource(store, "default", newTestConfig("http://127.0.0.1:1/token")).Token(context.Background())
	if !errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("エラー = %v, 期待値 = ErrProfileNotFound", err)
	}
}
