// auth.go は `freee-hr auth` コマンド群（login / status / logout）を実装します。
//
// `auth login` は OAuth 2.0 の認可コードフローでトークンを取得したあと、ログインユーザーの情報
// （GET /api/v1/users/me）から、選んだ事業所での従業員IDを調べてプロファイルに保存します。
// 以降のコマンドは、この事業所ID・従業員IDを既定の対象として使います。
package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/kasseika/freee-cli/internal/auth"
	"github.com/kasseika/freee-cli/internal/freeehr"
)

const (
	// envClientID / envClientSecret はクライアント情報を渡す環境変数の名前です。
	envClientID     = "FREEE_HR_CLIENT_ID"
	envClientSecret = "FREEE_HR_CLIENT_SECRET"
)

func (a *App) newAuthCmd(flags *globalFlags) *cobra.Command {
	cmd := newGroupCmd("auth", "認証の管理")
	cmd.AddCommand(a.newAuthLoginCmd(flags))
	cmd.AddCommand(a.newAuthStatusCmd(flags))
	cmd.AddCommand(a.newAuthLogoutCmd(flags))
	return cmd
}

func (a *App) newAuthLoginCmd(flags *globalFlags) *cobra.Command {
	var clientID, clientSecret string
	var port int
	var manual, readOnly bool

	cmd := &cobra.Command{
		Use:   "login",
		Short: "freee にログインし、事業所を選んでプロファイルに保存します",
		Long: "OAuth 2.0 の認可コードフローでログインします。ブラウザで事業所を選んで「許可する」を押してください。\n" +
			"既定では http://localhost:<port>/callback で認可コードを受け取ります（freee のアプリにこのコールバックURLを登録してください）。\n" +
			"--manual を付けると、ブラウザに表示された認可コードを貼り付ける方式（urn:ietf:wg:oauth:2.0:oob）になります。",
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) (err error) {
			if clientID == "" {
				clientID = os.Getenv(envClientID)
			}
			if clientSecret == "" {
				clientSecret = os.Getenv(envClientSecret)
			}
			// 対話入力と、手動方式の認可コードの読み込みで同じリーダーを使う。
			// 別々にバッファリングすると、先に読んだ側がまとめて読み進め、認可コードの行を失うためである。
			stdin := bufio.NewReader(a.Stdin)
			if clientID == "" || clientSecret == "" {
				clientID, clientSecret, err = a.promptClientCredentials(stdin, clientID, clientSecret)
				if err != nil {
					return err
				}
			}
			if clientID == "" || clientSecret == "" {
				return usageErrorf("クライアントIDとクライアントシークレットが必要です。"+
					"--client-id / --client-secret、または環境変数 %s / %s で指定してください", envClientID, envClientSecret)
			}

			token, err := auth.Login(cmd.Context(), auth.LoginOptions{
				ClientID:     clientID,
				ClientSecret: clientSecret,
				Port:         port,
				Manual:       manual,
				AuthURL:      a.AuthURL,
				TokenURL:     a.TokenURL,
				OpenBrowser:  a.OpenBrowser,
				Stdin:        stdin,
				Stderr:       a.Stderr,
			})
			if err != nil {
				return err
			}
			companyID, err := auth.CompanyIDFromToken(token)
			if err != nil {
				return err
			}

			// 選んだ事業所での従業員IDを調べる。従業員IDは以降のほぼすべてのコマンドで必要なため、
			// 取得に失敗した場合はログインを失敗とし、不完全なプロファイルを保存しない。
			ctx, cancel := timeoutContext(cmd.Context(), flags)
			defer cancel()
			company, err := a.lookupCompany(ctx, token.AccessToken, companyID)
			if err != nil {
				return err
			}

			creds := &auth.Credentials{
				ClientID:     clientID,
				ClientSecret: clientSecret,
				AccessToken:  token.AccessToken,
				RefreshToken: token.RefreshToken,
				Expiry:       token.Expiry,
				CompanyID:    companyID,
				CompanyName:  company.Name,
				EmployeeID:   company.EmployeeID,
				DisplayName:  company.DisplayName,
				ReadOnly:     readOnly,
			}
			if creds.EmployeeID == nil {
				_, _ = fmt.Fprintf(a.Stderr, "警告: 事業所「%s」に従業員として登録されていません。打刻・勤怠のコマンドでは --employee-id の指定が必要です。\n", company.Name)
			}

			store, err := a.credentialsStore()
			if err != nil {
				return err
			}
			if err := store.Save(ctx, flags.profile, creds); err != nil {
				return err
			}
			return writeJSON(a.Stdout, newAuthStatus(flags.profile, creds))
		},
	}

	cmd.Flags().StringVar(&clientID, "client-id", "", "freee アプリのClient ID（環境変数 "+envClientID+" でも指定可）")
	cmd.Flags().StringVar(&clientSecret, "client-secret", "", "freee アプリのClient Secret（環境変数 "+envClientSecret+" でも指定可）")
	cmd.Flags().IntVar(&port, "port", auth.DefaultLoginPort, "認可コードを受け取るローカルサーバーのポート")
	cmd.Flags().BoolVar(&manual, "manual", false, "ブラウザに表示された認可コードを貼り付ける方式でログインする")
	cmd.Flags().BoolVar(&readOnly, "read-only", false, "読み取り専用のプロファイルにする（打刻・勤怠の修正・申請の作成を拒否する）")
	return cmd
}

// lookupCompany は、ログイン直後のアクセストークンで users/me を呼び、選んだ事業所の情報を返します。
func (a *App) lookupCompany(ctx context.Context, accessToken string, companyID int) (*freeehr.MeCompany, error) {
	raw, err := a.newAPIClient(fixedTokenSource(accessToken)).GetMe(ctx)
	if err != nil {
		return nil, fmt.Errorf("ログインユーザー情報の取得に失敗しました: %w", err)
	}
	me, err := freeehr.ParseMe(raw)
	if err != nil {
		return nil, err
	}
	company, ok := me.Company(companyID)
	if !ok {
		return nil, fmt.Errorf("選択した事業所（ID: %d）がログインユーザーの所属事業所に見つかりません", companyID)
	}
	return &company, nil
}

// promptClientCredentials は、標準入力からクライアントID・シークレットを対話的に読み込みます。
// 標準入力が端末の場合、Client Secret は画面に表示せずに読み込みます。
// 標準入力が無い場合（Claude Code の Bash など）は空文字列のまま返り、呼び出し元が使い方の誤りとして扱います。
func (a *App) promptClientCredentials(stdin *bufio.Reader, clientID, clientSecret string) (string, string, error) {
	if clientID == "" {
		_, _ = fmt.Fprintln(a.Stderr, "freee アプリのクライアント情報が設定されていません。")
		_, _ = fmt.Fprint(a.Stderr, "Client ID: ")
		line, err := readPromptLine(stdin)
		if err != nil {
			return "", "", err
		}
		clientID = line
	}
	if clientSecret == "" {
		_, _ = fmt.Fprint(a.Stderr, "Client Secret: ")
		if fd, ok := a.terminalFD(); ok {
			secret, err := term.ReadPassword(fd)
			_, _ = fmt.Fprintln(a.Stderr)
			if err != nil {
				return "", "", fmt.Errorf("クライアントシークレットの読み込みに失敗しました: %w", err)
			}
			clientSecret = strings.TrimSpace(string(secret))
		} else {
			line, err := readPromptLine(stdin)
			if err != nil {
				return "", "", err
			}
			clientSecret = line
		}
	}
	return clientID, clientSecret, nil
}

// readPromptLine は1行を読み、前後の空白を除いて返します。入力の終わり（EOF）は空の入力として扱います。
func readPromptLine(r *bufio.Reader) (string, error) {
	line, err := r.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("標準入力の読み込みに失敗しました: %w", err)
	}
	return strings.TrimSpace(line), nil
}

// terminalFD は、標準入力が端末であればそのファイルディスクリプタを返します。
func (a *App) terminalFD() (int, bool) {
	f, ok := a.Stdin.(*os.File)
	if !ok {
		return 0, false
	}
	fd := int(f.Fd())
	return fd, term.IsTerminal(fd)
}

// authStatus は `auth status` と `auth login` が出力するプロファイルの状態です。
// トークンやクライアントシークレットなどの秘密情報は含めません。
type authStatus struct {
	Profile     string `json:"profile"`
	CompanyID   int    `json:"company_id"`
	CompanyName string `json:"company_name"`
	EmployeeID  *int   `json:"employee_id"`
	DisplayName string `json:"display_name"`
	ReadOnly    bool   `json:"read_only"`
	// AccessTokenExpiry はアクセストークンの有効期限です。期限が切れても、次のコマンド実行時に自動で更新されます。
	AccessTokenExpiry string `json:"access_token_expiry"`
}

func newAuthStatus(profile string, creds *auth.Credentials) authStatus {
	return authStatus{
		Profile:           profile,
		CompanyID:         creds.CompanyID,
		CompanyName:       creds.CompanyName,
		EmployeeID:        creds.EmployeeID,
		DisplayName:       creds.DisplayName,
		ReadOnly:          creds.ReadOnly,
		AccessTokenExpiry: creds.Expiry.Format(time.RFC3339),
	}
}

func (a *App) newAuthStatusCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "ログイン状態（事業所・従業員・読み取り専用か）を表示します",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := a.credentialsStore()
			if err != nil {
				return err
			}
			creds, err := store.Load(flags.profile)
			if err != nil {
				return err
			}
			return writeJSON(a.Stdout, newAuthStatus(flags.profile, creds))
		},
	}
}

func (a *App) newAuthLogoutCmd(flags *globalFlags) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "ログアウトします（このプロファイルの保存済みの認証情報を削除します）",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := a.credentialsStore()
			if err != nil {
				return err
			}
			if err := store.Delete(cmd.Context(), flags.profile); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(a.Stderr, "ログアウトしました（プロファイル: %s）\n", flags.profile)
			return nil
		},
	}
}

// fixedTokenSource は、ログイン直後でまだ認証情報を保存していない時点の API 呼び出しに使う、固定トークンの TokenSource です。
type fixedTokenSource string

func (t fixedTokenSource) Token(_ context.Context) (string, error) {
	if t == "" {
		return "", errors.New("アクセストークンが空です")
	}
	return string(t), nil
}
