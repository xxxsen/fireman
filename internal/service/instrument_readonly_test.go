package service

import "testing"

// TestDefaultParametersCurrentAge verifies that non-wizard plan creation must
// default current age to 35 so the parameters page matches the wizard default.
func TestDefaultParametersCurrentAge(t *testing.T) {
	if got := defaultParameters("pln_test", nil).CurrentAge; got != 35 {
		t.Fatalf("default CurrentAge = %d, want 35", got)
	}
}

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
