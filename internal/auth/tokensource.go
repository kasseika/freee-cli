// tokensource.go は、保存済みのリフレッシュトークンで期限切れのアクセストークンを自動的に更新する
// TokenSource を提供します。
//
// freee のリフレッシュトークンは1回しか使えず、更新のたびに新しい値が発行されます。
// そのため、更新の「読み込み → 更新 → 保存」全体を Store.Lock で排他し、ロック取得後に
// 認証情報を読み直します。別のプロセスが先に更新していれば、その結果をそのまま使います。
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"golang.org/x/oauth2"
)

// ErrTokenRefresh は、トークンエンドポイントがリフレッシュを拒否した（リフレッシュトークンが失効している等）場合のエラーです。
// 再ログインが必要です。通信の失敗など、再ログインで解決しない失敗にはこのエラーを使いません。
var ErrTokenRefresh = errors.New("アクセストークンの更新が拒否されました。もう一度 `freee-hr auth login` を実行してください")

// TokenSource は Store と連動して有効なアクセストークンを返すトークンプロバイダです。
type TokenSource struct {
	store   *Store
	profile string
	config  *oauth2.Config
}

// NewTokenSource は TokenSource を生成します。config にはクライアントID・シークレットとエンドポイントを設定します。
func NewTokenSource(store *Store, profile string, config *oauth2.Config) *TokenSource {
	return &TokenSource{store: store, profile: profile, config: config}
}

// Token は有効なアクセストークンを返します。期限切れの場合は更新し、新しいトークンを保存します。
func (t *TokenSource) Token(ctx context.Context) (string, error) {
	unlock, err := t.store.Lock(ctx)
	if err != nil {
		return "", err
	}
	defer unlock()

	// ロック取得後に読み直すことで、別プロセスが更新した最新のトークンを使う
	creds, err := t.store.Load(t.profile)
	if err != nil {
		return "", err
	}
	current := &oauth2.Token{
		AccessToken:  creds.AccessToken,
		RefreshToken: creds.RefreshToken,
		Expiry:       creds.Expiry,
	}
	if current.Valid() {
		return current.AccessToken, nil
	}

	refreshed, err := t.config.TokenSource(ctx, current).Token()
	if err != nil {
		return "", classifyRefreshError(err)
	}
	creds.AccessToken = refreshed.AccessToken
	if refreshed.RefreshToken != "" {
		creds.RefreshToken = refreshed.RefreshToken
	}
	creds.Expiry = refreshed.Expiry
	if err := t.store.saveLocked(t.profile, creds); err != nil {
		// 古いリフレッシュトークンは更新に使った時点で失効しているため、保存に失敗すると再ログインが必要になる
		return "", fmt.Errorf("%w（更新後のトークンを保存できませんでした: %v）", ErrTokenRefresh, err)
	}
	return refreshed.AccessToken, nil
}

// classifyRefreshError は、リフレッシュの失敗を「再ログインが必要な失敗」とそれ以外に分けます。
// トークンエンドポイントが 4xx で拒否した場合だけ ErrTokenRefresh とし、通信の失敗や 5xx はそのまま返します。
func classifyRefreshError(err error) error {
	var retrieveErr *oauth2.RetrieveError
	if errors.As(err, &retrieveErr) && retrieveErr.Response != nil &&
		retrieveErr.Response.StatusCode >= http.StatusBadRequest && retrieveErr.Response.StatusCode < http.StatusInternalServerError {
		return fmt.Errorf("%w: %v", ErrTokenRefresh, err)
	}
	return fmt.Errorf("アクセストークンの更新に失敗しました（通信の失敗の可能性があります。時間をおいて再実行してください）: %w", err)
}
