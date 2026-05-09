# Go 1.25 评判检查清单

## 正确性

### 并发安全
- [ ] `map` 在 goroutine 间未加锁并发读写 → 数据竞争
- [ ] `sync.WaitGroup` / `sync.Mutex` 被值拷贝（应传指针或嵌入结构体）
- [ ] goroutine 闭包捕获循环变量（Go 1.22+ 已修复 `for range` 变量逃逸，但普通 `for i` 仍需注意）
- [ ] `channel` 未关闭导致 goroutine 泄漏
- [ ] `sync/atomic` 操作的变量未 64 位对齐（在 32 位平台上）

### 错误处理
- [ ] 忽略 error 返回值（`_`）且该错误可能影响逻辑
- [ ] `errors.Is` / `errors.As` 而非 `==` 比较包装错误
- [ ] `defer f.Close()` 未捕获其 error（写文件场景尤其重要）
- [ ] panic 未在 goroutine 顶层 recover

### 空指针 / 零值
- [ ] 未检查 nil 直接解引用指针
- [ ] 未初始化的 map 直接赋值（`assignment to entry in nil map`）
- [ ] 接口值是否可能是 `(*T)(nil)` 而非 `nil`（接口 nil 判断陷阱）

### 类型与转换
- [ ] `int` 与 `int64` 隐式混用导致截断
- [ ] 切片越界风险（未做 `len` 检查）
- [ ] `time.Duration` 单位混用（如 `time.Sleep(5)` 而非 `time.Sleep(5 * time.Second)`）

### Go 1.25 新特性正确使用
- [ ] 泛型约束是否合理，避免过度使用 `any`
- [ ] `iter.Seq` / `iter.Seq2`（range-over-func，1.23+）是否正确实现迭代器协议
- [ ] `slices` / `maps` 标准库是否优先于手写循环

---

## 性能

### 内存分配
- [ ] 热路径中频繁 `append` 未预分配（应 `make([]T, 0, cap)`）
- [ ] 大结构体按值传递（应传指针）
- [ ] `string([]byte)` / `[]byte(string)` 在热路径中频繁转换 → 考虑 `unsafe` 或 `strings.Builder`
- [ ] `fmt.Sprintf` 用于简单字符串拼接 → 用 `+` 或 `strings.Builder`
- [ ] `sync.Pool` 缺失，导致高频小对象 GC 压力大

### 算法复杂度
- [ ] 嵌套循环中重复调用 `len(slice)` / map 查找（应提取到变量）
- [ ] 线性扫描可替换为 map 查找
- [ ] 排序后可二分查找的场景仍用线性扫描

### I/O
- [ ] 未使用 `bufio.Writer` / `bufio.Reader` 包装文件/网络 I/O
- [ ] 循环内重复打开/关闭文件
- [ ] 大文件一次性读入内存（应流式处理）

### 并发效率
- [ ] 锁粒度过粗（整个函数加锁 vs 仅临界区）
- [ ] 可并行任务串行执行，未利用 goroutine
- [ ] `context` 超时/取消未在阻塞调用处传递

---

## 安全

### 输入校验
- [ ] 外部输入直接拼接 SQL → SQL 注入（应使用参数化查询）
- [ ] 外部输入拼接命令行 → 命令注入（`exec.Command` 应拆分参数）
- [ ] 文件路径未做 `filepath.Clean` + 边界检查 → 路径穿越
- [ ] 整数溢出（`int32` 累加后用于切片索引）

### 密码学
- [ ] 使用 `math/rand` 而非 `crypto/rand` 生成安全随机数
- [ ] 硬编码密钥 / Token / 密码
- [ ] 自定义加密逻辑（应使用标准库 `crypto/*`）
- [ ] MD5 / SHA1 用于密码哈希（应使用 `bcrypt` / `argon2`）

### 网络 / HTTP
- [ ] `http.ListenAndServe` 未设置超时（`ReadTimeout` / `WriteTimeout` / `IdleTimeout`）
- [ ] SSRF：用户控制的 URL 直接发起 HTTP 请求
- [ ] 未校验 TLS 证书（`InsecureSkipVerify: true`）
- [ ] 敏感信息写入日志（密码、token、PII）

### 资源管理
- [ ] HTTP Response Body 未 `defer resp.Body.Close()`
- [ ] 文件描述符 / 数据库连接泄漏
- [ ] goroutine 泄漏（无法退出的 goroutine）

---

## 风格 / 可读性

### 命名
- [ ] 遵循 Go 命名规范：`MixedCaps` 而非 `snake_case`
- [ ] 接口名以 `-er` 结尾（`Reader`、`Stringer`）
- [ ] 缩写词全大写：`URL`、`HTTP`、`ID`（非 `Url`、`Http`、`Id`）
- [ ] 包名小写单词，不含下划线或驼峰
- [ ] 变量名长度与作用域匹配（循环变量 `i`、`v` 可以短，函数级变量应具描述性）

### 代码组织
- [ ] 函数职责单一，避免超过 50 行的函数（超长函数标注 Minor）
- [ ] 导出函数 / 类型有 godoc 注释
- [ ] 错误信息小写开头，不含标点（Go 惯例）
- [ ] `return` 提前返回，减少嵌套层级（避免"箭头代码"）

### 惯用写法（Idiomatic Go）
- [ ] 优先 `errors.New` / `fmt.Errorf("%w", err)` 而非自定义 error 类型（除非需要）
- [ ] `if err := f(); err != nil` 而非先赋值再判断（减少变量作用域）
- [ ] 使用 `slices.Contains` / `slices.Sort`（1.21+）而非手写循环
- [ ] `context.Context` 作为函数第一个参数
- [ ] 避免 `init()` 函数中的复杂逻辑

### 测试
- [ ] 关键函数是否有对应 `_test.go`
- [ ] 表驱动测试（`[]struct{ ... }` + `for _, tc := range cases`）
- [ ] 测试函数命名：`TestFuncName_Scenario`