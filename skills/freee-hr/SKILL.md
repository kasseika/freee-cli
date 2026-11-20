---
name: freee-hr
description: freee人事労務 API を freee-hr CLI から操作する。打刻（出勤・退勤・休憩）、いま打刻できる種類の確認、日ごとの勤怠記録や月の勤怠集計（労働時間・残業・有給の残り）の確認、勤怠の修正（出退勤・休憩・メモの登録や更新）、ログイン状態・事業所・従業員IDの確認など、freee人事労務・勤怠に関する依頼で使用する。
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

## 書き込み操作のルール（必ず守ること）

`timeclock punch` と `work-record update` は freee の会社データに書き込みます。次の手順を必ず守ってください。

1. 勤怠の修正の前に `freee-hr work-record get <日付>` で現在の記録（出退勤・休憩・メモ・`is_editable`）を確かめ、ユーザーに変更前と変更後を示す。`is_editable` が `false` の日は修正できない（締め済みなど）ので、その旨を伝える
2. 打刻の前に `freee-hr timeclock types` を実行し、頼まれた種類が `available_types` に含まれるかを確かめる。含まれない場合は打刻せず、いま打刻できる種類と `timeclock list` の直近の打刻をユーザーに伝える（例: 出勤済みなので出勤はできない）
3. `--dry-run` を付けて実行し、出力（`profile`・`company_name`・`employee_id`・`path`・`body`）をユーザーに見せる
4. ユーザーが**その内容で送信してよいと明示的に答えた場合だけ**、同じコマンドに `--yes` を付けて実行する（`--dry-run` は外す）
5. ユーザーの了承なしに `--yes` を付けない。「打刻して」と頼まれた場合も、種類（出勤・退勤など）と対象の事業所を dry-run で示して確認する
6. 失敗しても自動で再実行しない。終了コード4の場合は、エラー内容をユーザーに伝えて判断を仰ぐ（打刻が二重に登録されるおそれがあるため）。打刻の 400 では `timeclock types` と `timeclock list` で現在の状態を確かめてから伝える
7. サブコマンドを先に書き、`--profile` などのフラグは後ろに書く（例: `freee-hr timeclock punch clock_in --profile test --dry-run`）

`--yes` を付けずに実行すると、標準入力が無いため確認できず、送信せずに終了コード2で終わります。読み取り専用のプロファイルでは `--yes` を付けても送信できません（`--dry-run` は使えます）。

## 打刻

打刻の時刻は常に「いま」です。打刻の前に、いま打刻できる種類を確認してください。

```bash
# いま打刻できる種類（clock_in: 出勤 / break_begin: 休憩開始 / break_end: 休憩終了 / clock_out: 退勤）
freee-hr timeclock types | jq -r '.available_types[]'

# 出勤の打刻（まず dry-run で内容を確認する）
freee-hr timeclock punch clock_in --dry-run

# 日をまたいだ退勤（深夜0時を過ぎてからの退勤）は、打刻日に前日を指定する
freee-hr timeclock punch clock_out --base-date 2026-11-19 --dry-run

# 打刻の一覧（既定は当月の打刻開始日から当日まで。--limit は1〜100）
freee-hr timeclock list --from 2026-11-01 --to 2026-11-20
```

`timeclock types` に含まれない種類（例: 出勤済みなのに出勤、出勤していないのに退勤）は、freee が「打刻の種類が正しくありません。」の 400 で拒否します（終了コード4）。入社日より前の日付など、種類以外の原因の 400 もあるため、エラーの文面をそのままユーザーに伝えてください。

## 勤怠の確認

```bash
# 指定日の勤怠記録（出退勤時刻・休憩・労働時間など）
freee-hr work-record get 2026-11-20

# 給与支払い年月が 2026-11 の集計（分単位の労働時間・残業・深夜・有給の残りなど）
freee-hr work-record summary 2026-11

# 日ごとの勤怠記録も含める
freee-hr work-record summary 2026-11 --with-records
```

時間はすべて分単位です（例: `total_work_mins`）。

`summary` の年月は**給与支払い年月**です（カレンダーの月ではありません）。給与を翌月に支払う事業所では、`2026-11` が10月分の勤怠を指すことがあります。ユーザーに結果を伝えるときは、レスポンスの `start_date`・`end_date`（実際の集計期間）を必ず添えてください。ユーザーが「11月の勤怠」と言った場合は、`start_date` と `end_date` が11月を含むかを確かめ、違えば前後の年月で取り直してください。

## 勤怠の修正

```bash
# 現在の記録を確かめる（必ず最初に）
freee-hr work-record get 2026-11-20 | jq '{clock_in_at, clock_out_at, break_records, work_record_segments, note, is_editable}'

# 9:00〜18:00、休憩 12:00〜13:00 で登録する（まず dry-run）
freee-hr work-record update 2026-11-20 --clock-in 09:00 --clock-out 18:00 --break 12:00-13:00 --note "打刻漏れのため修正" --dry-run

# 日をまたぐ退勤は日付つきで指定する
freee-hr work-record update 2026-11-20 --clock-in 18:00 --clock-out "2026-11-21 02:00" --no-break --dry-run
```

- 出退勤（`--clock-in` と `--clock-out` は両方）を指定するときは、休憩を `--break HH:MM-HH:MM`（複数可）か `--no-break` で必ず指定する。既存の休憩を残したい場合も、`work-record get` で確かめた休憩を `--break` で指定し直す
- 休暇（有給の半休など）や複数の出退勤は、API の本文の形の JSON を `--file` で渡す（`-` で標準入力。日時は `YYYY-MM-DD HH:MM:SS`）。フラグで指定した項目はファイルより優先される。`company_id` は自動で入る
- `--file -` で標準入力を使う場合は確認プロンプトに答えられないため、dry-run でユーザーの了承を得てから `--yes` を付ける
- 振替出勤・振替休日・代休出勤・代休は API では登録できない
- 一般の従業員の権限では更新できない場合がある（終了コード4・403）。その場合は、freee の画面から勤務時間修正を申請するよう案内する

## 対象の従業員

対象は、ログイン時に選んだ事業所と、ログインユーザー本人です。管理者が他の従業員を扱う場合だけ `--employee-id <従業員ID>` を指定します。ユーザーが別の人を指定していないのに `--employee-id` を付けないでください。ログインユーザーが従業員として登録されていない場合は、終了コード2で `--employee-id` の指定を求められます。

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

# 給与支払い年月 2026-11 の集計期間と、総労働時間・時間外労働時間（時間に換算）
freee-hr work-record summary 2026-11 | jq '{start_date, end_date, work_hours: (.total_work_mins / 60), overtime_hours: (.total_overtime_except_normal_work_mins / 60)}'

# 今日の打刻の種類と時刻
freee-hr timeclock list --from 2026-11-20 --to 2026-11-20 | jq -r '.[] | "\(.type) \(.datetime)"'
```
