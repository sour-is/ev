package event

import (
	"bytes"
	"context"
	"encoding"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"strings"

	"go.sour.is/pkg/lg"
	"go.sour.is/pkg/locker"
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

	IsEvent
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

	e := &UnknownEvent{eventType: eventType, values: jsonValues}
	e.SetEventMeta(meta)
	return e
}
func NewUnknownEventFromRaw(eventType string, meta Meta, values map[string]json.RawMessage) *UnknownEvent {
	e := &UnknownEvent{eventType: eventType, values: values}
	e.SetEventMeta(meta)
	return e
}
func (u UnknownEvent) EventType() string { return u.eventType }
func (u *UnknownEvent) UnmarshalBinary(b []byte) error {
	return json.Unmarshal(b, &u.values)
}
func (u *UnknownEvent) MarshalBinary() ([]byte, error) {
	return json.Marshal(u.values)
}

// Register a type container for Unmarshalling values into. The type must implement Event and not be a nil value.
func Register(ctx context.Context, lis ...Event) error {
	ctx, span := lg.Span(ctx)
	defer span.End()

	for _, e := range lis {
		if err := ctx.Err(); err != nil {
			span.RecordError(err)
			return err
		}
		name := TypeOf(e)
		err := RegisterName(ctx, name, e)
		if err != nil {
			return err
		}
	}
	return nil
}
func RegisterName(ctx context.Context, name string, e Event) error {
	ctx, span := lg.Span(ctx)
	defer span.End()

	if e == nil {
		err := fmt.Errorf("can't register event.Event of type=%T with value=%v", e, e)
		span.RecordError(err)
		return err
	}

	value := reflect.ValueOf(e)

	if value.IsNil() {
		err := fmt.Errorf("can't register event.Event of type=%T with value=%v", e, e)
		span.RecordError(err)
		return err
	}
	value = reflect.Indirect(value)

	typ := value.Type()

	span.AddEvent("register: " + name)

	if err := eventTypes.Use(ctx, func(ctx context.Context, c *config) error {
		_, span := lg.Span(ctx)
		defer span.End()

		c.eventTypes[name] = typ
		return nil
	}); err != nil {
		span.RecordError(err)
		return err
	}
	return nil
}
func GetContainer(ctx context.Context, s string) Event {
	ctx, span := lg.Span(ctx)
	defer span.End()

	var e Event

	eventTypes.Use(ctx, func(ctx context.Context, c *config) error {
		_, span := lg.Span(ctx)
		defer span.End()

		typ, ok := c.eventTypes[s]
		if !ok {
			err := fmt.Errorf("not defined: %s", s)
			span.RecordError(err)
			return err
		}
		newType := reflect.New(typ)
		newInterface := newType.Interface()
		if iface, ok := newInterface.(Event); ok {
			e = iface
			return nil
		}
		err := fmt.Errorf("failed")
		span.RecordError(err)
		return err
	})
	if e == nil {
		e = &UnknownEvent{eventType: s}
	}

	return e
}

func MarshalBinary(e Event) ([]byte, error) {
	var err error
	b := &bytes.Buffer{}

	m := e.EventMeta()
	if _, err = b.WriteString(m.EventID.String()); err != nil {
		return nil, err
	}
	b.WriteRune('\t')
	if _, err = b.WriteString(m.StreamID); err != nil {
		return nil, err
	}
	b.WriteRune('\t')
	if _, err = b.WriteString(TypeOf(e)); err != nil {
		return nil, err
	}
	b.WriteRune('\t')
	switch e := e.(type) {
	case encoding.BinaryMarshaler:
		var txt []byte
		if txt, err = e.MarshalBinary(); err != nil {
			return nil, err
		}
		_, err = b.Write(txt)
	case encoding.TextMarshaler:
		var txt []byte
		if txt, err = e.MarshalText(); err != nil {
			return nil, err
		}
		_, err = b.Write(txt)
	default:
		err = json.NewEncoder(b).Encode(e)
	}

	return b.Bytes(), err
}

func UnmarshalBinary(ctx context.Context, txt []byte, pos uint64) (e Event, err error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	sp := bytes.SplitN(txt, []byte{'\t'}, 4)
	if len(sp) != 4 {
		err = fmt.Errorf("invalid format. expected=4, got=%d", len(sp))
		span.RecordError(err)
		return nil, err
	}

	m := Meta{}
	if err = m.EventID.UnmarshalText(sp[0]); err != nil {
		span.RecordError(err)
		return nil, err
	}

	m.StreamID = string(sp[1])
	m.Position = pos
	m.ActualStreamID = string(sp[1])
	m.ActualPosition = pos

	eventType := string(sp[2])
	e = GetContainer(ctx, eventType)
	span.AddEvent(fmt.Sprintf("%s == %T", eventType, e))
	switch e := e.(type) {
	case encoding.BinaryUnmarshaler:
		if err = e.UnmarshalBinary(sp[3]); err != nil {
			span.RecordError(err)
			return nil, err
		}
	case encoding.TextUnmarshaler:
		if err = e.UnmarshalText(sp[3]); err != nil {
			span.RecordError(err)
			return nil, err
		}
	default:
		if err = json.Unmarshal(sp[3], e); err != nil {
			span.RecordError(err)
			return nil, err
		}
	}
	e.SetEventMeta(m)

	return e, nil
}

// DecodeEvents unmarshals the byte list into Events.
func DecodeEvents(ctx context.Context, lis ...[]byte) (Events, error) {
	elis := make([]Event, len(lis))

	var err error
	for i, txt := range lis {
		elis[i], err = UnmarshalBinary(ctx, txt, uint64(i))
		if err != nil {
			return nil, err
		}
	}

	return elis, nil
}

func EncodeEvents(events ...Event) (lis [][]byte, err error) {
	lis = make([][]byte, len(events))

	for i, txt := range events {
		lis[i], err = MarshalBinary(txt)
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

func Values(e Event) map[string]any {
	var a any = e

	if e, ok := e.(interface{ Values() any }); ok {
		a = e.Values()
	}

	m := make(map[string]any)
	v := reflect.Indirect(reflect.ValueOf(a))
	for _, idx := range reflect.VisibleFields(v.Type()) {
		if !idx.IsExported() {
			continue
		}

		omitempty := false
		field := v.FieldByIndex(idx.Index)

		name := idx.Name
		if n, ok := idx.Tag.Lookup("json"); ok {
			var (
				opt   string
				found bool
			)

			name, opt, found = strings.Cut(n, ",")
			if name == "-" {
				continue
			}

			if found {
				if strings.Contains(opt, "omitempty") {
					omitempty = true
				}
			}
		}

		if omitempty && field.IsZero() {
			continue
		}

		m[name] = field.Interface()
	}
	return m
}
