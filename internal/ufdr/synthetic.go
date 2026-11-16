package ufdr

import (
	"fmt"
	"io"
	"strings"
)

const syntheticTimestamp = "2025-01-15T12:00:00+00:00"

func SyntheticBody(index int) string {
	return fmt.Sprintf("Synthetic message %04d. ", index) +
		strings.Repeat("The training team catalogued a fictional item in the simulation. ", 48)
}

// WriteSynthetic creates a byte-reproducible report with the supported Chat and
// InstantMessage structure. Message indexes start at one.
func WriteSynthetic(w io.Writer, count int) error {
	if count < 1 || count > 10000 {
		return fmt.Errorf("message count must be between 1 and 10000")
	}
	if _, err := io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<project id="synthetic-project" xmlns="http://pa.cellebrite.com/report/2.0">
<decodedData><model type="Chat" id="synthetic-chat" source_index="10">
<multiModelField name="Participants" type="Party">
<model type="Party" id="synthetic-party" source_index="11"><field name="Name">Example Sender</field></model>
</multiModelField>
<multiModelField name="Messages" type="InstantMessage">
`); err != nil {
		return err
	}
	for index := 1; index <= count; index++ {
		if _, err := fmt.Fprintf(w, `<model type="InstantMessage" id="synthetic-message-%04d" source_index="%d">
<modelField name="From" type="Party"><model type="Party" id="synthetic-party" source_index="11"><field name="Name">Example Sender</field></model></modelField>
<field name="Body" type="String">%s</field>
<field name="TimeStamp" type="TimeStamp"><value formattedTimestamp="%s"/></field>
</model>
`, index, index+11, SyntheticBody(index), syntheticTimestamp); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, "</multiModelField></model></decodedData></project>\n")
	return err
}
