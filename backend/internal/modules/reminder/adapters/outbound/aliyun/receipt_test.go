package aliyun

import (
	"testing"
)

const deliveredReportFixture = `{
	"phone_number": "13800138000",
	"send_time": "2026-08-20 08:00:01",
	"report_time": "2026-08-20 08:00:03",
	"success": true,
	"err_code": "",
	"err_msg": "用户接收成功",
	"biz_id": "biz-0001"
}`

const failedReportFixture = `{
	"phone_number": "13800138000",
	"send_time": "2026-08-20 08:00:01",
	"report_time": "2026-08-20 08:00:09",
	"success": false,
	"err_code": "MOBILE_IN_BLACK",
	"err_msg": "手机号在黑名单",
	"biz_id": "biz-0002"
}`

func TestParseSmsReportDelivered(t *testing.T) {
	payload, err := ParseSmsReport([]byte(deliveredReportFixture))
	if err != nil {
		t.Fatalf("ParseSmsReport() error = %v", err)
	}
	if payload.ProviderMessageID != "biz-0001" {
		t.Fatalf("ProviderMessageID = %q, want biz-0001", payload.ProviderMessageID)
	}
	if !payload.Delivered {
		t.Fatal("Delivered = false, want true")
	}
	if payload.ErrorCode != "" {
		t.Fatalf("ErrorCode = %q, want empty for a delivered report", payload.ErrorCode)
	}
}

func TestParseSmsReportFailedDelivery(t *testing.T) {
	payload, err := ParseSmsReport([]byte(failedReportFixture))
	if err != nil {
		t.Fatalf("ParseSmsReport() error = %v", err)
	}
	if payload.ProviderMessageID != "biz-0002" {
		t.Fatalf("ProviderMessageID = %q, want biz-0002", payload.ProviderMessageID)
	}
	if payload.Delivered {
		t.Fatal("Delivered = true, want false")
	}
	if payload.ErrorCode != "MOBILE_IN_BLACK" {
		t.Fatalf("ErrorCode = %q, want MOBILE_IN_BLACK", payload.ErrorCode)
	}
}

func TestParseSmsReportMissingBizID(t *testing.T) {
	body := `{"phone_number":"13800138000","send_time":"2026-08-20 08:00:01","report_time":"2026-08-20 08:00:03","success":true,"err_code":"","err_msg":"ok"}`
	if _, err := ParseSmsReport([]byte(body)); err == nil {
		t.Fatal("ParseSmsReport() error = nil, want missing biz_id error")
	}
}

func TestParseSmsReportMissingSuccess(t *testing.T) {
	body := `{"phone_number":"13800138000","send_time":"2026-08-20 08:00:01","report_time":"2026-08-20 08:00:03","err_code":"","err_msg":"ok","biz_id":"biz-0003"}`
	if _, err := ParseSmsReport([]byte(body)); err == nil {
		t.Fatal("ParseSmsReport() error = nil, want missing success error")
	}
}

func TestParseSmsReportMalformedJSON(t *testing.T) {
	if _, err := ParseSmsReport([]byte(`{not-json`)); err == nil {
		t.Fatal("ParseSmsReport() error = nil, want malformed JSON error")
	}
	if _, err := ParseSmsReport(nil); err == nil {
		t.Fatal("ParseSmsReport(nil) error = nil, want error")
	}
}
