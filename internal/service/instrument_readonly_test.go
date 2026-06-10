package service

import "testing"

func TestCheckInstrumentImportAsyncFieldsAllowsAssetClass(t *testing.T) {
	body := []byte(`{"ticket_id":"tkt_test","asset_class":"bond"}`)
	if err := CheckInstrumentImportAsyncFields(body); err != nil {
		t.Fatalf("expected asset_class allowed: %v", err)
	}
}

func TestCheckInstrumentImportAsyncFieldsRejectsName(t *testing.T) {
	body := []byte(`{"ticket_id":"tkt_test","asset_class":"bond","name":"override"}`)
	if err := CheckInstrumentImportAsyncFields(body); err == nil {
		t.Fatal("expected name to be rejected")
	}
}
