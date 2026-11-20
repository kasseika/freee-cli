// main.go は freee-hr CLI の実行ファイルのエントリポイントです。
// 処理はすべて internal/cli の App に委ね、その終了コードでプロセスを終了します。
package main

import (
	"os"

	"github.com/kasseika/freee-cli/internal/cli"
)

func main() {
	os.Exit(cli.NewApp().Execute(os.Args[1:]))
}
