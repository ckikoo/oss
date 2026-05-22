package tx

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	mysqlgorm "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRunInTxRunsAfterCommitHooksAfterCommit(t *testing.T) {
	db, mock := newMockGormDB(t)
	mock.ExpectBegin()
	mock.ExpectCommit()

	manager := NewGormTxManager(db)
	called := false
	err := manager.RunInTx(context.Background(), func(ctx context.Context, tx Tx) error {
		if ok := AfterCommit(ctx, func(context.Context) {
			called = true
		}); !ok {
			t.Fatalf("AfterCommit() = false, want true")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("RunInTx() error = %v", err)
	}
	if !called {
		t.Fatalf("after commit hook was not called")
	}
	assertTxSQLExpectations(t, mock)
}

func TestRunInTxSkipsAfterCommitHooksAfterRollback(t *testing.T) {
	db, mock := newMockGormDB(t)
	mock.ExpectBegin()
	mock.ExpectRollback()

	manager := NewGormTxManager(db)
	wantErr := errors.New("rollback")
	called := false
	err := manager.RunInTx(context.Background(), func(ctx context.Context, tx Tx) error {
		if ok := AfterCommit(ctx, func(context.Context) {
			called = true
		}); !ok {
			t.Fatalf("AfterCommit() = false, want true")
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("RunInTx() error = %v, want %v", err, wantErr)
	}
	if called {
		t.Fatalf("after commit hook should not run after rollback")
	}
	assertTxSQLExpectations(t, mock)
}

func TestRunInTxRunsAfterCommitHooksInRegistrationOrder(t *testing.T) {
	db, mock := newMockGormDB(t)
	mock.ExpectBegin()
	mock.ExpectCommit()

	manager := NewGormTxManager(db)
	var got []int
	err := manager.RunInTx(context.Background(), func(ctx context.Context, tx Tx) error {
		AfterCommit(ctx, func(context.Context) { got = append(got, 1) })
		AfterCommit(ctx, func(context.Context) { got = append(got, 2) })
		return nil
	})
	if err != nil {
		t.Fatalf("RunInTx() error = %v", err)
	}
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("hook order = %v, want [1 2]", got)
	}
	assertTxSQLExpectations(t, mock)
}

func TestRunInTxRecoversAfterCommitPanic(t *testing.T) {
	db, mock := newMockGormDB(t)
	mock.ExpectBegin()
	mock.ExpectCommit()

	manager := NewGormTxManager(db)
	calledAfterPanic := false
	err := manager.RunInTx(context.Background(), func(ctx context.Context, tx Tx) error {
		AfterCommit(ctx, func(context.Context) { panic("hook panic") })
		AfterCommit(ctx, func(context.Context) { calledAfterPanic = true })
		return nil
	})
	if err != nil {
		t.Fatalf("RunInTx() error = %v", err)
	}
	if !calledAfterPanic {
		t.Fatalf("hook after panic was not called")
	}
	assertTxSQLExpectations(t, mock)
}

func TestAfterCommitOrNowOutsideTransactionRunsImmediately(t *testing.T) {
	called := false
	AfterCommitOrNow(context.Background(), func(context.Context) {
		called = true
	})
	if !called {
		t.Fatalf("AfterCommitOrNow() did not run outside transaction")
	}
}

func TestAfterCommitOutsideTransaction(t *testing.T) {
	if ok := AfterCommit(context.Background(), func(context.Context) {}); ok {
		t.Fatalf("AfterCommit() outside transaction = true, want false")
	}
}

func newMockGormDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()

	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}

	db, err := gorm.Open(mysqlgorm.New(mysqlgorm.Config{
		Conn:                      sqlDB,
		SkipInitializeWithVersion: true,
	}), &gorm.Config{
		DisableAutomaticPing:   true,
		SkipDefaultTransaction: true,
		Logger:                 logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("gorm.Open() error = %v", err)
	}
	return db, mock
}

func assertTxSQLExpectations(t *testing.T, mock sqlmock.Sqlmock) {
	t.Helper()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations were not met: %v", err)
	}
}
