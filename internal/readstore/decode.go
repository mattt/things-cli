package readstore

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/mattt/things-cli/internal/domain"
)

func decodeStatus(v int) (string, error) {
	switch v {
	case 0:
		return "incomplete", nil
	case 2:
		return "canceled", nil
	case 3:
		return "completed", nil
	}
	return "", domain.E("schema_decode", "Unknown Things item status.", 5)
}

func decodeDate(v sql.NullInt64) (string, error) {
	if !v.Valid {
		return "", nil
	}
	y, m, d := (v.Int64>>16)&2047, (v.Int64>>12)&15, (v.Int64>>7)&31
	s := fmt.Sprintf("%04d-%02d-%02d", y, m, d)
	if _, err := time.Parse("2006-01-02", s); err != nil {
		return "", domain.E("schema_decode", "Invalid Things calendar date.", 5)
	}
	return s, nil
}

func decodeTime(v sql.NullInt64) (string, error) {
	if !v.Valid {
		return "", nil
	}
	h, m := (v.Int64>>26)&31, (v.Int64>>20)&63
	if h > 23 || m > 59 {
		return "", domain.E("schema_decode", "Invalid Things reminder time.", 5)
	}
	return fmt.Sprintf("%02d:%02d", h, m), nil
}

func timestamp(v sql.NullFloat64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := time.Unix(int64(v.Float64), int64((v.Float64-float64(int64(v.Float64)))*1e9)).UTC()
	return &t
}
