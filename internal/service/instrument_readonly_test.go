package service

import "testing"

func TestCheckInstrumentImportAsyncFieldsAllowsAssetClass(t *testing.T) {
	body := []byte(`{"ticket_id":"tkt_test","asset_class":"bond","region":"domestic"}`)
	if err := CheckInstrumentImportAsyncFields(body); err != nil {
		t.Fatalf("expected asset_class and region allowed: %v", err)
	}
}

func TestCheckInstrumentImportAsyncFieldsRejectsName(t *testing.T) {
	body := []byte(`{"ticket_id":"tkt_test","asset_class":"bond","region":"foreign","name":"override"}`)
	if err := CheckInstrumentImportAsyncFields(body); err == nil {
		t.Fatal("expected name to be rejected")
	}
}
