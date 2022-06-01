package telephonist

type Sequence struct {
	id string
	c  *Client
}

type EventDataWithoutSequence struct {
	Name string
	Data interface{}
}

func (s *Sequence) Publish(d EventDataWithoutSequence) error {
	return s.c.Publish(EventData{Name: d.Name, Data: d.Data, SequenceID: s.id})
}

func (s *Sequence) Finish(d FinishSequenceRequest) error {
	_, err := s.c.FinishSequence(s.id, d)
	return err
}
