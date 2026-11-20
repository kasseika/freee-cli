# freee-cli

[freee人事労務 API](https://developer.freee.co.jp/reference/hr/reference) を操作する非公式の CLI `freee-hr` と、Claude Code 用の Agent Skill を同梱するリポジトリです。

Claude Code などのエージェントが、必要なときだけ Skill を読み込み、JSON の出力を `jq` で絞り込みながら freee人事労務を扱えるようにすることを目的としています。

> 開発中です。v0.1 では打刻・勤怠の確認・勤怠の修正・申請（作成・一覧）に対応する予定です。現在は認証（`auth`）・ログインユーザーの取得（`user me`）・打刻（`timeclock`）・勤怠の確認（`work-record get / summary`）・勤怠の修正（`work-record update`）・申請（`approval`）に対応しています。

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

### ログインで `invalid_grant` が出る場合

ブラウザで許可したあと「トークンの取得に失敗しました: oauth2: "invalid_grant" ...」と表示される場合、freee のドキュメント（[40x エラーの確認ポイント](https://developer.freee.co.jp/reference/40x-checkpoint)）では、client_id と client_secret が正しいかの確認が挙げられています。エラーには送信した `client_id`・`redirect_uri` と Client Secret の文字数が表示されるので、次を確認してください。

1. Client Secret が、その Client ID のアプリのものと一致しているか（アプリ管理画面の値と文字数を比べてください）
2. アプリ管理画面のコールバックURLが、表示された `redirect_uri`（既定は `http://localhost:8765/callback`）と完全に一致し、保存されているか
3. ブラウザで許可してから、すぐにログインを完了したか（認可コードは1回しか使えません）

### 打刻と勤怠の確認

対象は、ログイン時に選んだ事業所と、ログインユーザー本人（従業員）です。管理者が他の従業員を扱う場合は `--employee-id <従業員ID>` を指定します。

```bash
# いま打刻できる種類（clock_in / break_begin / break_end / clock_out）
freee-hr timeclock types

# 打刻の一覧（期間・件数は省略可）
freee-hr timeclock list --from 2026-11-01 --to 2026-11-20

# 打刻する（送信前に確認を求めます）
freee-hr timeclock punch clock_in

# 日をまたいだ退勤は、打刻日に前日を指定する
freee-hr timeclock punch clock_out --base-date 2026-11-19

# 指定日の勤怠記録
freee-hr work-record get 2026-11-20

# 月の集計（労働時間・残業・有給の残りなど）。--with-records で日ごとの記録も含める
freee-hr work-record summary 2026-11
```

`work-record summary` の年月は **給与支払い年月** です（freee の API の仕様）。給与を翌月に支払う事業所では、`2026-11` が10月分の勤怠を指すことがあります。実際の集計期間はレスポンスの `start_date`・`end_date` で確認してください。

### 勤怠の修正

指定日の勤怠記録を登録・更新します。出退勤を指定するときは、休憩を `--break`（複数可）か `--no-break` で必ず指定します。

```bash
# 9:00〜18:00、休憩 12:00〜13:00、メモつきで登録する（送信前に確認を求めます）
freee-hr work-record update 2026-11-20 --clock-in 09:00 --clock-out 18:00 --break 12:00-13:00 --note "打刻漏れのため修正"

# 日をまたぐ退勤は日付つきで指定する
freee-hr work-record update 2026-11-20 --clock-in 18:00 --clock-out "2026-11-21 02:00" --no-break

# 休暇など、フラグに無い項目は API の本文の形の JSON で指定する（フラグの値が優先されます）
freee-hr work-record update 2026-11-20 --file work-record.json --dry-run
```

`--file` の JSON は [勤怠の更新 API](https://developer.freee.co.jp/reference/hr/reference) の本文と同じ形です（日時は `YYYY-MM-DD HH:MM:SS`）。`company_id` はプロファイルの事業所が自動で入ります。振替出勤・振替休日・代休出勤・代休は API では登録できません。

### 申請（勤怠申請）

勤務時間修正・有給休暇・特別休暇・残業・月次締めの申請を作成し、一覧と詳細を表示します。

| 種類 | コマンド | 対象 |
|---|---|---|
| 勤務時間修正申請 | `approval work-time` | 日（YYYY-MM-DD） |
| 有給休暇申請 | `approval paid-holiday` | 日 |
| 特別休暇申請 | `approval special-holiday` | 日 |
| 残業申請 | `approval overtime-work` | 日 |
| 月次勤怠締め申請 | `approval monthly-attendance` | 年月（YYYY-MM） |

申請の作成には、申請経路ID（`--route-id`）が必須です。申請時に承認者を選ぶ型の経路（`approval route get` のステップの `resource_type` が `selected_user`）では、承認者のユーザーID（`--approver-id`）も指定します。承認者のIDは**従業員IDではなくユーザーID**です（自分のユーザーIDは `freee-hr user me` の `id`）。

```bash
# 勤怠申請に使える申請経路と、その承認のステップ
freee-hr approval route list
freee-hr approval route get 1582563

# 勤務時間修正申請（9:00〜18:00、休憩 12:00〜13:00）。送信前に確認を求めます
freee-hr approval work-time create 2026-11-19 --clock-in 09:00 --clock-out 18:00 --break 12:00-13:00 --route-id 1582563 --approver-id 111 --comment "打刻漏れのため"

# 有給休暇申請（全休。時間休は --type hourly --start 10:00 --end 12:00）
freee-hr approval paid-holiday create 2026-12-01 --type full --route-id 1582563 --approver-id 111

# 特別休暇申請（半休・時間休は --start と --end が必須）
freee-hr approval special-holiday create 2026-12-02 --setting-id 55 --type full --route-id 1582563 --approver-id 111

# 残業申請。どの時刻を指定するかは事業所の設定で変わるため、先に設定を確かめる
freee-hr approval overtime-work setting --date 2026-11-20
freee-hr approval overtime-work create 2026-11-20 --overtime-start 18:00 --overtime-end 20:00 --route-id 1582563 --approver-id 111

# 月次勤怠締め申請
freee-hr approval monthly-attendance create 2026-11 --route-id 1582563 --approver-id 111

# 申請の一覧（状態・対象日の期間・申請者のユーザーIDで絞り込める）と詳細
freee-hr approval paid-holiday list --status in_progress --from 2026-12-01 --to 2026-12-31
freee-hr approval paid-holiday get 77
```

- 残業申請は、`overtime-work setting` の `should_reflect_in_work_record` が `true`（勤怠に反映する）なら `--overtime-start`/`--overtime-end`・`--early-start`/`--early-end` を、`false` なら `--start`/`--end` を指定します。反映する設定では、残業の開始は所定の退勤時刻、早出の終了は所定の出勤時刻と同じにします。
- フラグに無い項目（遅刻・早退の分数、日をまたぐ時刻、勤怠に反映する設定での残業申請の取り消しなど）は、`--file` に [申請の作成 API](https://developer.freee.co.jp/reference/hr/reference) の本文と同じ形の JSON を指定します。フラグの値が優先され、`company_id` は自動で入ります。
- 部門役職データ連携を使う申請経路（役職指定・部門および役職指定）は、API では参照だけができ、申請の作成には使えません。
- 申請の更新・取り下げ・承認は v0.1 では対応していません。freee の画面で操作してください。

### 書き込み系コマンドの安全策

freee の会社データに書き込むコマンド（`timeclock punch`・`work-record update`・`approval <種類> create`）は、次の順に安全策を通ります。

1. `--dry-run` を付けると、API を呼ばずに送る予定のリクエスト（事業所・従業員・メソッド・パス・本文）を JSON で出力します。
2. 読み取り専用のプロファイル（`auth login --read-only`）では送信を拒否します（終了コード2）。
3. `--yes` が無い場合は、送る予定の内容を表示して `[y/N]` で確認します。`y` 以外の答え、または標準入力が無い場合は送信しません（終了コード2）。
4. 送信に失敗しても自動で再試行しません（二重の打刻を防ぐため）。

Claude Code から使う場合は、プロジェクトの `.claude/settings.json` で書き込み系コマンドを `ask` にしておくと、Claude Code が `--yes` を付けて実行しようとしても、人の確認が入ります（[権限の設定](https://code.claude.com/docs/en/permissions)。`ask` は `allow` より優先されます）。グローバルフラグをサブコマンドの前に書いた場合と、カレントディレクトリでビルドしたバイナリ（`./freee-hr`）を使う場合にも一致するよう、コマンドごとに4通りの形を並べます。

```json
{
  "permissions": {
    "ask": [
      "Bash(freee-hr timeclock punch *)",
      "Bash(freee-hr * timeclock punch *)",
      "Bash(./freee-hr timeclock punch *)",
      "Bash(./freee-hr * timeclock punch *)",
      "Bash(freee-hr work-record update *)",
      "Bash(freee-hr * work-record update *)",
      "Bash(./freee-hr work-record update *)",
      "Bash(./freee-hr * work-record update *)",
      "Bash(freee-hr approval * create *)",
      "Bash(freee-hr * approval * create *)",
      "Bash(./freee-hr approval * create *)",
      "Bash(./freee-hr * approval * create *)"
    ]
  }
}
```

この設定はコマンドの文字列に対する一致で判定されるため、絶対パス（`/usr/local/bin/freee-hr` など）や `sh -c '...'` 経由の呼び出しには一致しません。`freee-hr` は PATH の通った場所にインストールし、コマンド名だけで呼び出してください（Skill もその前提で書かれています）。CLI 側の確認（`--yes` が無ければ送信しない）は、呼び出し方に関わらず働きます。

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
