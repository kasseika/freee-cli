// client.go は freee人事労務 API に HTTP リクエストを送る薄いクライアントです。
//
// CLI 固有の関心事（フラグの解析・出力）は持たず、API との通信とエラーの変換だけを担います。
// レスポンスは json.RawMessage のまま返します。CLI は API のレスポンスを加工せずに出力し、
// 必要な部分は利用者（Claude Code など）が jq で取り出す方針のためです。
//
// 429（リクエスト回数の上限）を含め、失敗したリクエストは自動で再試行しません。
// 打刻などの書き込みを二重に登録しないためです。
package freeehr

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// DefaultBaseURL は freee人事労務 API の既定のベースURLです。
const DefaultBaseURL = "https://api.freee.co.jp/hr"

// TokenSource は HTTP リクエストに付けるアクセストークンを提供します。
// 実装（internal/auth）は、必要に応じてトークンを更新してから返します。
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

// Client は freee人事労務 API の HTTP クライアントです。
type Client struct {
	baseURL    string
	httpClient *http.Client
	tokens     TokenSource
}

// Option は Client の生成時のオプションです。
type Option func(*Client)

// WithBaseURL はベースURLを変更します。主にテストで httptest のサーバーを指定するために使います。
func WithBaseURL(baseURL string) Option {
	return func(c *Client) { c.baseURL = baseURL }
}

// WithHTTPClient は内部で使う *http.Client を差し替えます。
func WithHTTPClient(httpClient *http.Client) Option {
	return func(c *Client) { c.httpClient = httpClient }
}

// NewClient は freee人事労務 API のクライアントを生成します。
func NewClient(tokens TokenSource, opts ...Option) *Client {
	c := &Client{baseURL: DefaultBaseURL, httpClient: http.DefaultClient, tokens: tokens}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Do は API を呼び出し、成功時のレスポンス本文を返します。
//
// reqBody が nil でない場合は JSON にエンコードして本文に設定します。
// ステータスコードが 400 以上の場合は *APIError を返します。
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, reqBody any) (json.RawMessage, error) {
	token, err := c.tokens.Token(ctx)
	if err != nil {
		return nil, fmt.Errorf("アクセストークンの取得に失敗しました: %w", err)
	}

	var bodyReader io.Reader
	if reqBody != nil {
		encoded, err := json.Marshal(reqBody)
		if err != nil {
			return nil, fmt.Errorf("リクエスト本文のエンコードに失敗しました: %w", err)
		}
		bodyReader = bytes.NewReader(encoded)
	}

	reqURL := c.baseURL + path
	if len(query) > 0 {
		reqURL += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("リクエストの組み立てに失敗しました: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("リクエストの送信に失敗しました: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("レスポンス本文の読み込みに失敗しました: %w", err)
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, newAPIError(resp, respBody)
	}
	return json.RawMessage(respBody), nil
}

// GetMe はログインユーザーと、所属する事業所の一覧を取得します（GET /api/v1/users/me）。
func (c *Client) GetMe(ctx context.Context) (json.RawMessage, error) {
	return c.Do(ctx, http.MethodGet, "/api/v1/users/me", nil, nil)
}
