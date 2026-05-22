package tx

import (
	"context"
	"sync"
)

// Tx 事务接口，不透明类型
// Service/Repo 只通过这个抽象使用事务，不依赖具体 ORM 类型。
type Tx interface{}

// ITxManager 事务管理器接口
// 仅暴露 RunInTx，不提供具体数据库类型。
type ITxManager interface {
	RunInTx(ctx context.Context, fn func(context.Context, Tx) error) error
}

type AfterCommitFunc func(context.Context)

type afterCommitKey struct{}

type afterCommitState struct {
	mu     sync.Mutex
	closed bool
	funcs  []AfterCommitFunc
}

func (s *afterCommitState) add(fn AfterCommitFunc) bool {
	if fn == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.funcs = append(s.funcs, fn)
	return true
}

func (s *afterCommitState) run(ctx context.Context) {
	s.mu.Lock()
	funcs := append([]AfterCommitFunc(nil), s.funcs...)
	s.closed = true
	s.funcs = nil
	s.mu.Unlock()

	for _, fn := range funcs {
		func() {
			defer func() {
				_ = recover()
			}()
			fn(ctx)
		}()
	}
}

func AfterCommit(ctx context.Context, fn AfterCommitFunc) bool {
	state, ok := ctx.Value(afterCommitKey{}).(*afterCommitState)
	if !ok {
		return false
	}
	return state.add(fn)
}

func AfterCommitOrNow(ctx context.Context, fn AfterCommitFunc) {
	if AfterCommit(ctx, fn) {
		return
	}
	if fn != nil {
		fn(context.WithoutCancel(ctx))
	}
}
