// login.go は freee の OAuth 2.0 認可コードフロー（Authorization Code Grant）を実装します。
//
// 認可コードの受け取り方は2通りあり、利用者がフラグで明示的に選びます（自動では切り替えません）。
//
//   - ループバック方式（既定）: 127.0.0.1 で一時的にHTTPサーバーを起動し、
//     `http://localhost:<port>/callback` へのリダイレクトで認可コードを受け取ります。
//     freee のアプリ管理画面で、このURLをコールバックURLとして登録しておく必要があります。
//   - 手動方式（Manual）: コールバックURLに `urn:ietf:wg:oauth:2.0:oob` を使い、
//     ブラウザに表示された認可コードを利用者が標準入力に貼り付けます。
//
// どちらの方式でも `prompt=select_company` を付け、トークンを1つの事業所に限定します。
// freee のドキュメントに PKCE の記載が無いため、CSRF 対策は state の照合で行います
// （手動方式ではリダイレクトを受けないため state を照合できません）。
package auth

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
)

const (
	// AuthURL は freee の OAuth 2.0 認可エンドポイントです。
	AuthURL = "https://accounts.secure.freee.co.jp/public_api/authorize"
	// TokenURL は freee の OAuth 2.0 トークンエンドポイントです。
	TokenURL = "https://accounts.secure.freee.co.jp/public_api/token"
	// ManualRedirectURL は手動方式で使うコールバックURLです（認可コードがブラウザに表示されます）。
	ManualRedirectURL = "urn:ietf:wg:oauth:2.0:oob"

	// DefaultLoginPort はループバック方式で待ち受ける既定のポート番号です。
	DefaultLoginPort = 8765

	defaultLoginTimeout = 5 * time.Minute
	stateBytes          = 16
)

// Endpoint は freee のトークン取得に使う OAuth 2.0 のエンドポイント設定を返します。
// freee のドキュメントに合わせ、クライアントID・シークレットはフォームの本文で送ります。
func Endpoint(authURL, tokenURL string) oauth2.Endpoint {
	return oauth2.Endpoint{AuthURL: authURL, TokenURL: tokenURL, AuthStyle: oauth2.AuthStyleInParams}
}

// LoginOptions は Login の実行パラメータです。
type LoginOptions struct {
	// ClientID / ClientSecret は freee のアプリ管理画面で発行された値です（必須）。
	ClientID     string
	ClientSecret string
	// Port はループバック方式で待ち受けるポート番号です。0 を指定すると空きポートを使います（テスト用）。
	Port int
	// Manual が true の場合は手動方式（認可コードの貼り付け）でログインします。
	Manual bool
	// AuthURL / TokenURL はテストで差し替えるためのエンドポイントです。空文字列の場合は freee の実際のURLを使います。
	AuthURL  string
	TokenURL string
	// OpenBrowser は認可URLをブラウザで開く処理です。nil の場合はブラウザを開きません。
	OpenBrowser func(authURL string) error
	// Stdin は手動方式で認可コードを読み込む入力です。
	Stdin io.Reader
	// Stderr は認可URLや案内を表示する出力です（必須）。
	Stderr io.Writer
	// Timeout は利用者の操作を待つ最大時間です。0 の場合は5分です。
	Timeout time.Duration
}

// Login は認可コードフローを実行し、取得したトークンを返します。
func Login(ctx context.Context, opts LoginOptions) (*oauth2.Token, error) {
	if opts.ClientID == "" || opts.ClientSecret == "" {
		return nil, errors.New("クライアントIDとクライアントシークレットが必要です。" +
			"freee のアプリ管理画面（https://app.secure.freee.co.jp/developers/applications）でアプリを登録してください")
	}
	authURL := opts.AuthURL
	if authURL == "" {
		authURL = AuthURL
	}
	tokenURL := opts.TokenURL
	if tokenURL == "" {
		tokenURL = TokenURL
	}
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = defaultLoginTimeout
	}
	config := &oauth2.Config{
		ClientID:     opts.ClientID,
		ClientSecret: opts.ClientSecret,
		Endpoint:     Endpoint(authURL, tokenURL),
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if opts.Manual {
		return loginManual(timeoutCtx, config, opts)
	}
	return loginLoopback(timeoutCtx, config, opts)
}

// showAuthURL は認可URLを標準エラー出力に表示し、可能であればブラウザで開きます。
// ブラウザを自動で開けない環境でも認証できるよう、URLは必ず表示します。
func showAuthURL(opts LoginOptions, authCodeURL string) {
	_, _ = fmt.Fprintf(opts.Stderr, "ブラウザで以下のURLを開き、事業所を選んで「許可する」を押してください:\n%s\n", authCodeURL)
	if opts.OpenBrowser != nil {
		_ = opts.OpenBrowser(authCodeURL)
	}
}

// loginManual は手動方式で、標準入力に貼り付けられた認可コードからトークンを取得します。
func loginManual(ctx context.Context, config *oauth2.Config, opts LoginOptions) (*oauth2.Token, error) {
	config.RedirectURL = ManualRedirectURL
	state, err := randomState()
	if err != nil {
		return nil, fmt.Errorf("stateの生成に失敗しました: %w", err)
	}
	showAuthURL(opts, config.AuthCodeURL(state, oauth2.SetAuthURLParam("prompt", "select_company")))
	_, _ = fmt.Fprint(opts.Stderr, "表示された認可コードを貼り付けて Enter を押してください: ")

	line, err := bufio.NewReader(opts.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("認可コードの読み込みに失敗しました: %w", err)
	}
	code := strings.TrimSpace(line)
	if code == "" {
		return nil, errors.New("認可コードが入力されませんでした")
	}
	token, err := config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("トークンの取得に失敗しました: %w", err)
	}
	return token, nil
}

// loginCallbackResult はコールバックの処理結果を Login 本体へ伝える内部の型です。
type loginCallbackResult struct {
	code string
	err  error
}

// loginLoopback はループバック方式で、コールバックで受け取った認可コードからトークンを取得します。
func loginLoopback(ctx context.Context, config *oauth2.Config, opts LoginOptions) (*oauth2.Token, error) {
	state, err := randomState()
	if err != nil {
		return nil, fmt.Errorf("stateの生成に失敗しました: %w", err)
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", opts.Port))
	if err != nil {
		return nil, fmt.Errorf("ローカルサーバーの起動に失敗しました（ポート%dが使用中の可能性があります）: %w", opts.Port, err)
	}
	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		_ = listener.Close()
		return nil, fmt.Errorf("ローカルサーバーのアドレスを取得できませんでした: %s", listener.Addr())
	}
	config.RedirectURL = fmt.Sprintf("http://localhost:%d/callback", tcpAddr.Port)

	// コールバックは1回だけ処理する（2回目以降のアクセスで結果を上書きしない）
	resultCh := make(chan loginCallbackResult, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		result := handleCallback(w, r, state)
		select {
		case resultCh <- result:
		default:
		}
	})
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(listener) }()
	defer func() { _ = server.Close() }()

	showAuthURL(opts, config.AuthCodeURL(state, oauth2.SetAuthURLParam("prompt", "select_company")))

	select {
	case res := <-resultCh:
		if res.err != nil {
			return nil, res.err
		}
		token, err := config.Exchange(ctx, res.code)
		if err != nil {
			return nil, fmt.Errorf("トークンの取得に失敗しました: %w", err)
		}
		return token, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("認証がタイムアウトしました。もう一度 `freee-hr auth login` を実行してください: %w", ctx.Err())
	}
}

// handleCallback はコールバックのクエリを検証し、ブラウザに結果を表示します。
func handleCallback(w http.ResponseWriter, r *http.Request, expectedState string) loginCallbackResult {
	q := r.URL.Query()
	if errParam := q.Get("error"); errParam != "" {
		http.Error(w, "認可が拒否されました: "+errParam, http.StatusBadRequest)
		return loginCallbackResult{err: fmt.Errorf("認可が拒否されました: %s", errParam)}
	}
	if q.Get("state") != expectedState {
		http.Error(w, "stateが一致しません。認証を中断しました。", http.StatusBadRequest)
		return loginCallbackResult{err: errors.New("コールバックのstateが一致しません（CSRF対策のため中断しました）")}
	}
	code := q.Get("code")
	if code == "" {
		http.Error(w, "認可コードが含まれていません。", http.StatusBadRequest)
		return loginCallbackResult{err: errors.New("コールバックに認可コードが含まれていません")}
	}
	_, _ = fmt.Fprintln(w, "認証が完了しました。このタブを閉じて構いません。")
	return loginCallbackResult{code: code}
}

// randomState は CSRF 対策用のランダムな state 値を生成します。
func randomState() (string, error) {
	buf := make([]byte, stateBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// CompanyIDFromToken は、トークンレスポンスに含まれる company_id（ログイン時に選んだ事業所のID）を取り出します。
// freee のレスポンスでは数値または文字列で返るため、どちらにも対応します。含まれない場合はエラーにします。
func CompanyIDFromToken(token *oauth2.Token) (int, error) {
	switch v := token.Extra("company_id").(type) {
	case float64:
		return int(v), nil
	case json.Number:
		id, err := strconv.Atoi(v.String())
		if err != nil {
			return 0, fmt.Errorf("トークンレスポンスの company_id が数値ではありません: %q", v)
		}
		return id, nil
	case string:
		id, err := strconv.Atoi(v)
		if err != nil {
			return 0, fmt.Errorf("トークンレスポンスの company_id が数値ではありません: %q", v)
		}
		return id, nil
	default:
		return 0, errors.New("トークンレスポンスに company_id が含まれていません（認可URLに prompt=select_company が必要です）")
	}
}
