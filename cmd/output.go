package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/mattt/things-cli/internal/domain"
)

func writeJSON(w io.Writer, value any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(value)
}

// safe removes ANSI sequences and control characters from terminal display.
func safe(v string) string {
	var b strings.Builder
	state := 0
	for _, r := range v {
		switch state {
		case 1:
			if r == '[' {
				state = 2
			} else if r == ']' || r == 'P' || r == '^' || r == '_' {
				state = 3
			} else {
				state = 0
			}
			continue
		case 2:
			if r >= 0x40 && r <= 0x7e {
				state = 0
			}
			continue
		case 3:
			if r == 7 {
				state = 0
			} else if r == 27 {
				state = 4
			}
			continue
		case 4:
			if r == '\\' {
				state = 0
			} else {
				state = 3
			}
			continue
		}

		if r == 27 {
			state = 1
			continue
		}
		if r == 0x9b {
			state = 2
			continue
		}
		if r == 0x9d {
			state = 3
			continue
		}

		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			if r == '\n' || r == '\t' || r == '\r' {
				b.WriteByte(' ')
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func writeItem(w io.Writer, i domain.Item) error {
	when := i.Start
	if i.StartDate != "" {
		when = i.StartDate
	}
	if i.Evening {
		when += " evening"
	}

	_, err := fmt.Fprintf(w, "%s  %s  %s  %s\n", safe(when), safe(i.Status), safe(i.Title), safe(i.ID))
	return err
}

func writeText(w io.Writer, r domain.Response) error {
	switch value := r.Data.(type) {
	case []domain.Item:
		if len(value) == 0 {
			_, err := fmt.Fprintln(w, "No matching items.")
			return err
		}

		for _, i := range value {
			if err := writeItem(w, i); err != nil {
				return err
			}
		}
		if r.Meta.NextCursor != nil {
			_, err := fmt.Fprintf(w, "Next cursor: %s\n", *r.Meta.NextCursor)
			return err
		}
		return nil
	case domain.Item:
		if err := writeItem(w, value); err != nil {
			return err
		}
		if value.Notes != "" {
			if _, err := fmt.Fprintln(w, safe(value.Notes)); err != nil {
				return err
			}
		}

		for _, c := range value.Checklist {
			if _, err := fmt.Fprintf(w, "  %s  %s\n", safe(c.Status), safe(c.Title)); err != nil {
				return err
			}
		}

		for _, i := range value.Items {
			if err := writeItem(w, i); err != nil {
				return err
			}
			for _, c := range i.Items {
				if err := writeItem(w, c); err != nil {
					return err
				}
			}
		}
		return nil
	case domain.Operation:
		if value.DryRun {
			_, err := fmt.Fprintln(w, "Validated plan (no dispatch):")
			if err != nil {
				return err
			}
			return writeJSON(w, value)
		}

		if _, err := fmt.Fprintf(w, "%s  operation %s\n", safe(value.Outcome), safe(value.ID)); err != nil {
			return err
		}
		for _, id := range value.ItemIDs {
			if _, err := fmt.Fprintf(w, "Item: %s\n", safe(id)); err != nil {
				return err
			}
		}
		for _, c := range value.Children {
			if _, err := fmt.Fprintf(w, "  %s  operation %s\n", safe(c.Outcome), safe(c.ID)); err != nil {
				return err
			}
		}
		return nil
	default:
		return writeJSON(w, r.Data)
	}
}
