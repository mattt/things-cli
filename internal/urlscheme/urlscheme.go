// Package urlscheme encodes only validated, typed Things operations.
package urlscheme

import (
	"context"
	"encoding/json"
	"net/url"
	"os/exec"
	"runtime"
	"strings"

	"github.com/mattt/things-cli/internal/domain"
)

type Dispatcher interface {
	Dispatch(context.Context, string) error
}

type System struct{}

func (System) Dispatch(ctx context.Context, value string) error {
	if runtime.GOOS != "darwin" {
		return domain.E("platform_unsupported", "Things URL dispatch requires macOS.", 5)
	}
	if err := exec.CommandContext(ctx, "/usr/bin/open", value).Run(); err != nil {
		return domain.E("dispatch_failed", "The Things URL could not be dispatched. The result is uncertain; inspect the operation.", 7)
	}
	return nil
}

func encode(command string, q url.Values) string {
	return "things:///" + command + "?" + strings.ReplaceAll(q.Encode(), "+", "%20")
}

func Show(id string) string { return encode("show", url.Values{"id": {id}}) }

type object struct {
	Type       string         `json:"type"`
	Attributes map[string]any `json:"attributes"`
}

func attrs(p domain.Patch) map[string]any {
	a := map[string]any{}
	for _, f := range []struct {
		k string
		v *string
	}{
		{"title", p.Title},
		{"notes", p.Notes},
		{"when", p.When},
		{"deadline", p.Deadline},
		{"list-id", p.Project},
		{"area-id", p.Area},
		{"heading-id", p.Heading},
	} {
		if f.v != nil {
			a[f.k] = *f.v
		}
	}

	if p.Area != nil && p.Project == nil {
		delete(a, "area-id")
		a["list-id"] = *p.Area
	}
	if p.Reminder != nil && p.When != nil && *p.Reminder != "" {
		a["when"] = *p.When + "@" + *p.Reminder
	}
	if p.Tags != nil {
		a["tags"] = *p.Tags
	}
	if len(p.AddTags) > 0 {
		a["add-tags"] = strings.Join(p.AddTags, ",")
	}

	if p.Status != nil {
		switch *p.Status {
		case "completed":
			a["completed"] = true
		case "canceled":
			a["canceled"] = true
		case "incomplete":
			a["completed"] = false
			a["canceled"] = false
		}
	}

	if p.Checklist != nil {
		items := []object{}
		for _, i := range *p.Checklist {
			a := map[string]any{"title": i.Title}
			if i.Status == "completed" {
				a["completed"] = true
			}
			if i.Status == "canceled" {
				a["canceled"] = true
			}
			items = append(items, object{"checklist-item", a})
		}
		a["checklist-items"] = items
	}
	return a
}

func Build(r domain.Request, token string, callbacks map[string]string) (string, error) {
	command := "add"
	if r.Action == "create_project" {
		command = "add-project"
	}
	if r.Action == "update" {
		command = "update"
	}
	if r.Action == "update_project" {
		command = "update-project"
	}

	a := attrs(r.Patch)
	if command == "add-project" || command == "update-project" {
		delete(a, "list-id")
		if r.Patch.Area != nil {
			a["area-id"] = *r.Patch.Area
		}
	}

	q := url.Values{}
	if r.Action == "create_project" && len(r.Items) > 0 || r.Action == "create_todo" && r.Patch.Checklist != nil {
		command = "json"
		children := []object{}
		for _, i := range r.Items {
			typ := "to-do"
			if i.Kind == "heading" {
				typ = "heading"
			}
			children = append(children, object{typ, attrs(i.Patch)})
		}
		if len(children) > 0 {
			a["items"] = children
		}

		typ := "to-do"
		if r.Action == "create_project" {
			typ = "project"
		}
		b, err := json.Marshal([]object{{typ, a}})
		if err != nil {
			return "", err
		}
		q.Set("data", string(b))
	} else {
		for k, v := range a {
			switch x := v.(type) {
			case string:
				q.Set(k, x)
			case bool:
				if x {
					q.Set(k, "true")
				} else {
					q.Set(k, "false")
				}
			case []string:
				q.Set(k, strings.Join(x, ","))
			case []object:
				titles := []string{}
				for _, i := range x {
					titles = append(titles, i.Attributes["title"].(string))
				}
				q.Set(k, strings.Join(titles, "\n"))
			}
		}
	}

	if r.ID != "" {
		q.Set("id", r.ID)
	}
	if r.Action == "update" || r.Action == "update_project" {
		q.Set("auth-token", token)
	}
	for k, v := range callbacks {
		q.Set(k, v)
	}
	return encode(command, q), nil
}

func NormalizeID(value string) (string, error) {
	if !strings.HasPrefix(value, "things:") {
		return value, nil
	}
	u, err := url.Parse(value)
	if err != nil || u.Scheme != "things" || u.Path != "/show" || u.Query().Get("id") == "" {
		return "", domain.E("invalid_selector", "Use an item ID, an ID prefix, or a Things item link.", 2)
	}
	return u.Query().Get("id"), nil
}
