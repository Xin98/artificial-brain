package dto

import "encoding/json"

type RequestRecord struct {
	Hash     string
	Response json.RawMessage
	Claimed  bool
}
