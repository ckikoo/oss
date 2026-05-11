package tx

import "context"

// Tx 事务接口，不透明类型
// Service/Repo 只通过这个抽象使用事务，不依赖具体 ORM 类型。
type Tx interface{}

// ITxManager 事务管理器接口
// 仅暴露 RunInTx，不提供具体数据库类型。
type ITxManager interface {
	RunInTx(ctx context.Context, fn func(context.Context, Tx) error) error
}
