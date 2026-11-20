# freee-cli

[freee人事労務 API](https://developer.freee.co.jp/reference/hr/reference) を操作する非公式の CLI `freee-hr` と、Claude Code 用の Agent Skill を同梱するリポジトリです。

Claude Code などのエージェントが、必要なときだけ Skill を読み込み、JSON の出力を `jq` で絞り込みながら freee人事労務を扱えるようにすることを目的としています。

> 開発中です。v0.1 では打刻・勤怠の確認・勤怠の修正・申請（作成・一覧）に対応する予定です。現在は認証（`auth`）とログインユーザーの取得（`user me`）だけに対応しています。

## インストール

### go install（開発者向け）

```bash
go install github.com/kasseika/freee-cli/cmd/freee-hr@latest
```

### ソースからビルド

```bash
git clone https://github.com/kasseika/freee-cli.git
cd freee-cli
go build -o freee-hr ./cmd/freee-hr
./freee-hr --version
```

v0.1.0 以降は、[Releases](https://github.com/kasseika/freee-cli/releases) から OS・CPU に合ったアーカイブ（例: Apple Silicon の macOS なら `freee-hr_<version>_darwin_arm64.tar.gz`）もダウンロードできます。

## freee アプリの登録

freee人事労務 API を使うには、事前に freee のアプリを登録する必要があります。

1. 対象の事業所に所属するアカウントで [アプリ管理](https://app.secure.freee.co.jp/developers/applications) を開き、「新規追加」でアプリを作成します。種類は **プライベートアプリ** を選びます（後から変更できません）。
2. 「基本設定」タブの **コールバックURL** に `http://localhost:8765/callback` を設定します。
   - 設定できない場合は既定値（`urn:ietf:wg:oauth:2.0:oob`）のままにし、ログイン時に `--manual` を付けてください。
3. 「権限設定」で、人事労務のうち次の権限だけを有効にします（給与・年末調整・従業員の個人情報などは不要です）。
   - ユーザー情報 / 打刻 / 勤怠 / 勤怠の申請（勤務時間修正・有給休暇・特別休暇・残業・月次締め） / 申請経路
4. 表示された **Client ID** と **Client Secret** を、パスワード管理ツールなどで安全に保管します。

## 使い方

### ログイン

Client ID と Client Secret は環境変数で渡します（`--client-id` / `--client-secret` でも指定できますが、シェルの履歴に残ります）。

```bash
FREEE_HR_CLIENT_ID='<Client ID>' FREEE_HR_CLIENT_SECRET='<Client Secret>' freee-hr auth login
```

ブラウザが開くので、事業所を選んで「許可する」を押してください。ログインに成功すると、選んだ事業所と従業員IDが JSON で表示されます。

| フラグ | 説明 |
|---|---|
| `--profile <名前>` | 事業所ごとにプロファイルを分けます（省略時は `default`）。利用時にも同じ `--profile` を指定します |
| `--manual` | ブラウザに表示された認可コードを貼り付ける方式でログインします |
| `--read-only` | 読み取り専用のプロファイルにします（打刻・勤怠の修正・申請の作成を拒否します） |
| `--port <番号>` | 認可コードを受け取るポートです（既定は `8765`。コールバックURLのポートと一致させます） |

```bash
# 開発用テスト事業所を「test」プロファイルとして登録する
FREEE_HR_CLIENT_ID='<Client ID>' FREEE_HR_CLIENT_SECRET='<Client Secret>' freee-hr --profile test auth login

# ログイン状態（事業所・従業員ID・読み取り専用か）の確認
freee-hr --profile test auth status

# ログインユーザーと所属する事業所
freee-hr --profile test user me

# ログアウト（保存済みの認証情報を削除）
freee-hr --profile test auth logout
```

認証情報は `~/.config/freee-hr/credentials.json`（パーミッション 0600）に保存されます。アクセストークン（有効期限6時間）は、期限が切れると自動で更新されます。リフレッシュトークン（有効期限90日）が失効した場合は、もう一度 `auth login` を実行してください。

### 出力と終了コード

出力は常に JSON です。エラーは標準エラー出力に `エラー: ...` の形で書き込みます。

| コード | 意味 |
|---|---|
| 0 | 成功 |
| 1 | 一般的な失敗（ネットワーク・ファイルの入出力など） |
| 2 | 使い方の誤り（不正なフラグの値・必須の指定が無いなど） |
| 3 | 未認証（プロファイルが無い・トークンの更新に失敗した）。`freee-hr auth login` が必要です |
| 4 | freee API がエラーを返した（429・403 を含む） |

## Claude Code との連携

`skills/freee-hr/SKILL.md` に、Claude Code がこの CLI を操作するためのガイドがあります。このリポジトリを Claude Code のプロジェクトに含めるか、`skills/freee-hr` を `~/.claude/skills/` にコピーしてください。

## 開発

```bash
go build ./...
go vet ./...
go test ./...
golangci-lint run ./...
```

## ライセンス

[MIT](LICENSE)
