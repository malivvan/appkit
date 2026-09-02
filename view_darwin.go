package appkit

import "encoding/json"

type cursorRequest struct {
	Edge string `json:"edge"`
}

func parseCursor(params json.RawMessage) cursorRequest {
	var arr []json.RawMessage
	if err := json.Unmarshal(params, &arr); err != nil || len(arr) == 0 {
		return cursorRequest{}
	}
	var p cursorRequest
	_ = json.Unmarshal(arr[0], &p)
	return p
}
