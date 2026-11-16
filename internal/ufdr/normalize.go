// Package ufdr supports only the synthetic report contract documented in this repository.
package ufdr

import (
	"encoding/xml"
	"fmt"
)

type Party struct {
	ID   string  `json:"id"`
	Name *string `json:"name"`
}
type Message struct {
	ID          string  `json:"id"`
	SourceIndex string  `json:"source_index"`
	ChatID      string  `json:"chat_id"`
	Body        *string `json:"body"`
	Timestamp   *string `json:"timestamp"`
	From        *Party  `json:"from"`
	To          *Party  `json:"to"`
}
type Result struct {
	Messages     []Message `json:"messages"`
	Participants []Party   `json:"participants"`
	Notes        []string  `json:"notes"`
}
type field struct {
	Name  string `xml:"name,attr"`
	Text  string `xml:",chardata"`
	Value struct {
		Timestamp string `xml:"formattedTimestamp,attr"`
	} `xml:"value"`
}
type model struct {
	Type   string  `xml:"type,attr"`
	ID     string  `xml:"id,attr"`
	Index  string  `xml:"source_index,attr"`
	Fields []field `xml:"field"`
	Multi  []group `xml:"multiModelField"`
	Single []group `xml:"modelField"`
}
type group struct {
	Name   string  `xml:"name,attr"`
	Models []model `xml:"model"`
}

func party(m model) *Party {
	p := &Party{ID: m.ID}
	for _, f := range m.Fields {
		if f.Name == "Name" {
			s := f.Text
			p.Name = &s
		}
	}
	return p
}
func Normalize(data []byte) (Result, error) {
	var doc struct {
		XMLName xml.Name
		Models  []model `xml:"decodedData>model"`
	}
	r := Result{Messages: []Message{}, Participants: []Party{}, Notes: []string{"All identifiers and message content are fictional.", "source_index is preserved as a report reference; it is not asserted to be a byte offset.", "The source XML contains no To field, so the expected recipient is null."}}
	if err := xml.Unmarshal(data, &doc); err != nil {
		return r, err
	}
	if doc.XMLName.Local != "project" || doc.XMLName.Space != "http://pa.cellebrite.com/report/2.0" {
		return r, fmt.Errorf("unsupported report root")
	}
	for _, chat := range doc.Models {
		if chat.Type != "Chat" {
			continue
		}
		for _, g := range chat.Multi {
			for _, m := range g.Models {
				if g.Name == "Participants" && m.Type == "Party" {
					r.Participants = append(r.Participants, *party(m))
				}
				if g.Name != "Messages" || m.Type != "InstantMessage" {
					continue
				}
				v := Message{ID: m.ID, SourceIndex: m.Index, ChatID: chat.ID}
				for _, f := range m.Fields {
					s := f.Text
					if f.Name == "Body" {
						v.Body = &s
					}
					if f.Name == "TimeStamp" && f.Value.Timestamp != "" {
						s = f.Value.Timestamp
						v.Timestamp = &s
					}
				}
				for _, s := range m.Single {
					if len(s.Models) > 0 {
						if s.Name == "From" {
							v.From = party(s.Models[0])
						}
						if s.Name == "To" {
							v.To = party(s.Models[0])
						}
					}
				}
				r.Messages = append(r.Messages, v)
			}
		}
	}
	// Notes describe the checked-in synthetic contract; reject recipient variants until their notes contract is defined.
	for _, m := range r.Messages {
		if m.To != nil {
			return r, fmt.Errorf("To records are outside this PoC contract")
		}
	}
	return r, nil
}
