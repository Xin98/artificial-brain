package aliyun

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
)

// smsReport is one Aliyun SMS delivery report as pushed to the receipt
// webhook. Success is a pointer so a missing verdict is distinguishable from
// a false one.
type smsReport struct {
	PhoneNumber string `json:"phone_number"`
	SendTime    string `json:"send_time"`
	ReportTime  string `json:"report_time"`
	Success     *bool  `json:"success"`
	ErrCode     string `json:"err_code"`
	ErrMsg      string `json:"err_msg"`
	BizID       string `json:"biz_id"`
}

// ParseSmsReport parses one delivery-report body into the verdict the
// record-receipt command applies. A report is unusable without its biz_id
// (which delivery it belongs to) or its success verdict; malformed bodies
// and missing required fields are errors.
func ParseSmsReport(body []byte) (dto.ReceiptPayload, error) {
	var report smsReport
	if err := json.Unmarshal(body, &report); err != nil {
		return dto.ReceiptPayload{}, fmt.Errorf("aliyun: parse sms report: %w", err)
	}
	if report.BizID == "" {
		return dto.ReceiptPayload{}, errors.New("aliyun: sms report missing biz_id")
	}
	if report.Success == nil {
		return dto.ReceiptPayload{}, errors.New("aliyun: sms report missing success")
	}
	return dto.ReceiptPayload{
		ProviderMessageID: report.BizID,
		Delivered:         *report.Success,
		ErrorCode:         report.ErrCode,
	}, nil
}
