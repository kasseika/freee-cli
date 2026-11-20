// user.go はログインユーザー（GET /api/v1/users/me）のレスポンスのうち、
// CLI が内部で使う部分（事業所ごとの従業員ID・表示名）を解析します。
package freeehr

import (
	"encoding/json"
	"fmt"
)

// Me はログインユーザーの情報です。
type Me struct {
	ID        int         `json:"id"`
	Companies []MeCompany `json:"companies"`
}

// MeCompany はログインユーザーが所属する事業所と、その事業所での従業員情報です。
type MeCompany struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
	// EmployeeID はその事業所での従業員IDです。従業員として登録されていない場合は nil です。
	EmployeeID  *int   `json:"employee_id"`
	DisplayName string `json:"display_name"`
}

// ParseMe は GET /api/v1/users/me のレスポンスを解析します。
func ParseMe(raw json.RawMessage) (*Me, error) {
	var me Me
	if err := json.Unmarshal(raw, &me); err != nil {
		return nil, fmt.Errorf("ログインユーザー情報の解析に失敗しました: %w", err)
	}
	return &me, nil
}

// Company は指定したIDの事業所を返します。所属していない場合は ok=false です。
func (m *Me) Company(companyID int) (company MeCompany, ok bool) {
	for _, c := range m.Companies {
		if c.ID == companyID {
			return c, true
		}
	}
	return MeCompany{}, false
}
