package peerfinder

import (
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/keys-pub/keys/json"
	"github.com/sour-is/ev/pkg/es/event"
	"github.com/sour-is/ev/pkg/set"
)

type Time time.Time

func (t *Time) UnmarshalJSON(b []byte) error {
	time, err := time.Parse(`"2006-01-02 15:04:05"`, string(b))
	*t = Time(time)
	return err
}
func (t *Time) MarshalJSON() ([]byte, error) {
	if t == nil {
		return nil, nil
	}
	i := *t
	return time.Time(i).MarshalJSON()
}

type ipFamily string

const (
	ipFamilyV4   ipFamily = "IPv4"
	ipFamilyV6   ipFamily = "IPv6"
	ipFamilyBoth ipFamily = "both"
	ipFamilyNone ipFamily = "none"
)

func (t *ipFamily) UnmarshalJSON(b []byte) error {
	i, err := strconv.Atoi(strings.Trim(string(b), `"`))
	switch i {
	case 1:
		*t = ipFamilyV4
	case 2:
		*t = ipFamilyV6
	case 3:
		*t = ipFamilyBoth
	default:
		*t = ipFamilyNone
	}
	return err
}

type peerType []string

func (t *peerType) UnmarshalJSON(b []byte) error {
	var bs string
	json.Unmarshal(b, &bs)
	*t = strings.Split(bs, ",")
	return nil
}

type Peer struct {
	ID      string   `json:"peer_id,omitempty"`
	Owner   string   `json:"peer_owner"`
	Nick    string   `json:"peer_nick"`
	Name    string   `json:"peer_name"`
	Country string   `json:"peer_country"`
	Note    string   `json:"peer_note"`
	Family  ipFamily `json:"peer_family"`
	Type    peerType `json:"peer_type"`
	Created Time     `json:"peer_created"`
}

func (p *Peer) CanSupport(ip string) bool {
	addr := net.ParseIP(ip)
	if addr == nil {
		return false
	}
	if !(addr.IsGlobalUnicast() || addr.IsLoopback() || addr.IsPrivate()) {
		return false
	}

	switch p.Family {
	case ipFamilyV4:
		return addr.To4() != nil
	case ipFamilyV6:
		return addr.To16() != nil
	case ipFamilyNone:
		return false
	}

	return true
}

type PeerResults struct {
	set.Set[string]
	event.AggregateRoot
}

func (p *PeerResults) ApplyEvent(lis ...event.Event) {
	for _, e := range lis {
		switch e := e.(type) {
		case *ResultSubmitted:
			if p.Set == nil {
				p.Set = set.New[string]()
			}
			p.Set.Add(e.RequestID)
		}
	}
}

