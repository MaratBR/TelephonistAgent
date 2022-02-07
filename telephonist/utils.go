package telephonist

import (
	"encoding/json"
)

func convertToStruct(raw json.RawMessage, objPtr interface{}) error {
	return json.Unmarshal(raw, objPtr)
}
