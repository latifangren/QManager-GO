package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"qmanager/internal/atengine"
)

// SMS Handler Tests (sms.go)
func TestSMSHandler_Full(t *testing.T) {
	mock := atengine.NewMockTransport()
	eng := atengine.NewEngine(mock)
	t.Cleanup(func() { _ = eng.Close() })

	h := NewSMSHandler(eng)

	// 1. GetSMSCenter (List and storage)
	mock.SetResponse("AT+CPMS?", `+CPMS: "SM",5,50,"SM",5,50,"SM",5,50`+"\r\nOK")
	mock.SetResponse("AT+CMGF=1", "OK")
	mock.SetResponse("AT+CSCS=\"GSM\"", "OK")
	mock.SetResponse("AT+CPMS=\"SM\",\"SM\",\"SM\"", "OK")
	mock.SetResponse(`AT+CMGL="ALL"`, `+CMGL: 1,"REC READ","+628123456789",,"2026/08/30 12:00:00+28"
Test SMS Content
OK`)

	reqList := httptest.NewRequest(http.MethodGet, "/api/cellular/sms", nil)
	wList := httptest.NewRecorder()
	h.GetSMSCenter(wList, reqList)
	if wList.Code != http.StatusOK {
		t.Fatalf("GetSMSCenter returned %d, want 200: %s", wList.Code, wList.Body.String())
	}

	// 2. HandleSMSAction - send action
	mock.SetResponse("AT+CMGF=1", "OK")
	mock.SetResponse("AT+CSCS=\"GSM\"", "OK")
	mock.SetResponse("AT+CMGS=\"+62899999999\"", "+CMGS: 123\r\nOK")

	bodySend, _ := json.Marshal(map[string]interface{}{
		"action":  "send",
		"phone":   "+62899999999",
		"message": "Hello from test",
	})
	reqSend := httptest.NewRequest(http.MethodPost, "/api/cellular/sms", bytes.NewBuffer(bodySend))
	wSend := httptest.NewRecorder()
	h.HandleSMSAction(wSend, reqSend)
	if wSend.Code != http.StatusOK {
		t.Fatalf("HandleSMSAction send returned %d, want 200: %s", wSend.Code, wSend.Body.String())
	}

	// 3. SendSMS endpoint
	reqSendDirect := httptest.NewRequest(http.MethodPost, "/api/cellular/sms/send", bytes.NewBuffer(bodySend))
	wSendDirect := httptest.NewRecorder()
	h.SendSMS(wSendDirect, reqSendDirect)
	if wSendDirect.Code != http.StatusOK {
		t.Fatalf("SendSMS returned %d, want 200: %s", wSendDirect.Code, wSendDirect.Body.String())
	}

	// 4. HandleSMSAction - delete single index
	mock.SetResponse("AT+CMGD=1", "OK")
	idx := 1
	bodyDel, _ := json.Marshal(map[string]interface{}{
		"action": "delete",
		"index":  &idx,
	})
	reqDel := httptest.NewRequest(http.MethodPost, "/api/cellular/sms", bytes.NewBuffer(bodyDel))
	wDel := httptest.NewRecorder()
	h.HandleSMSAction(wDel, reqDel)
	if wDel.Code != http.StatusOK {
		t.Fatalf("HandleSMSAction delete returned %d, want 200", wDel.Code)
	}

	// 5. HandleSMSAction - delete_all
	mock.SetResponse("AT+CMGD=1,4", "OK")
	bodyDelAll, _ := json.Marshal(map[string]interface{}{
		"action": "delete_all",
	})
	reqDelAll := httptest.NewRequest(http.MethodPost, "/api/cellular/sms", bytes.NewBuffer(bodyDelAll))
	wDelAll := httptest.NewRecorder()
	h.HandleSMSAction(wDelAll, reqDelAll)
	if wDelAll.Code != http.StatusOK {
		t.Fatalf("HandleSMSAction delete_all returned %d, want 200", wDelAll.Code)
	}

	// 6. HandleSMSAction - invalid payload
	reqBad := httptest.NewRequest(http.MethodPost, "/api/cellular/sms", bytes.NewBufferString(`bad`))
	wBad := httptest.NewRecorder()
	h.HandleSMSAction(wBad, reqBad)
	if wBad.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for bad payload, got %d", wBad.Code)
	}
}
