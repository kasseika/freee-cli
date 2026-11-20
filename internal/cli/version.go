// version.go はビルド時に ldflags で埋め込むバージョン文字列を保持します。
package cli

// Version は freee-hr CLI のバージョンです。
// goreleaser のビルドで `-X github.com/kasseika/freee-cli/internal/cli.Version={{.Version}}` により上書きされます。
// ローカルのビルド（go build / go install）では "dev" のままです。
var Version = "dev"
