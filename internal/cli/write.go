// write.go は、freee の会社データに書き込むコマンド（打刻・勤怠の修正・申請の作成）に共通の安全策を実装します。
//
// Claude Code などのエージェントが誤って書き込まないよう、送信までに次の関門を設けます。
//
//  1. --dry-run: API を呼ばず、送る予定のリクエストを JSON で標準出力に書いて終了コード0で終わります。
//  2. 読み取り専用のプロファイル（auth login --read-only）では、送信を拒否します（終了コード2）。
//  3. --yes が無い場合は、送る予定の内容を標準エラー出力に示して [y/N] で確認します。
//     改行で終わる y / yes 以外の答え、または標準入力が無い場合（Claude Code の Bash など）は送信せず、終了コード2で終わります。
//
// 送信したリクエストは自動で再試行しません（打刻などを二重に登録しないため）。
package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/kasseika/freee-cli/internal/freeehr"
)

// writeFlags は書き込み系コマンドに共通のフラグです。
type writeFlags struct {
	dryRun bool
	yes    bool
}

// addWriteFlags は --dry-run と --yes をコマンドに追加します。
func addWriteFlags(cmd *cobra.Command, wf *writeFlags) {
	cmd.Flags().BoolVar(&wf.dryRun, "dry-run", false, "送信せずに、送る予定のリクエストを JSON で出力する")
	cmd.Flags().BoolVarP(&wf.yes, "yes", "y", false, "確認なしで送信する（内容を確かめてから使うこと）")
}

// validate は --dry-run と --yes の組み合わせを検証します。どちらの意図か曖昧になるため、同時指定は使い方の誤りにします。
func (wf *writeFlags) validate() error {
	if wf.dryRun && wf.yes {
		return usageErrorf("--dry-run と --yes は同時に指定できません")
	}
	return nil
}

// writePlan は、--dry-run の出力と確認プロンプトで示す「送る予定の内容」です。
type writePlan struct {
	DryRun      bool   `json:"dry_run"`
	Profile     string `json:"profile"`
	CompanyID   int    `json:"company_id"`
	CompanyName string `json:"company_name"`
	// EmployeeID は対象の従業員IDです。申請の作成のように従業員IDを使わないコマンドでは 0 になり、出力しません。
	EmployeeID int `json:"employee_id,omitempty"`
	// Request は送る予定のリクエストです。埋め込みのため、JSON では method・path・query・body が同じ階層に並びます。
	freeehr.Request
}

// sendWrite は安全策（--dry-run・読み取り専用・確認）を通したうえで、書き込みのリクエストを送ります。
func (a *App) sendWrite(ctx context.Context, flags *globalFlags, s *session, wf *writeFlags, req freeehr.Request) error {
	plan := writePlan{
		DryRun:      wf.dryRun,
		Profile:     s.profile,
		CompanyID:   s.creds.CompanyID,
		CompanyName: s.creds.CompanyName,
		EmployeeID:  s.employeeID,
		Request:     req,
	}
	if wf.dryRun {
		return writeJSON(a.Stdout, plan)
	}
	if s.creds.ReadOnly {
		return usageErrorf("プロファイル「%s」は読み取り専用のため、送信できません（--dry-run で内容の確認だけはできます）", s.profile)
	}
	if !wf.yes {
		confirmed, err := a.confirmWrite(plan)
		if err != nil {
			return err
		}
		if !confirmed {
			return usageErrorf("送信を中止しました。確認なしで送信するには、内容をユーザーに確かめてから --yes を付けてください")
		}
	}

	// 確認を待った時間をタイムアウトに含めないよう、送信の直前にタイムアウトを設定する
	ctx, cancel := timeoutContext(ctx, flags)
	defer cancel()
	raw, err := s.client.Send(ctx, req)
	if err != nil {
		return err
	}
	return writeJSON(a.Stdout, raw)
}

// confirmWrite は送る予定の内容を標準エラー出力に示し、標準入力から [y/N] の答えを読みます。
// 改行で終わる y / yes だけを「はい」とし、入力の終わり（EOF）は途中まで入力があっても「いいえ」として扱います。
func (a *App) confirmWrite(plan writePlan) (bool, error) {
	var buf bytes.Buffer
	if err := writeJSON(&buf, plan); err != nil {
		return false, err
	}
	_, _ = fmt.Fprintf(a.Stderr, "次の内容を freee に送信します:\n%s", buf.String())
	_, _ = fmt.Fprint(a.Stderr, "この内容で送信しますか？ [y/N]: ")

	line, err := bufio.NewReader(a.Stdin).ReadString('\n')
	if errors.Is(err, io.EOF) {
		// 改行で終わらない入力（標準入力が無い・入力が途中で切れた）は、答えが確定していないため「いいえ」とする。
		// プロンプトの後ろで改行してから、呼び出し元がエラーを表示する
		_, _ = fmt.Fprintln(a.Stderr)
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("確認の入力の読み込みに失敗しました: %w", err)
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}
