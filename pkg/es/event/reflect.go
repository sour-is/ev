package event

import (
	"bytes"
	"context"
	"encoding"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"reflect"
	"strings"

	"github.com/sour-is/ev/pkg/locker"
)

type config struct {
	eventTypes map[string]reflect.Type
}

var (
	eventTypes = locker.New(&config{eventTypes: make(map[string]reflect.Type)})
)

type UnknownEvent struct {
	eventType string
	values    map[string]json.RawMessage

	eventMeta Meta
}

var _ Event = (*UnknownEvent)(nil)

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
func (u *UnknownEvent) UnmarshalText(b []byte) error {
	return json.Unmarshal(b, &u.values)
}
func (u *UnknownEvent) MarshalText() ([]byte, error) {
	return json.Marshal(u.values)
}

// Register a type container for Unmarshalling values into. The type must implement Event and not be a nil value.
func Register(ctx context.Context, lis ...Event) error {
	for _, e := range lis {
		if err := ctx.Err(); err != nil {
			return err
		}
		if e == nil {
			return fmt.Errorf("can't register event.Event of type=%T with value=%v", e, e)
		}

		value := reflect.ValueOf(e)

		if value.IsNil() {
			return fmt.Errorf("can't register event.Event of type=%T with value=%v", e, e)
		}

		value = reflect.Indirect(value)

		name := TypeOf(e)
		typ := value.Type()

		if err := eventTypes.Modify(ctx, func(c *config) error {
			c.eventTypes[name] = typ
			return nil
		}); err != nil {
			return err
		}
	}
	return nil
}
func GetContainer(ctx context.Context, s string) Event {
	var e Event

	eventTypes.Modify(ctx, func(c *config) error {
		typ, ok := c.eventTypes[s]
		if !ok {
			return fmt.Errorf("not defined")
		}
		newType := reflect.New(typ)
		newInterface := newType.Interface()
		if iface, ok := newInterface.(Event); ok {
			e = iface
			return nil
		}
		return fmt.Errorf("failed")
	})
	if e == nil {
		e = &UnknownEvent{eventType: s}
	}

	return e
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
	if txt, err = e.MarshalText(); err != nil {
		return nil, err
	}
	_, err = b.Write(txt)

	return b.Bytes(), err
}

func UnmarshalText(ctx context.Context, txt []byte, pos uint64) (e Event, err error) {
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
	e = GetContainer(ctx, eventType)

	if err = e.UnmarshalText(sp[3]); err != nil {
		return nil, err
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
func DecodeEvents(ctx context.Context, lis ...[]byte) (Events, error) {
	elis := make([]Event, len(lis))

	var err error
	for i, txt := range lis {
		elis[i], err = UnmarshalText(ctx, txt, uint64(i))
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
