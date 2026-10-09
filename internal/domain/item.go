package domain

import "time"

type ChecklistItem struct {
	ID     string `json:"id,omitempty"`
	Title  string `json:"title"`
	Status string `json:"status,omitempty"`
}

type Tag struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type Item struct {
	ID                 string            `json:"id"`
	Kind               string            `json:"kind"`
	Title              string            `json:"title"`
	Notes              string            `json:"notes"`
	Status             string            `json:"status"`
	Trashed            bool              `json:"trashed"`
	ContextTrashed     bool              `json:"context_trashed"`
	ProjectID          string            `json:"project_id"`
	AreaID             string            `json:"area_id"`
	HeadingID          string            `json:"heading_id"`
	Tags               []Tag             `json:"tags"`
	Start              string            `json:"start"`
	StartDate          string            `json:"start_date"`
	Evening            bool              `json:"evening"`
	Deadline           string            `json:"deadline"`
	Reminder           string            `json:"reminder"`
	Checklist          []ChecklistItem   `json:"checklist"`
	CreatedAt          *time.Time        `json:"created_at"`
	ModifiedAt         *time.Time        `json:"modified_at"`
	StoppedAt          *time.Time        `json:"stopped_at"`
	Recurrence         string            `json:"recurrence"`
	TemplateID         string            `json:"template_id"`
	Capabilities       map[string]string `json:"capabilities"`
	Fingerprint        string            `json:"fingerprint"`
	Items              []Item            `json:"items,omitempty"`
	Index              int64             `json:"-"`
	TodayIndex         int64             `json:"-"`
	DeadlineSuppressed bool              `json:"-"`
}

func Fingerprint(i Item) string {
	i.Fingerprint = ""
	i.Items = nil
	return Hash(i)
}
