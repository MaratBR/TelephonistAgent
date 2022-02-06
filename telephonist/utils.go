package telephonist

import (
	"encoding/json"
	"sync"
)

func convertToStruct(raw json.RawMessage, objPtr interface{}) error {
	return json.Unmarshal(raw, objPtr)
}

func runOnWG(wg *sync.WaitGroup, fn func()) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		fn()
	}()
}
