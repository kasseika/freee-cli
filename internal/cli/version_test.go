// version_test.go は `freee-hr --version` を検証します。
package cli

import (
	"strings"
	"testing"
)

// TestVersion は、--version でバージョン文字列を出力することを検証します。
func TestVersion(t *testing.T) {
	env := newTestEnv(t, nil)

	code := env.app.Execute([]string{"--version"})
	if code != 0 {
		t.Fatalf("終了コード = %d, 期待値 = 0", code)
	}
	if !strings.Contains(env.stdout.String(), Version) {
		t.Errorf("出力 = %q, 期待値 = バージョン %q を含む", env.stdout.String(), Version)
	}
}
