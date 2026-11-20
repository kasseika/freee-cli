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
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/kasseika/freee-cli/internal/auth"
	"github.com/kasseika/freee-cli/internal/freeehr"
)

const (
	// envClientID / envClientSecret はクライアント情報を渡す環境変数の名前です。
	envClientID     = "FREEE_HR_CLIENT_ID"
	envClientSecret = "FREEE_HR_CLIENT_SECRET"
)

func (a *App) newAuthCmd(flags *globalFlags) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "認証の管理",
	}
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
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if clientID == "" {
				clientID = os.Getenv(envClientID)
			}
			if clientSecret == "" {
				clientSecret = os.Getenv(envClientSecret)
			}
			if clientID == "" || clientSecret == "" {
				clientID, clientSecret = a.promptClientCredentials(clientID, clientSecret)
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
				Stdin:        a.Stdin,
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
			if err := store.Save(flags.profile, creds); err != nil {
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
// 標準入力が無い場合（Claude Code の Bash など）は空文字列のまま返り、呼び出し元が使い方の誤りとして扱います。
func (a *App) promptClientCredentials(clientID, clientSecret string) (string, string) {
	reader := bufio.NewReader(a.Stdin)
	if clientID == "" {
		_, _ = fmt.Fprintln(a.Stderr, "freee アプリのクライアント情報が設定されていません。")
		_, _ = fmt.Fprint(a.Stderr, "Client ID: ")
		line, _ := reader.ReadString('\n')
		clientID = strings.TrimSpace(line)
	}
	if clientSecret == "" {
		_, _ = fmt.Fprint(a.Stderr, "Client Secret: ")
		line, _ := reader.ReadString('\n')
		clientSecret = strings.TrimSpace(line)
	}
	return clientID, clientSecret
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
		Args:  cobra.NoArgs,
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
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := a.credentialsStore()
			if err != nil {
				return err
			}
			if err := store.Delete(flags.profile); err != nil {
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
