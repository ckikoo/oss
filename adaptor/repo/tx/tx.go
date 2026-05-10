// Package tx 定义了事务相关的接口和类型 请不要移动这个包，或者修改
package tx

import "context"

// Tx 事务接口，不透明类型
type Tx interface{}

// ITxManager 事务管理器接口
type ITxManager interface {
	// RunInTx 在事务中运行函数
	RunInTx(ctx context.Context, fn func(tx Tx) error) error
}
