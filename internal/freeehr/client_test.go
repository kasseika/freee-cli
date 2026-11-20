// client_test.go は freee人事労務 API クライアントの共通の振る舞い（認証ヘッダー・エラー変換）を検証します。
package freeehr

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fixedTokens はテスト用の固定トークンを返す TokenSource です。
type fixedTokens string

func (f fixedTokens) Token(context.Context) (string, error) { return string(f), nil }

// failingTokens は常にエラーを返す TokenSource です。
type failingTokens struct{ err error }

func (f failingTokens) Token(context.Context) (string, error) { return "", f.err }

// newTestClient は handler を使うモックの freee API サーバーに接続するクライアントを生成します。
func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return NewClient(fixedTokens("テスト用トークン"), WithBaseURL(server.URL))
}

// TestGetMe は、ログインユーザーの取得APIを Bearer トークン付きで呼び、レスポンスをそのまま返すことを検証します。
func TestGetMe(t *testing.T) {
	const body = `{"id":1,"companies":[{"id":123,"name":"テスト事業所","role":"self_only","employee_id":456,"display_name":"地活 太郎"}]}`
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/users/me" {
			t.Errorf("リクエスト = %s %s, 期待値 = GET /api/v1/users/me", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer テスト用トークン" {
			t.Errorf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	})

	raw, err := client.GetMe(context.Background())
	if err != nil {
		t.Fatalf("取得に失敗しました: %v", err)
	}
	if string(raw) != body {
		t.Errorf("レスポンス = %s, 期待値 = %s", raw, body)
	}

	me, err := ParseMe(raw)
	if err != nil {
		t.Fatalf("解析に失敗しました: %v", err)
	}
	company, ok := me.Company(123)
	if !ok || company.Name != "テスト事業所" || company.EmployeeID == nil || *company.EmployeeID != 456 || company.DisplayName != "地活 太郎" {
		t.Errorf("事業所の情報が一致しません: %+v", company)
	}
	if _, ok := me.Company(999); ok {
		t.Error("所属していない事業所が見つかってしまいます")
	}
}

// TestDo_SendsJSONBody は、書き込み系のリクエストで本文をJSONとして送ることを検証します。
func TestDo_SendsJSONBody(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/sample" || r.URL.Query().Get("company_id") != "123" {
			t.Errorf("リクエスト = %s %s", r.Method, r.URL)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q", ct)
		}
		var got map[string]any
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil || got["note"] != "テストの備考" {
			t.Errorf("本文 = %v (err=%v)", got, err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	raw, err := client.Do(context.Background(), http.MethodPost, "/api/v1/sample", map[string][]string{"company_id": {"123"}}, map[string]string{"note": "テストの備考"})
	if err != nil {
		t.Fatalf("リクエストに失敗しました: %v", err)
	}
	if string(raw) != `{"ok":true}` {
		t.Errorf("レスポンス = %s", raw)
	}
}

// TestAPIError_FreeeErrorBody は、freee の共通エラーレスポンス（errors[].messages）を APIError に変換することを検証します。
func TestAPIError_FreeeErrorBody(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"status_code":400,"errors":[{"type":"bad_request","messages":["リクエストの形式が不正です。","日付が不正です。"]}]}`))
	})

	_, err := client.GetMe(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("エラー = %v, 期待値 = *APIError", err)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("StatusCode = %d", apiErr.StatusCode)
	}
	if len(apiErr.Messages) != 2 || apiErr.Messages[0] != "リクエストの形式が不正です。" || apiErr.Messages[1] != "日付が不正です。" {
		t.Errorf("Messages = %v", apiErr.Messages)
	}
	if !strings.Contains(err.Error(), "日付が不正です。") {
		t.Errorf("エラーメッセージにAPIの説明が含まれていません: %v", err)
	}
}

// TestAPIError_MessageBody は、message フィールドだけのエラーレスポンスも変換できることを検証します。
func TestAPIError_MessageBody(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"アクセス権限がありません"}`))
	})

	_, err := client.GetMe(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || len(apiErr.Messages) != 1 || apiErr.Messages[0] != "アクセス権限がありません" {
		t.Fatalf("エラー = %#v", err)
	}
}

// TestAPIError_NonJSONBody は、JSONでないエラーレスポンスの本文もメッセージとして残すことを検証します。
func TestAPIError_NonJSONBody(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("メンテナンス中です"))
	})

	_, err := client.GetMe(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || len(apiErr.Messages) != 1 || apiErr.Messages[0] != "メンテナンス中です" {
		t.Fatalf("エラー = %#v", err)
	}
}

// TestAPIError_RateLimit は、429 のときに X-RateLimit-Reset の時刻をエラーに含めることを検証します。
func TestAPIError_RateLimit(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Reset", "2026-11-20T12:00:00.000000Z")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"status_code":429,"errors":[{"type":"too_many_requests","messages":["リクエスト回数の上限を超えました。"]}]}`))
	})

	_, err := client.GetMe(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.RateLimitReset != "2026-11-20T12:00:00.000000Z" {
		t.Fatalf("エラー = %#v", err)
	}
	if !strings.Contains(err.Error(), "2026-11-20T12:00:00.000000Z") {
		t.Errorf("エラーメッセージに制限が解除される時刻が含まれていません: %v", err)
	}
}

// TestDo_TokenError は、トークンの取得に失敗した場合にリクエストを送らず、元のエラーを辿れることを検証します。
func TestDo_TokenError(t *testing.T) {
	sentinel := errors.New("再ログインが必要です")
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	t.Cleanup(server.Close)
	client := NewClient(failingTokens{err: sentinel}, WithBaseURL(server.URL))

	_, err := client.GetMe(context.Background())
	if !errors.Is(err, sentinel) {
		t.Fatalf("エラー = %v, 期待値 = 元のエラーを含む", err)
	}
	if called {
		t.Error("トークンが無いのにリクエストを送りました")
	}
}
