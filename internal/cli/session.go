// session.go は、各コマンドが freee API を呼ぶ前に確定させる「誰の・どの事業所のデータを扱うか」をまとめます。
//
// 事業所はログイン時に選んだもの（プロファイルの company_id）を使います。従業員は、--employee-id を
// 指定した場合はその従業員、省略した場合はログインユーザー本人（プロファイルの employee_id）です。
// 本人が従業員として登録されておらず、--employee-id も無い場合は、推測せずに使い方の誤り（終了コード2）にします。
package cli

import (
	"github.com/spf13/cobra"

	"github.com/kasseika/freee-cli/internal/auth"
	"github.com/kasseika/freee-cli/internal/freeehr"
)

// employeeIDFlag は --employee-id フラグの名前です。
const employeeIDFlag = "employee-id"

// session は1回のコマンド実行で使う API クライアントと、対象の事業所・従業員です。
type session struct {
	profile    string
	client     *freeehr.Client
	creds      *auth.Credentials
	employeeID int
}

// addEmployeeIDFlag は --employee-id フラグをコマンドに追加します。
func addEmployeeIDFlag(cmd *cobra.Command, employeeID *int) {
	cmd.Flags().IntVar(employeeID, employeeIDFlag, 0, "対象の従業員ID（省略時はログインユーザー本人。他の従業員を扱うには管理者の権限が必要）")
}

// openEmployeeSession はプロファイルを読み込み、対象の従業員を確定させた session を返します。
// プロファイルが無い場合は auth.ErrProfileNotFound（終了コード3）を返します。
func (a *App) openEmployeeSession(cmd *cobra.Command, flags *globalFlags, employeeID int) (*session, error) {
	explicit := cmd.Flags().Changed(employeeIDFlag)
	if explicit && employeeID <= 0 {
		return nil, usageErrorf("--%s には1以上の従業員IDを指定してください: %d", employeeIDFlag, employeeID)
	}
	client, creds, err := a.newClient(flags.profile)
	if err != nil {
		return nil, err
	}
	if !explicit {
		if creds.EmployeeID == nil {
			return nil, usageErrorf("ログインユーザーは事業所「%s」に従業員として登録されていません。--%s で対象の従業員を指定してください", creds.CompanyName, employeeIDFlag)
		}
		employeeID = *creds.EmployeeID
	}
	return &session{profile: flags.profile, client: client, creds: creds, employeeID: employeeID}, nil
}

// sendRead は読み取りのリクエストを送り、API のレスポンスをそのまま JSON で出力します。
func (a *App) sendRead(cmd *cobra.Command, flags *globalFlags, s *session, req freeehr.Request) error {
	ctx, cancel := timeoutContext(cmd.Context(), flags)
	defer cancel()
	raw, err := s.client.Send(ctx, req)
	if err != nil {
		return err
	}
	return writeJSON(a.Stdout, raw)
}
