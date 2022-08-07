package gql_ev

import "github.com/sour-is/ev/pkg/es/event"

type Edge interface {
	IsEdge()
}

type Connection struct {
	Paging *PageInfo `json:"paging"`
	Edges  []Edge    `json:"edges"`
}

type Event struct {
	ID      string      `json:"id"`
	Payload string      `json:"payload"`
	Tags    []string    `json:"tags"`
	Meta    *event.Meta `json:"meta"`
}

func (Event) IsEdge() {}

type PageInfo struct {
	Next  bool   `json:"next"`
	Prev  bool   `json:"prev"`
	Begin uint64 `json:"begin"`
	End   uint64 `json:"end"`
}

type PageInput struct {
	Idx   *int64 `json:"idx"`
	Count *int64 `json:"count"`
}

func (p *PageInput) GetIdx(v int64) int64 {
	if p == nil || p.Idx == nil {
		return v
	}
	return *p.Idx
}
func (p *PageInput) GetCount(v int64) int64 {
	if p == nil || p.Count == nil {
		return v
	}
	return *p.Count
}
