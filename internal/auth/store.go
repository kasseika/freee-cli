// store.go は OAuth 2.0 の認証情報をローカルファイルに保存する Store を提供します。
//
// 事業所（本番の事業所・開発用テスト事業所など）を切り替えられるよう、プロファイル名を
// キーとした JSON ファイルとして保存します。ファイルにはアクセストークン・リフレッシュトークン・
// クライアントシークレットが含まれるため、パーミッションを 0600（ディレクトリは 0700）に制限します。
//
// freee のリフレッシュトークンは1回しか使えません。複数のプロセス（Claude Code が並列に実行する
// コマンドなど）が同時に更新すると片方の更新が失効するため、更新の前後は Lock で排他します。
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/gofrs/flock"
)

// Credentials は1プロファイル分の認証情報と、ログイン時に確定した事業所・従業員の情報です。
type Credentials struct {
	// ClientID / ClientSecret は freee のアプリ管理画面で発行された値です。
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	// AccessToken は現在のアクセストークンです（有効期限は6時間）。
	AccessToken string `json:"access_token"`
	// RefreshToken はアクセストークンの更新に使うトークンです。1回しか使えず、更新のたびに新しい値に置き換わります。
	RefreshToken string `json:"refresh_token"`
	// Expiry はアクセストークンの有効期限です。
	Expiry time.Time `json:"expiry"`
	// CompanyID はログイン時に選択した事業所のIDです（トークンレスポンスの company_id）。
	CompanyID int `json:"company_id"`
	// CompanyName は選択した事業所の名前です（`auth status` の表示用）。
	CompanyName string `json:"company_name"`
	// EmployeeID はその事業所でのログインユーザーの従業員IDです。
	// 従業員として登録されていないユーザー（管理者のみ等）の場合は nil です。
	EmployeeID *int `json:"employee_id"`
	// DisplayName はその事業所でのログインユーザーの表示名です。
	DisplayName string `json:"display_name,omitempty"`
	// ReadOnly が true のプロファイルでは、書き込み系のコマンド（打刻・勤怠の修正・申請の作成）を拒否します。
	ReadOnly bool `json:"read_only"`
}

// ErrProfileNotFound は指定したプロファイルの認証情報が保存されていない場合のエラーです。
var ErrProfileNotFound = errors.New("認証情報が見つかりません。`freee-hr auth login` を実行してください")

const (
	// lockRetryInterval はロック取得を再試行する間隔です。
	lockRetryInterval = 50 * time.Millisecond
	// lockTimeout はロック取得を諦めるまでの時間です。トークン更新の通信時間より十分に長くします。
	lockTimeout = 60 * time.Second
)

// Store はプロファイル名をキーとした認証情報をファイルに保存します。
type Store struct {
	path string
}

// NewStore は path のファイルを使う Store を生成します。
// ファイルが存在しない場合は、最初の Save で作成されます。
func NewStore(path string) *Store {
	return &Store{path: path}
}

// DefaultCredentialsPath は既定の認証情報ファイルのパス（$HOME/.config/freee-hr/credentials.json）を返します。
func DefaultCredentialsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("ホームディレクトリの取得に失敗しました: %w", err)
	}
	return filepath.Join(home, ".config", "freee-hr", "credentials.json"), nil
}

// profileMap はファイル上のデータ形式（プロファイル名 -> 認証情報）です。
type profileMap map[string]Credentials

func (s *Store) readAll() (profileMap, error) {
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return profileMap{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("認証情報ファイルの読み込みに失敗しました: %w", err)
	}
	all := profileMap{}
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, fmt.Errorf("認証情報ファイルの解析に失敗しました: %w", err)
	}
	return all, nil
}

// writeAll は全プロファイルを書き込みます。書き込み途中で中断しても元のファイルが壊れないよう、
// 同じディレクトリの一時ファイルに書いてから置き換えます。
func (s *Store) writeAll(all profileMap) error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("認証情報ディレクトリの作成に失敗しました: %w", err)
	}
	data, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return fmt.Errorf("認証情報のエンコードに失敗しました: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".credentials-*.json")
	if err != nil {
		return fmt.Errorf("一時ファイルの作成に失敗しました: %w", err)
	}
	tmpPath := tmp.Name()
	// 置き換えに失敗した場合に一時ファイルを残さない（成功後は存在しないため削除は失敗するが問題ない）
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("認証情報ファイルの書き込みに失敗しました: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("認証情報ファイルの書き込みに失敗しました: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return fmt.Errorf("認証情報ファイルのパーミッション設定に失敗しました: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("認証情報ファイルの置き換えに失敗しました: %w", err)
	}
	return nil
}

// Load はプロファイルの認証情報を読み込みます。保存されていない場合は ErrProfileNotFound を返します。
func (s *Store) Load(profile string) (*Credentials, error) {
	all, err := s.readAll()
	if err != nil {
		return nil, err
	}
	creds, ok := all[profile]
	if !ok {
		return nil, fmt.Errorf("%w (profile=%s)", ErrProfileNotFound, profile)
	}
	return &creds, nil
}

// Save はプロファイルの認証情報を保存します。ほかのプロファイルはそのまま残します。
// トークンの更新と同時に書き込んで内容を失わないよう、ロックを取得してから書き込みます。
func (s *Store) Save(ctx context.Context, profile string, creds *Credentials) error {
	unlock, err := s.Lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	return s.saveLocked(profile, creds)
}

// saveLocked は、呼び出し元がロックを取得済みであることを前提に、プロファイルの認証情報を保存します。
func (s *Store) saveLocked(profile string, creds *Credentials) error {
	all, err := s.readAll()
	if err != nil {
		return err
	}
	all[profile] = *creds
	return s.writeAll(all)
}

// Delete はプロファイルの認証情報を削除します。保存されていない場合は ErrProfileNotFound を返します。
// Save と同じく、ロックを取得してから書き込みます。
func (s *Store) Delete(ctx context.Context, profile string) error {
	unlock, err := s.Lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()

	all, err := s.readAll()
	if err != nil {
		return err
	}
	if _, ok := all[profile]; !ok {
		return fmt.Errorf("%w (profile=%s)", ErrProfileNotFound, profile)
	}
	delete(all, profile)
	return s.writeAll(all)
}

// Lock は認証情報ファイルに対する排他ロック（別プロセス間でも有効）を取得し、解除する関数を返します。
//
// ロックは認証情報ファイルと同じディレクトリの `<ファイル名>.lock` に対する flock で実現します。
// ctx の期限、または lockTimeout のどちらか早いほうを過ぎても取得できない場合はエラーを返します。
func (s *Store) Lock(ctx context.Context) (unlock func(), err error) {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return nil, fmt.Errorf("認証情報ディレクトリの作成に失敗しました: %w", err)
	}
	lockCtx, cancel := context.WithTimeout(ctx, lockTimeout)
	defer cancel()

	lock := flock.New(s.path + ".lock")
	locked, err := lock.TryLockContext(lockCtx, lockRetryInterval)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		return nil, fmt.Errorf("認証情報ファイルのロックに失敗しました: %w", err)
	}
	if !locked {
		return nil, fmt.Errorf("認証情報ファイルのロックを取得できませんでした（別の freee-hr が実行中の可能性があります）: %w", lockCtx.Err())
	}
	return func() { _ = lock.Unlock() }, nil
}
