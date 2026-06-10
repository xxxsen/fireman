package repository

// InstrumentFetchPayload is stored in jobs.payload_json for instrument_fetch jobs.
type InstrumentFetchPayload struct {
	InstrumentID   string `json:"instrument_id"`
	Market         string `json:"market"`
	InstrumentType string `json:"instrument_type"`
	Code           string `json:"code"`
	ProviderSymbol string `json:"provider_symbol"`
	AdjustPolicy   string `json:"adjust_policy"`
}
