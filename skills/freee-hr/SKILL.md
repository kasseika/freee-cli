---
name: freee-hr
description: freee人事労務 API を freee-hr CLI から操作する。freee人事労務のログイン状態の確認、ログインユーザー・事業所・従業員IDの確認など、freee人事労務に関する依頼で使用する。
---

# freee-hr CLI

freee人事労務 API を操作する非公式の CLI です。`freee-hr <resource> <action>` の形式で操作します。

出力は常に JSON です。必要な部分は `jq` で絞り込んでください。エラーは標準エラー出力に `エラー: ...` の形で出力されます。

## 前提: 認証確認

コマンドを実行する前に、必ず認証状態を確認してください。

```bash
freee-hr auth status
```

- 終了コード3の場合は未ログイン、またはリフレッシュトークンが失効しています。ユーザーに `freee-hr auth login` の実行を依頼してください。ブラウザでの操作と Client ID / Client Secret が必要なため、Claude Code が代わりにログインすることはできません。
- **Client ID / Client Secret をチャットで受け取ったり、コマンドの引数に書いたりしないでください。** ユーザー自身のターミナルで、環境変数 `FREEE_HR_CLIENT_ID` / `FREEE_HR_CLIENT_SECRET` を付けて実行してもらいます。
- `--profile <名前>` で事業所（本番・開発用テスト事業所など）を切り替えます。省略時は `default` です。どのプロファイルを使うか分からない場合は、ユーザーに確認してください。
- `auth status` の `read_only` が `true` のプロファイルでは、書き込み系のコマンドは実行できません。

## 終了コード

| コード | 意味 |
|---|---|
| 0 | 成功 |
| 1 | 一般的な失敗（ネットワーク・ファイルの入出力など） |
| 2 | 使い方の誤り（不正なフラグの値・必須の指定が無いなど） |
| 3 | 未認証（`freee-hr auth login` が必要） |
| 4 | freee API がエラーを返した（標準エラー出力に理由が出る。429 はリクエスト回数の上限、403 は権限不足） |

429 が返った場合は、メッセージに含まれる解除時刻まで待ってください。自動での再試行はしません。

## よく使うコマンド

```bash
# ログイン状態（事業所・従業員ID・読み取り専用か）
freee-hr auth status

# ログインユーザーと所属する事業所の一覧
freee-hr user me
```

### jq での絞り込み例

```bash
# いまのプロファイルの事業所IDと従業員ID
freee-hr auth status | jq '{company_id, employee_id}'

# 所属する事業所の名前と自分の従業員ID
freee-hr user me | jq '.companies[] | {name, employee_id}'
```
