package gql

import (
	"context"
	"encoding/json"

	"go.sour.is/ev/pkg/es/event"
)

type Edge interface {
	IsEdge()
}

type Connection struct {
	Paging *PageInfo `json:"paging"`
	Edges  []Edge    `json:"edges"`
}

type PostEvent struct {
	ID      string      `json:"id"`
	Payload string      `json:"payload"`
	Tags    []string    `json:"tags"`
	Meta    *event.Meta `json:"meta"`
}

func (PostEvent) IsEdge() {}

func (e *PostEvent) PayloadJSON(ctx context.Context) (m map[string]interface{}, err error) {
	err = json.Unmarshal([]byte(e.Payload), &m)
	return
}

type PageInfo struct {
	Next  bool   `json:"next"`
	Prev  bool   `json:"prev"`
	Begin uint64 `json:"begin"`
	End   uint64 `json:"end"`
}

type PageInput struct {
	After  *int64 `json:"after"`
	Before *int64 `json:"before"`
	Count  *int64 `json:"count"`
}

func (p *PageInput) GetIdx(v int64) int64 {
	if p == nil {
		// pass
	} else if p.Before != nil {
		return (*p.Before)
	} else if p.After != nil {
		return *p.After
	}

	return v
}
func (p *PageInput) GetCount(v int64) int64 {
	if p == nil || p.Count == nil {
		return v
	} else if p.Before != nil {
		return -(*p.Count)
	} else if p.After != nil {
		return *p.Count
	}

	return *p.Count
}
