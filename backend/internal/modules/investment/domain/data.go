package domain

import "time"

type Provenance struct {
	Source, SourceRecordID string
	IngestedAt             time.Time
}
type TickerChange struct {
	Ticker      string
	EffectiveAt time.Time
}
type Instrument struct {
	Provenance
	ID, Ticker, Name, CIK, Exchange, SIC, Kind string
	Tradable                                   bool
	TickerHistory                              []TickerChange
}
type Bar struct {
	Provenance
	InstrumentID             string
	SessionDate, AvailableAt time.Time
	Open, High, Low, Close   Price
	Volume                   Quantity
	NoOpeningTrade           bool
}
type FinancialFact struct {
	Provenance
	InstrumentID, Accession, Concept, Value, Unit, Currency string
	PeriodStart, PeriodEnd, AvailableAt                     time.Time
}
type CorporateAction struct {
	Provenance
	ID, InstrumentID, Kind, Currency string
	EffectiveAt, AvailableAt, PayAt  time.Time
	RatioNumerator, RatioDenominator int64
	Amount                           Price
	CashInLieu                       *Money
}
type NewsItem struct {
	Provenance
	ID, Title, Summary, URL  string
	InstrumentIDs            []string
	PublishedAt, AvailableAt time.Time
}
type Snapshot struct {
	ID, DatasetVersion, Mode, Feed string
	AsOf                           time.Time
	Instruments                    []Instrument
	Bars                           []Bar
	Facts                          []FinancialFact
	Actions                        []CorporateAction
	News                           []NewsItem
	QualityFlags                   []string
	Calendar                       Calendar
}
