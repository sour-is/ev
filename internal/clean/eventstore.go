package clean

import "encoding"

type EventLog[T, K, C comparable, E any] interface {
	EventLog(T) List[K, C, E]
}

type EventStore[T, K, C comparable, E, A any] interface {
	Bus[T, K, E]
	EventLog[T, K, C, E]

	Load(T, A) error
	Store(A) error
	Truncate(T) error
}

type Event[T, C comparable, V any] struct {
	Topic    T
	Position C
	Payload  V
}

type codec interface {
	encoding.BinaryMarshaler
	encoding.BinaryUnmarshaler
}

type aggr = struct{}

type evvent = Event[string, uint64, codec]
type evvee = EventStore[string, string, uint64, evvent, aggr]
type evvesub = Subscription[Event[string, uint64, codec]]

type PAGE = Page[string, string]
type LOG struct{}

var _ List[string, string, evvee] = (*LOG)(nil)

func (*LOG) First(n uint64, after string) ([]PAGE, error) { panic("N/A") }
func (*LOG) Last(n uint64, before string) ([]PAGE, error) { panic("N/A") }

type SUB struct{}

var _ evvesub = (*SUB)(nil)

func (*SUB) Recv() error      { return nil }
func (*SUB) Events() []evvent { return nil }
func (*SUB) Close()           {}

type EV struct{}

var _ evvee = (*EV)(nil)

func (*EV) Emit(topic string, event evvent) error              { panic("N/A") }
func (*EV) EventLog(topic string) List[string, uint64, evvent] { panic("N/A") }
func (*EV) Subscribe(topic string, after uint64) evvesub       { panic("N/A") }
func (*EV) Load(topic string, a aggr) error                    { panic("N/A") }
func (*EV) Store(a aggr) error                                 { panic("N/A") }
func (*EV) Truncate(topic string) error                        { panic("N/A") }
