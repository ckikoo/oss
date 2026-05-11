package repoerr

import (
	"database/sql"
	"errors"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

var (
	ErrNotFound    = errors.New("repo: record not found")
	ErrDuplicate   = errors.New("repo: duplicate key")
	ErrFKViolated  = errors.New("repo: foreign key violated")
	ErrInvalidData = errors.New("repo: invalid data")
)

func Wrap(err error) error {
	if err == nil {
		return nil
	}

	var mysqlErr *mysql.MySQLError

	switch {
	case errors.Is(err, gorm.ErrRecordNotFound), errors.Is(err, sql.ErrNoRows):
		return ErrNotFound
	case errors.Is(err, gorm.ErrInvalidData):
		return ErrInvalidData
	case errors.As(err, &mysqlErr):
		switch mysqlErr.Number {
		case 1062:
			return ErrDuplicate
		case 1451, 1452:
			return ErrFKViolated
		}
	}
	return err
}
