package webfinger

import (
	"go.sour.is/ev/pkg/es/event"
)

type SubjectSet struct {
	Subject    string             `json:"subject"`
	Aliases    []string           `json:"aliases,omitempty"`
	Properties map[string]*string `json:"properties,omitempty"`

	event.IsEvent `json:"-"`
}

type SubjectDeleted struct {
	Subject string `json:"subject"`

	event.IsEvent `json:"-"`
}

var _ event.Event = (*SubjectDeleted)(nil)

type LinkSet struct {
	Index      uint64             `json:"idx"`
	Rel        string             `json:"rel"`
	Type       string             `json:"type,omitempty"`
	HRef       string             `json:"href,omitempty"`
	Titles     map[string]string  `json:"titles,omitempty"`
	Properties map[string]*string `json:"properties,omitempty"`
	Template   string             `json:"template,omitempty"`

	event.IsEvent `json:"-"`
}

var _ event.Event = (*LinkSet)(nil)

type LinkDeleted struct {
	Index uint64 `json:"idx"`
	Rel   string `json:"rel"`

	event.IsEvent `json:"-"`
}

var _ event.Event = (*LinkDeleted)(nil)
