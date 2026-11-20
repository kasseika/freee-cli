// usage_test.go は、コマンドの使い方を誤った場合に終了コード2（使い方の誤り）になることを検証します。
// エージェントが「自分の呼び出し方の誤り」と「通信などの一時的な失敗（終了コード1）」を区別できるようにするためです。
package cli

import "testing"

// TestUsageErrors は、不正なフラグの値・未知のサブコマンド・余分な引数・不正なタイムアウトが終了コード2になることを検証します。
func TestUsageErrors(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"数値のフラグに文字列を指定", []string{"auth", "login", "--port", "八千"}},
		{"存在しないフラグ", []string{"user", "me", "--存在しないフラグ"}},
		{"存在しないコマンド", []string{"打刻"}},
		{"存在しないサブコマンド", []string{"auth", "foo"}},
		{"余分な引数", []string{"user", "me", "余分な引数"}},
		{"タイムアウトが0秒", []string{"--timeout", "0", "user", "me"}},
		{"タイムアウトが負の値", []string{"--timeout", "-5", "auth", "status"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := newTestEnv(t, nil)
			env.saveLoggedIn(t, "default", false)
			if code := env.app.Execute(c.args); code != 2 {
				t.Errorf("終了コード = %d, 期待値 = 2, stderr=%s", code, env.stderr.String())
			}
		})
	}
}

// TestGroupCommandWithoutSubcommand は、サブコマンドを省略した場合はヘルプを表示して成功することを検証します。
func TestGroupCommandWithoutSubcommand(t *testing.T) {
	env := newTestEnv(t, nil)
	if code := env.app.Execute([]string{"auth"}); code != 0 {
		t.Fatalf("終了コード = %d, 期待値 = 0, stderr=%s", code, env.stderr.String())
	}
	if env.stdout.Len() == 0 {
		t.Error("ヘルプが表示されていません")
	}
}
