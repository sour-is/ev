package webfinger

import (
	"go.sour.is/ev/pkg/es/event"
)

type SubjectSet struct {
	Subject    string             `json:"subject"`
	Aliases    []string           `json:"aliases,omitempty"`
	Properties map[string]*string `json:"properties,omitempty"`

	event.IsEvent
}

var _ event.Event = (*SubjectSet)(nil)

type SubjectDeleted struct {
	Subject string `json:"subject"`

	event.IsEvent
}

var _ event.Event = (*SubjectDeleted)(nil)

type LinkSet struct {
	Rel        string             `json:"rel"`
	Type       string             `json:"type,omitempty"`
	HRef       string             `json:"href,omitempty"`
	Titles     map[string]string  `json:"titles,omitempty"`
	Properties map[string]*string `json:"properties,omitempty"`

	event.IsEvent
}

var _ event.Event = (*LinkSet)(nil)

type LinkDeleted struct {
	Rel string `json:"rel"`

	event.IsEvent
}

var _ event.Event = (*LinkDeleted)(nil)
