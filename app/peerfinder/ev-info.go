package peerfinder

import (
	"bytes"

	"github.com/tj/go-semver"

	"go.sour.is/ev/pkg/event"
)

type Info struct {
	ScriptVersion string `json:"script_version"`

	event.IsAggregate
}

var _ event.Aggregate = (*Info)(nil)

func (a *Info) ApplyEvent(lis ...event.Event) {
	for _, e := range lis {
		switch e := e.(type) {
		case *VersionChanged:
			a.ScriptVersion = e.ScriptVersion
		}
	}
}
func (a *Info) MarshalEnviron() ([]byte, error) {
	var b bytes.Buffer

	b.WriteString("SCRIPT_VERSION=")
	b.WriteString(a.ScriptVersion)
	b.WriteRune('\n')

	return b.Bytes(), nil
}
func (a *Info) OnUpsert() error {
	if a.StreamVersion() == 0 {
		event.Raise(a, &VersionChanged{ScriptVersion: initVersion})
	}
	current, _ := semver.Parse(initVersion)
	previous, _ := semver.Parse(a.ScriptVersion)

	if current.Compare(previous) > 0 {
		event.Raise(a, &VersionChanged{ScriptVersion: initVersion})
	}

	return nil
}

type VersionChanged struct {
	ScriptVersion string `json:"script_version"`

	event.IsEvent
}

var _ event.Event = (*VersionChanged)(nil)
