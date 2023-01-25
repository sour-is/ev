package webfinger

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"sort"

	"github.com/sour-is/ev/pkg/es/event"
	"github.com/sour-is/ev/pkg/set"
	"github.com/sour-is/ev/pkg/slice"
	"gopkg.in/yaml.v3"
)

func StreamID(subject string) string {
	h := fnv.New128a()
	h.Write([]byte(subject))
	return "webfinger." + base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// JRD is a JSON Resource Descriptor, specifying properties and related links
// for a resource.
type JRD struct {
	Subject    string             `json:"subject,omitempty" yaml:"subject,omitempty"`
	Aliases    []string           `json:"aliases,omitempty" yaml:"aliases,omitempty"`
	Properties map[string]*string `json:"properties,omitempty" yaml:"properties,omitempty"`
	Links      Links              `json:"links,omitempty" yaml:"links,omitempty"`

	deleted             bool
	event.AggregateRoot `yaml:"-"`
}

func (a *JRD) CloneValues() *JRD {
	m := make(map[string]*string, len(a.Properties))
	for k,v := range a.Properties {
		m[k] = v
	}
	return &JRD{
		Subject:       a.Subject,
		Aliases:       append([]string{}, a.Aliases...),
		Properties:    m,
		Links:         append([]*Link{}, a.Links...),
	}
}

var _ event.Aggregate = (*JRD)(nil)

// Link is a link to a related resource.
type Link struct {
	Rel        string             `json:"rel,omitempty"`
	Type       string             `json:"type,omitempty"`
	HRef       string             `json:"href,omitempty"`
	Titles     map[string]string  `json:"titles,omitempty"`
	Properties map[string]*string `json:"properties,omitempty"`
}

type Links []*Link

// Len is the number of elements in the collection.
func (l Links) Len() int {
	if l == nil {
		return 0
	}
	return len(l)
}

// Less reports whether the element with index i
func (l Links) Less(i int, j int) bool {
	if l[i] == nil || l[j] == nil {
		return false
	}
	return l[i].Rel < l[j].Rel
}

// Swap swaps the elements with indexes i and j.
func (l Links) Swap(i int, j int) {
	if l == nil {
		return
	}
	l[i], l[j] = l[j], l[i]
}

// ParseJRD parses the JRD using json.Unmarshal.
func ParseJRD(blob []byte) (*JRD, error) {
	jrd := JRD{}
	err := json.Unmarshal(blob, &jrd)
	if err != nil {
		return nil, err
	}
	return &jrd, nil
}

// GetLinkByRel returns the first *Link with the specified rel value.
func (jrd *JRD) GetLinkByRel(rel string) *Link {
	for _, link := range jrd.Links {
		if link.Rel == rel {
			return link
		}
	}
	return nil
}

// GetLinksByRel returns the first *Link with the specified rel value.
func (jrd *JRD) GetLinksByRel(rel ...string) []*Link {
	var lis []*Link
	rels := set.New(rel...)

	for _, link := range jrd.Links {
		if rels.Has(link.Rel) {
			lis = append(lis, link)
		}
	}
	return lis
}

// GetProperty Returns the property value as a string.
// Per spec a property value can be null, empty string is returned in this case.
func (jrd *JRD) GetProperty(uri string) string {
	if jrd.Properties[uri] == nil {
		return ""
	}
	return *jrd.Properties[uri]
}
func (a *JRD) SetProperty(name string, value *string) {
	if a.Properties == nil {
		a.Properties = make(map[string]*string)
	}
	a.Properties[name] = value
}
func (a *JRD) DeleteProperty(name string) {
	if a.Properties == nil {
		return
	}
	delete(a.Properties, name)
}
func (a *JRD) IsDeleted() bool {
	return a.deleted
}

// GetProperty Returns the property value as a string.
// Per spec a property value can be null, empty string is returned in this case.
func (link *Link) GetProperty(uri string) string {
	if link.Properties[uri] == nil {
		return ""
	}
	return *link.Properties[uri]
}
func (link *Link) SetProperty(name string, value *string) {
	if link.Properties == nil {
		link.Properties = make(map[string]*string)
	}
	link.Properties[name] = value
}
func (link *Link) DeleteProperty(name string) {
	if link.Properties == nil {
		return
	}
	delete(link.Properties, name)
}

// ApplyEvent implements event.Aggregate
func (a *JRD) ApplyEvent(events ...event.Event) {
	for _, e := range events {
		switch e := e.(type) {
		case *SubjectSet:
			a.deleted = false

			a.Subject = e.Subject
			a.Aliases = e.Aliases
			a.Properties = e.Properties

		case *SubjectDeleted:
			a.deleted = true

			a.Subject = e.Subject
			a.Aliases = a.Aliases[:0]
			a.Links = a.Links[:0]
			a.Properties = map[string]*string{}

		case *LinkSet:
			link, ok := slice.FindFn(func(l *Link) bool { return l.Rel == e.Rel }, a.Links...)
			if !ok {
				link = &Link{}
				link.Rel = e.Rel
				a.Links = append(a.Links, link)
			}

			link.HRef = e.HRef
			link.Type = e.Type
			link.Titles = e.Titles
			link.Properties = e.Properties

		case *LinkDeleted:
			a.Links = slice.FilterFn(func(link *Link) bool { return link.Rel != e.Rel }, a.Links...)
		}
	}
}

const NSauth = "https://sour.is/ns/auth"
const NSpubkey = "https://sour.is/ns/pubkey"
const NSredirect = "https://sour.is/rel/redirect"

func (a *JRD) OnAuth(claim, auth *JRD) error {
	pubkey := claim.Properties[NSpubkey]

	if v, ok := auth.Properties[NSpubkey]; ok && v != nil && cmpPtr(v, pubkey) {
		// pubkey matches!
	} else {
		return fmt.Errorf("pubkey does not match")
	}

	if a.Version() > 0 && !a.IsDeleted() && a.Subject != claim.Subject {
		return fmt.Errorf("subject does not match")
	}

	if auth.Subject == claim.Subject {
		claim.SetProperty(NSpubkey, pubkey)
	} else {
		claim.SetProperty(NSauth, &auth.Subject)
		claim.DeleteProperty(NSpubkey)
	}

	return nil
}

func (a *JRD) OnDelete(jrd *JRD) error {
	if a.Version() == 0 || a.IsDeleted() {
		return nil
	}

	event.Raise(a, &SubjectDeleted{Subject: jrd.Subject})
	return nil
}

func (a *JRD) OnClaims(jrd *JRD) error {

	err := a.OnSubjectSet(jrd.Subject, jrd.Aliases, jrd.Properties)
	if err != nil {
		return err
	}

	sort.Sort(jrd.Links)
	sort.Sort(a.Links)
	for _, z := range slice.Align(
		jrd.Links,
		a.Links,
		func(l, r *Link) bool { return l.Rel < r.Rel },
	) {
		// Not in new == delete
		if z.Key == nil {
			link := *z.Value
			event.Raise(a, &LinkDeleted{Rel: link.Rel})
			continue
		}

		// Not in old == create
		if z.Value == nil {
			link := *z.Key
			event.Raise(a, &LinkSet{
				Rel:        link.Rel,
				Type:       link.Type,
				HRef:       link.HRef,
				Titles:     link.Titles,
				Properties: link.Properties,
			})
			continue
		}

		// in both == compare
		a.OnLinkSet(*z.Key, *z.Value)
	}

	return nil
}

func (a *JRD) OnSubjectSet(subject string, aliases []string, props map[string]*string) error {
	modified := false
	e := &SubjectSet{
		Subject:    subject,
		Aliases:    aliases,
		Properties: props,
	}

	if subject != a.Subject {
		modified = true
	}

	sort.Strings(aliases)
	sort.Strings(a.Aliases)
	for _, z := range slice.Zip(aliases, a.Aliases) {
		if z.Key != z.Value {
			modified = true
			break
		}
	}

	for _, z := range slice.Zip(
		slice.Zip(slice.FromMap(props)),
		slice.Zip(slice.FromMap(a.Properties)),
	) {
		newValue := z.Key
		curValue := z.Value

		if newValue.Key != curValue.Key {
			modified = true
			break
		}

		if !cmpPtr(newValue.Value, curValue.Value) {
			modified = true
			break
		}
	}

	if modified {
		event.Raise(a, e)
	}

	return nil
}

func (a *JRD) OnLinkSet(o, n *Link) error {
	modified := false
	e := &LinkSet{
		Rel:        n.Rel,
		Type:       n.Type,
		HRef:       n.HRef,
		Titles:     n.Titles,
		Properties: n.Properties,
	}

	if n.Rel != o.Rel {
		modified = true
	}
	if n.Type != o.Type {
		modified = true
	}
	if n.HRef != o.HRef {
		modified = true
	}

	for _, z := range slice.Zip(
		slice.Zip(slice.FromMap(n.Titles)),
		slice.Zip(slice.FromMap(o.Titles)),
	) {
		if z.Key != z.Value {
			modified = true
			break
		}
	}

	for _, z := range slice.Zip(
		slice.Zip(slice.FromMap(n.Properties)),
		slice.Zip(slice.FromMap(o.Properties)),
	) {
		newValue := z.Key
		curValue := z.Value

		if newValue.Key != curValue.Key {
			modified = true
			break
		}

		if !cmpPtr(newValue.Value, curValue.Value) {
			modified = true
			break
		}
	}

	if modified {
		event.Raise(a, e)
	}

	return nil
}

func cmpPtr[T comparable](l, r *T) bool {
	if l == nil {
		return r == nil
	}

	if r == nil {
		return l == nil
	}

	return *l == *r
}

func (a *JRD) String() string {
	b := &bytes.Buffer{}
	y := yaml.NewEncoder(b)
	_ = y.Encode(a)

	return b.String()
}
