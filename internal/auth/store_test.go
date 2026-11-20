// store_test.go は認証情報ファイル（Store）の保存・読み込み・削除を検証します。
package auth

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// sampleCredentials はテストで使う、意味の分かる値を持つ認証情報です。
func sampleCredentials() *Credentials {
	employeeID := 456
	return &Credentials{
		ClientID:     "テスト用クライアントID",
		ClientSecret: "テスト用シークレット",
		AccessToken:  "アクセストークン1",
		RefreshToken: "リフレッシュトークン1",
		Expiry:       time.Date(2026, 11, 20, 18, 0, 0, 0, time.UTC),
		CompanyID:    123,
		CompanyName:  "テスト事業所",
		EmployeeID:   &employeeID,
		DisplayName:  "地活 太郎",
		ReadOnly:     true,
	}
}

// TestStore_SaveAndLoad は保存した認証情報をそのまま読み込めることを検証します。
func TestStore_SaveAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "freee-hr", "credentials.json")
	store := NewStore(path)

	if err := store.Save("default", sampleCredentials()); err != nil {
		t.Fatalf("保存に失敗しました: %v", err)
	}
	got, err := store.Load("default")
	if err != nil {
		t.Fatalf("読み込みに失敗しました: %v", err)
	}
	if got.CompanyID != 123 || got.CompanyName != "テスト事業所" || got.EmployeeID == nil || *got.EmployeeID != 456 {
		t.Errorf("事業所・従業員の情報が一致しません: %+v", got)
	}
	if got.RefreshToken != "リフレッシュトークン1" || !got.ReadOnly {
		t.Errorf("トークンまたは読み取り専用フラグが一致しません: %+v", got)
	}
}

// TestStore_FilePermission は認証情報ファイルが0600、ディレクトリが0700で作成されることを検証します。
func TestStore_FilePermission(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "freee-hr")
	path := filepath.Join(dir, "credentials.json")
	if err := NewStore(path).Save("default", sampleCredentials()); err != nil {
		t.Fatalf("保存に失敗しました: %v", err)
	}

	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := fileInfo.Mode().Perm(); perm != 0o600 {
		t.Errorf("ファイルのパーミッション = %o, 期待値 = 600", perm)
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("ディレクトリのパーミッション = %o, 期待値 = 700", perm)
	}
}

// TestStore_KeepsOtherProfiles は別プロファイルの保存で既存のプロファイルが消えないことを検証します。
func TestStore_KeepsOtherProfiles(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "credentials.json"))
	if err := store.Save("本番", sampleCredentials()); err != nil {
		t.Fatal(err)
	}
	if err := store.Save("テスト事業所", sampleCredentials()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load("本番"); err != nil {
		t.Errorf("既存のプロファイルが失われました: %v", err)
	}
}

// TestStore_LoadNotFound は未保存のプロファイルで ErrProfileNotFound を返すことを検証します。
func TestStore_LoadNotFound(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "credentials.json"))
	_, err := store.Load("存在しないプロファイル")
	if !errors.Is(err, ErrProfileNotFound) {
		t.Fatalf("エラー = %v, 期待値 = ErrProfileNotFound", err)
	}
}

// TestStore_Delete は削除したプロファイルが読み込めなくなること、未保存なら ErrProfileNotFound を返すことを検証します。
func TestStore_Delete(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "credentials.json"))
	if err := store.Save("default", sampleCredentials()); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("default"); err != nil {
		t.Fatalf("削除に失敗しました: %v", err)
	}
	if _, err := store.Load("default"); !errors.Is(err, ErrProfileNotFound) {
		t.Errorf("削除後も読み込めてしまいます: %v", err)
	}
	if err := store.Delete("default"); !errors.Is(err, ErrProfileNotFound) {
		t.Errorf("2回目の削除のエラー = %v, 期待値 = ErrProfileNotFound", err)
	}
}
