// Code generated by github.com/99designs/gqlgen, DO NOT EDIT.

package model

import (
	"github.com/sour-is/ev/pkg/es/event"
)

type Event struct {
	ID        string                 `json:"id"`
	EventID   string                 `json:"eventID"`
	Values    map[string]interface{} `json:"values"`
	EventMeta *event.Meta            `json:"eventMeta"`
}

func (Event) IsEdge() {}
