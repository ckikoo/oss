package repoerr

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

func TestWrap(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error
	}{
		{"nil error", nil, nil},
		{"sql no rows", sql.ErrNoRows, ErrNotFound},
		{"gorm not found", gorm.ErrRecordNotFound, ErrNotFound},
		{"gorm invalid data", gorm.ErrInvalidData, ErrInvalidData},
		{"mysql duplicate", &mysql.MySQLError{Number: 1062}, ErrDuplicate},
		{"mysql fk violated 1451", &mysql.MySQLError{Number: 1451}, ErrFKViolated},
		{"mysql fk violated 1452", &mysql.MySQLError{Number: 1452}, ErrFKViolated},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Wrap(tt.err)
			if !errors.Is(got, tt.want) {
				t.Fatalf("Wrap(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
