package event

import (
	"bytes"
	"encoding"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"reflect"
	"strings"
	"sync"
)

var (
	eventTypes sync.Map
)

type UnknownEvent struct {
	eventType string
	values    map[string]json.RawMessage

	eventMeta Meta
}

var _ Event = (*UnknownEvent)(nil)
var _ json.Marshaler = (*UnknownEvent)(nil)
var _ json.Unmarshaler = (*UnknownEvent)(nil)

func NewUnknownEventFromValues(eventType string, meta Meta, values url.Values) *UnknownEvent {
	jsonValues := make(map[string]json.RawMessage, len(values))
	for k, v := range values {
		switch len(v) {
		case 0:
			jsonValues[k] = []byte("null")
		case 1:
			jsonValues[k] = embedJSON(v[0])
		default:
			parts := make([][]byte, len(v))
			for i := range v {
				parts[i] = embedJSON(v[i])
			}
			jsonValues[k] = append([]byte("["), bytes.Join(parts, []byte(","))...)
			jsonValues[k] = append(jsonValues[k], ']')
		}
	}

	return &UnknownEvent{eventType: eventType, eventMeta: meta, values: jsonValues}
}
func NewUnknownEventFromRaw(eventType string, meta Meta, values map[string]json.RawMessage) *UnknownEvent {
	return &UnknownEvent{eventType: eventType, eventMeta: meta, values: values}
}
func (u UnknownEvent) EventMeta() Meta   { return u.eventMeta }
func (u UnknownEvent) EventType() string { return u.eventType }
func (u *UnknownEvent) SetEventMeta(em Meta) {
	u.eventMeta = em
}
func (u *UnknownEvent) UnmarshalJSON(b []byte) error {
	return json.Unmarshal(b, &u.values)
}
func (u *UnknownEvent) MarshalJSON() ([]byte, error) {
	return json.Marshal(u.values)
}

// Register a type container for Unmarshalling values into. The type must implement Event and not be a nil value.
func Register(lis ...Event) {
	for _, e := range lis {
		if e == nil {
			panic(fmt.Sprintf("can't register event.Event of type=%T with value=%v", e, e))
		}

		value := reflect.ValueOf(e)

		if value.IsNil() {
			panic(fmt.Sprintf("can't register event.Event of type=%T with value=%v", e, e))
		}

		value = reflect.Indirect(value)
		typ := value.Type()

		eventTypes.LoadOrStore(TypeOf(e), typ)
	}
}
func GetContainer(s string) Event {
	if typ, ok := eventTypes.Load(s); ok {
		if typ, ok := typ.(reflect.Type); ok {
			newType := reflect.New(typ)
			newInterface := newType.Interface()
			if typ, ok := newInterface.(Event); ok {
				return typ
			}
		}
	}
	return &UnknownEvent{eventType: s}
}

func MarshalText(e Event) (txt []byte, err error) {
	b := &bytes.Buffer{}

	if _, err = writeMarshaler(b, e.EventMeta().EventID); err != nil {
		return nil, err
	}
	b.WriteRune('\t')
	if _, err = b.WriteString(e.EventMeta().StreamID); err != nil {
		return nil, err
	}
	b.WriteRune('\t')
	if _, err = b.WriteString(TypeOf(e)); err != nil {
		return nil, err
	}
	b.WriteRune('\t')
	if enc, ok := e.(encoding.TextMarshaler); ok {
		if txt, err = enc.MarshalText(); err != nil {
			return nil, err
		}
	} else {
		if txt, err = json.Marshal(e); err != nil {
			return nil, err
		}
	}
	_, err = b.Write(txt)

	return b.Bytes(), err
}

func UnmarshalText(txt []byte, pos uint64) (e Event, err error) {
	sp := bytes.SplitN(txt, []byte{'\t'}, 4)
	if len(sp) != 4 {
		return nil, fmt.Errorf("invalid format. expected=4, got=%d", len(sp))
	}

	m := Meta{}
	if err = m.EventID.UnmarshalText(sp[0]); err != nil {
		return nil, err
	}

	m.StreamID = string(sp[1])
	m.Position = pos

	eventType := string(sp[2])
	e = GetContainer(eventType)

	if enc, ok := e.(encoding.TextUnmarshaler); ok {
		if err = enc.UnmarshalText(sp[3]); err != nil {
			return nil, err
		}
	} else {
		if err = json.Unmarshal(sp[3], e); err != nil {
			return nil, err
		}
	}
	e.SetEventMeta(m)

	return e, nil
}

func writeMarshaler(out io.Writer, in encoding.TextMarshaler) (int, error) {
	if b, err := in.MarshalText(); err != nil {
		return 0, err
	} else {
		return out.Write(b)
	}
}

// DecodeEvents unmarshals the byte list into Events.
func DecodeEvents(lis ...[]byte) (Events, error) {
	elis := make([]Event, len(lis))

	var err error
	for i, txt := range lis {
		elis[i], err = UnmarshalText(txt, uint64(i))
		if err != nil {
			return nil, err
		}
	}

	return elis, nil
}

func EncodeEvents(events ...Event) (lis [][]byte, err error) {
	lis = make([][]byte, len(events))

	for i, txt := range events {
		lis[i], err = MarshalText(txt)
		if err != nil {
			return nil, err
		}
	}

	return lis, nil
}

func embedJSON(s string) json.RawMessage {
	if len(s) > 1 && s[0] == '{' && s[len(s)-1] == '}' {
		return []byte(s)
	}
	if len(s) > 1 && s[0] == '[' && s[len(s)-1] == ']' {
		return []byte(s)
	}
	return []byte(fmt.Sprintf(`"%s"`, strings.Replace(s, `"`, `\"`, -1)))
}
