package telephonist

type Sequence struct {
	id string
	c  *Client
}

type EventDataWithoutSequence struct {
	Name string
	Data interface{}
}

func (s *Sequence) Publish(d EventDataWithoutSequence) *CombinedError {
	return s.c.Publish(EventData{Name: d.Name, Data: d.Data, SequenceID: s.id})
}

func (s *Sequence) Finish(d FinishSequenceRequest) *CombinedError {
	_, err := s.c.FinishSequence(s.id, d)
	return err
}
