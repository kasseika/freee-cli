// app.go は freee-hr CLI のエントリポイントとなる App 構造体を定義します。
//
// App は標準入出力・認証情報ファイルの場所・freee の各エンドポイント・ブラウザの起動処理を
// 差し替えられるようにし、実際の通信やホームディレクトリを使わずにコマンドを検証できるようにしています。
package cli

import (
	"context"
	"io"
	"os"

	"github.com/pkg/browser"
	"github.com/spf13/cobra"
	"golang.org/x/oauth2"

	"github.com/kasseika/freee-cli/internal/auth"
	"github.com/kasseika/freee-cli/internal/freeehr"
)

// App は freee-hr CLI の実行に必要な依存をまとめた構造体です。
type App struct {
	// Stdout / Stderr はコマンドの標準出力・標準エラー出力です。
	Stdout, Stderr io.Writer
	// Stdin はコマンドの標準入力です（`auth login` の対話入力に使います）。
	Stdin io.Reader
	// CredentialsPath は認証情報ファイルのパスです。空文字列の場合は auth.DefaultCredentialsPath() を使います。
	CredentialsPath string
	// APIBaseURL は freee人事労務 API のベースURLです。空文字列の場合は freeehr.DefaultBaseURL を使います。
	APIBaseURL string
	// AuthURL / TokenURL は OAuth 2.0 のエンドポイントです。空文字列の場合は freee の実際のURLを使います。
	AuthURL, TokenURL string
	// OpenBrowser は認可URLをブラウザで開く処理です。
	OpenBrowser func(url string) error
}

// NewApp は標準入出力と実際の freee のエンドポイントを使う既定の App を生成します。
func NewApp() *App {
	return &App{
		Stdout:      os.Stdout,
		Stderr:      os.Stderr,
		Stdin:       os.Stdin,
		OpenBrowser: browser.OpenURL,
	}
}

// credentialsStore は CredentialsPath（未設定なら既定のパス）を使う Store を返します。
func (a *App) credentialsStore() (*auth.Store, error) {
	path := a.CredentialsPath
	if path == "" {
		var err error
		path, err = auth.DefaultCredentialsPath()
		if err != nil {
			return nil, err
		}
	}
	return auth.NewStore(path), nil
}

// tokenURL は使用するトークンエンドポイントのURLを返します。
func (a *App) tokenURL() string {
	if a.TokenURL != "" {
		return a.TokenURL
	}
	return auth.TokenURL
}

// newAPIClient は tokens を使う freee人事労務 API のクライアントを生成します。
func (a *App) newAPIClient(tokens freeehr.TokenSource) *freeehr.Client {
	opts := []freeehr.Option{}
	if a.APIBaseURL != "" {
		opts = append(opts, freeehr.WithBaseURL(a.APIBaseURL))
	}
	return freeehr.NewClient(tokens, opts...)
}

// newClient は保存済みの認証情報から、トークンの自動更新に対応したクライアントを生成します。
// 認証情報と一緒に返すのは、各コマンドが事業所ID・従業員IDを使うためです。
// プロファイルが無い場合は auth.ErrProfileNotFound（終了コード3）を返します。
func (a *App) newClient(profile string) (*freeehr.Client, *auth.Credentials, error) {
	store, err := a.credentialsStore()
	if err != nil {
		return nil, nil, err
	}
	creds, err := store.Load(profile)
	if err != nil {
		return nil, nil, err
	}
	config := &oauth2.Config{
		ClientID:     creds.ClientID,
		ClientSecret: creds.ClientSecret,
		Endpoint:     auth.Endpoint(auth.AuthURL, a.tokenURL()),
	}
	return a.newAPIClient(auth.NewTokenSource(store, profile, config)), creds, nil
}

// Execute はコマンドライン引数を解釈して実行し、終了コードを返します。
// エラーは「エラー: 」に続けて標準エラー出力へ書き込みます。
func (a *App) Execute(args []string) int {
	root := a.buildRootCommand()
	root.SetArgs(args)
	root.SetOut(a.Stdout)
	root.SetErr(a.Stderr)
	root.SetIn(a.Stdin)

	err := root.ExecuteContext(context.Background())
	if err != nil {
		_, _ = io.WriteString(a.Stderr, "エラー: "+err.Error()+"\n")
	}
	return exitCodeFor(err)
}

// globalFlags はすべてのサブコマンドで共有するグローバルフラグです。
type globalFlags struct {
	profile string
	timeout int // 秒単位
}

func (a *App) buildRootCommand() *cobra.Command {
	flags := &globalFlags{}

	root := &cobra.Command{
		Use:           "freee-hr",
		Short:         "freee人事労務 API を操作するCLI（出力は常にJSON）",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&flags.profile, "profile", "default", "使用するプロファイル名")
	root.PersistentFlags().IntVar(&flags.timeout, "timeout", defaultTimeoutSeconds, "APIリクエストのタイムアウト（秒）")

	root.AddCommand(a.newAuthCmd(flags))
	root.AddCommand(a.newUserCmd(flags))
	return root
}
