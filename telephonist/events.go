package telephonist

type EventQueue struct {
	Channel    chan Event
	wsc        *WSClient
	subscribed map[string]uint32
}

func (q *EventQueue) Subcribe(eventKey string) bool {
	isSubscribed := q.IsSubscribed(eventKey)
	if !isSubscribed {
		q.subscribed[eventKey] = q.wsc.addEventChannel(eventKey, q.Channel)
	}
	return isSubscribed
}

func (q *EventQueue) Unsubscribe(eventKey string) bool {
	isSubscribed := q.IsSubscribed(eventKey)
	if isSubscribed {
		q.wsc.removeEventChannel(eventKey, q.subscribed[eventKey])
		delete(q.subscribed, eventKey)
	}
	return isSubscribed
}

func (q *EventQueue) IsSubscribed(eventKey string) bool {
	_, exists := q.subscribed[eventKey]
	return exists
}

func (q *EventQueue) UnsubscribeAll() {
	for eventKey, id := range q.subscribed {
		q.wsc.removeEventChannel(eventKey, id)
	}
}
