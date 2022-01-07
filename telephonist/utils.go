package telephonist

import (
	"encoding/json"
	"errors"
)

func convertToStruct(i, objPtr interface{}) error {
	s, err := json.Marshal(i)
	if err != nil {
		return errors.New("failed to marshal data for type conversion: " + err.Error())
	}
	return json.Unmarshal(s, objPtr)
}
